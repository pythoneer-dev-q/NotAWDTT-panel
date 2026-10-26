package server

import "fmt"

const wgHostsPerSubnet = 253 // .2 through .254; .1 stays reserved for the gateway.

var (
	wgIPCacheDB    *Database
	wgIPCacheCount int
	wgIPCacheUsed  map[string]struct{}
	wgIPCacheNext  int
)

// All functions here run under dbMutex. The cache avoids scanning every device
// on each allocation when the /16 contains many registered clients.
func invalidateWGIPCacheLocked() {
	wgIPCacheDB = nil
	wgIPCacheUsed = nil
	wgIPCacheCount = 0
	wgIPCacheNext = 0
}

func wgIPForIndex(index int) string {
	subnet := index / wgHostsPerSubnet
	if subnet < 10 {
		subnet += 66 // Original allocation range first.
	} else {
		subnet -= 10
		if subnet >= 66 {
			subnet += 10
		}
	}
	return fmt.Sprintf("10.66.%d.%d", subnet, index%wgHostsPerSubnet+2)
}

func allocateWGIPLocked() string {
	if db == nil {
		return ""
	}
	if wgIPCacheDB != db || wgIPCacheCount != len(db.Devices) || wgIPCacheUsed == nil {
		wgIPCacheDB = db
		wgIPCacheUsed = make(map[string]struct{}, len(db.Devices))
		for _, dev := range db.Devices {
			if dev != nil && dev.IP != "" {
				wgIPCacheUsed[dev.IP] = struct{}{}
			}
		}
		wgIPCacheCount = len(db.Devices)
		wgIPCacheNext = 0
	}
	for checked := 0; checked < maxUsersSubnetLimit; checked++ {
		index := (wgIPCacheNext + checked) % maxUsersSubnetLimit
		ip := wgIPForIndex(index)
		if _, used := wgIPCacheUsed[ip]; used {
			continue
		}
		wgIPCacheUsed[ip] = struct{}{}
		wgIPCacheCount = len(db.Devices) + 1
		wgIPCacheNext = (index + 1) % maxUsersSubnetLimit
		return ip
	}
	return ""
}
