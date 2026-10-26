package server

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// mbpsToTcRate converts stored MB/s (megabytes/sec) to a tc rate string.
// UI shows Мбит/с and stores value/8 as MB/s.
func mbpsToTcRate(mbPerSec float64) string {
	if mbPerSec <= 0 {
		return "8kbit"
	}
	// MB/s → kbit/s: * 8 (bits) * 1000
	kbit := int(mbPerSec*8000.0 + 0.5)
	if kbit < 8 {
		kbit = 8 // ~1 KB/s floor
	}
	if kbit%1000 == 0 {
		return fmt.Sprintf("%dmbit", kbit/1000)
	}
	return fmt.Sprintf("%dkbit", kbit)
}

func ipClassID(ip string) (int, error) {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return 0, fmt.Errorf("invalid ip: %s", ip)
	}
	third, err := strconv.Atoi(parts[2])
	if err != nil || third < 0 || third > 255 {
		return 0, fmt.Errorf("invalid ip: %s", ip)
	}
	last, err := strconv.Atoi(parts[3])
	if err != nil || last < 1 || last > 254 {
		return 0, fmt.Errorf("invalid ip: %s", ip)
	}
	id := third*256 + last
	if id == 1 { // 1:1 is the HTB parent class.
		id = 256 // .0 is never allocated.
	} else if id == 999 { // 1:999 is the HTB default class.
		id = 768 // .0 is never allocated.
	}
	return id, nil
}

func tcAvailable() bool {
	_, err := runCmd("tc", "-Version")
	return err == nil
}

func resetTcOnIface(iface string) {
	runCmdSilent("tc", "qdisc", "del", "dev", iface, "root")
	runCmdSilent("tc", "qdisc", "del", "dev", iface, "ingress")
}

// tcNeedsHTBReset — true if root is not clean HTB 1: (e.g. fq from BBR default_qdisc).
func tcNeedsHTBReset(qdiscShow string) bool {
	hasHTB := false
	hasOtherRoot := false
	for _, line := range strings.Split(qdiscShow, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, " root ") {
			continue
		}
		if strings.Contains(line, "qdisc htb 1:") {
			hasHTB = true
			continue
		}
		if strings.HasPrefix(line, "qdisc ") {
			hasOtherRoot = true
		}
	}
	return !hasHTB || hasOtherRoot
}

func ensureTcHTB(iface string) {
	out, _ := runCmd("tc", "qdisc", "show", "dev", iface)
	if tcNeedsHTBReset(out) {
		runCmdSilent("tc", "qdisc", "del", "dev", iface, "root")
		if msg := runCmdSilent("tc", "qdisc", "add", "dev", iface, "root", "handle", "1:", "htb", "default", "999"); msg != "" {
			log.Printf("[TC] qdisc htb add %s: %s", iface, msg)
		}
	}
	runCmdSilent("tc", "class", "add", "dev", iface, "parent", "1:", "classid", "1:1", "htb", "rate", "10gbit")
	runCmdSilent("tc", "class", "add", "dev", iface, "parent", "1:1", "classid", "1:999", "htb", "rate", "10gbit", "ceil", "10gbit")
}

func ensureTcIngress(iface string) {
	out, _ := runCmd("tc", "qdisc", "show", "dev", iface)
	if strings.Contains(out, "ingress") {
		return
	}
	if msg := runCmdSilent("tc", "qdisc", "add", "dev", iface, "handle", "ffff:", "ingress"); msg != "" {
		log.Printf("[TC] ingress add %s: %s", iface, msg)
	}
}

func removeClientSpeedLimits(iface, ip string) {
	octet, err := ipClassID(ip)
	if err != nil {
		return
	}
	classID := fmt.Sprintf("1:%d", octet)
	runCmdSilent("tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", strconv.Itoa(octet))
	runCmdSilent("tc", "class", "del", "dev", iface, "classid", classID)
	runCmdSilent("tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "ffff:", "prio", strconv.Itoa(octet))
}

func tcFilterExists(iface, parent string, prio int) bool {
	out, _ := runCmd("tc", "filter", "show", "dev", iface, "parent", parent)
	pref := fmt.Sprintf("pref %d ", prio)
	pref2 := fmt.Sprintf("pref %d\n", prio)
	return strings.Contains(out, pref) || strings.Contains(out, pref2) ||
		strings.Contains(out, fmt.Sprintf("prio %d", prio))
}

// applyClientSpeedLimits updates live HTB rates in place (class replace) so
// Save 1→5 Mbit applies without client reconnect. Filters are kept when possible.
func applyClientSpeedLimits(iface, ip string, downMBps, upMBps float64) {
	if downMBps <= 0 && upMBps <= 0 {
		removeClientSpeedLimits(iface, ip)
		return
	}
	octet, err := ipClassID(ip)
	if err != nil {
		log.Printf("[TC] %v", err)
		return
	}
	if downMBps > 0 {
		ensureTcHTB(iface)
		rate := mbpsToTcRate(downMBps)
		classID := fmt.Sprintf("1:%d", octet)
		// replace = create or update rate without dropping the class/flows
		if out := runCmdSilent("tc", "class", "replace", "dev", iface, "parent", "1:1", "classid", classID,
			"htb", "rate", rate, "ceil", rate); out != "" {
			log.Printf("[TC] class replace %s: %s", ip, out)
		}
		prio := octet
		if !tcFilterExists(iface, "1:", prio) {
			if out := runCmdSilent("tc", "filter", "add", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", strconv.Itoa(prio),
				"u32", "match", "ip", "dst", ip+"/32", "flowid", classID); out != "" {
				log.Printf("[TC] filter dst %s: %s", ip, out)
			}
		}
		log.Printf("[TC] ↓ %s: %.4f MB/s (%s)", ip, downMBps, rate)
	} else {
		// clear download only
		prio := octet
		classID := fmt.Sprintf("1:%d", octet)
		runCmdSilent("tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", strconv.Itoa(prio))
		runCmdSilent("tc", "class", "del", "dev", iface, "classid", classID)
	}
	if upMBps > 0 {
		ensureTcIngress(iface)
		rate := mbpsToTcRate(upMBps)
		prio := octet
		// police rate cannot be changed in place — recreate filter
		runCmdSilent("tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "ffff:", "prio", strconv.Itoa(prio))
		if out := runCmdSilent("tc", "filter", "add", "dev", iface, "parent", "ffff:", "protocol", "ip", "prio", strconv.Itoa(prio),
			"u32", "match", "ip", "src", ip+"/32", "police", "rate", rate, "burst", "32kb", "drop"); out != "" {
			log.Printf("[TC] filter src %s: %s", ip, out)
		} else {
			log.Printf("[TC] ↑ %s: %.4f MB/s (%s)", ip, upMBps, rate)
		}
	} else {
		runCmdSilent("tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "ffff:", "prio", strconv.Itoa(octet))
	}
}

func speedLimitIPsForEntry(entry *PasswordEntry) []string {
	ips := make([]string, 0)
	for _, devID := range allEntryDeviceIDs(entry) {
		dev, ok := db.Devices[devID]
		if ok && dev != nil && dev.IP != "" {
			ips = append(ips, dev.IP)
		}
	}
	return ips
}

type clientSpeedLimit struct {
	down float64
	up   float64
}

var (
	speedApplyMu       sync.Mutex
	appliedSpeedLimits = make(map[string]clientSpeedLimit)
	speedTcMissingOnce sync.Once
	speedSyncOnce      sync.Once
	speedSyncRequests  = make(chan struct{}, 1)
)

func speedLimitKey(iface, ip string) string { return iface + "|" + ip }

// Called while dbMutex is held by GETCONF; tc work must happen after unlock.
func applySpeedLimitForEntryUnlocked(entry *PasswordEntry) {
	if entry != nil {
		requestSpeedLimitSync()
	}
}

func applySpeedLimitForPassword(_ string) { syncAllSpeedLimits() }

func requestSpeedLimitSync() {
	speedSyncOnce.Do(func() {
		go func() {
			for range speedSyncRequests {
				time.Sleep(50 * time.Millisecond)
				syncAllSpeedLimits()
			}
		}()
	})
	select {
	case speedSyncRequests <- struct{}{}:
	default:
	}
}

func syncAllSpeedLimits() {
	speedApplyMu.Lock()
	defer speedApplyMu.Unlock()

	limits := make(map[string]clientSpeedLimit)
	dbMutex.Lock()
	if db != nil {
		for _, entry := range db.Passwords {
			if entry == nil || entry.IsDeactivated || isPasswordExpired(entry) || isTrafficExceeded(entry) {
				continue
			}
			limit := clientSpeedLimit{down: entry.MaxDownMBps, up: entry.MaxUpMBps}
			if limit.down <= 0 && limit.up <= 0 {
				continue
			}
			for _, ip := range speedLimitIPsForEntry(entry) {
				limits[speedLimitKey(wgIfaceName, ip)] = limit
			}
		}
	}
	dbMutex.Unlock()

	// RAW addresses are assigned per active session and are distinct from WG IPs.
	rawSessionsByIP.Range(func(ipKey, value any) bool {
		ip, _ := ipKey.(string)
		group, _ := value.(*rawSessionGroup)
		if ip == "" || group == nil {
			return true
		}
		group.mu.Lock()
		password := ""
		if len(group.sessions) > 0 && group.sessions[0] != nil {
			password = group.sessions[0].password
		}
		group.mu.Unlock()
		if password == "" {
			return true
		}
		dbMutex.Lock()
		var entry *PasswordEntry
		if db != nil {
			entry = db.Passwords[password]
		}
		if entry != nil && !entry.IsDeactivated && !isPasswordExpired(entry) && !isTrafficExceeded(entry) && (entry.MaxDownMBps > 0 || entry.MaxUpMBps > 0) {
			limits[speedLimitKey(rawIfaceName, ip)] = clientSpeedLimit{down: entry.MaxDownMBps, up: entry.MaxUpMBps}
		}
		dbMutex.Unlock()
		return true
	})

	if len(limits) == 0 && len(appliedSpeedLimits) == 0 {
		return
	}
	if !tcAvailable() {
		speedTcMissingOnce.Do(func() {
			log.Print("[TC] tc не найден: лимиты скорости будут применены после установки iproute2 и перезагрузки конфигурации")
		})
		return
	}
	for key := range appliedSpeedLimits {
		if _, ok := limits[key]; !ok {
			parts := strings.SplitN(key, "|", 2)
			removeClientSpeedLimits(parts[0], parts[1])
			delete(appliedSpeedLimits, key)
		}
	}
	for key, limit := range limits {
		if appliedSpeedLimits[key] == limit {
			continue
		}
		parts := strings.SplitN(key, "|", 2)
		applyClientSpeedLimits(parts[0], parts[1], limit.down, limit.up)
		appliedSpeedLimits[key] = limit
	}
}
