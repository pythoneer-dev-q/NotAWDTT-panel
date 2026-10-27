package panel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/ildarmaga/wdtt/pkg/paneldb"
	"github.com/ildarmaga/wdtt/pkg/vkhash"
	"golang.org/x/crypto/bcrypt"
)

func readJSON(r *http.Request, v interface{}) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("empty body")
	}
	if err := json.Unmarshal(body, v); err == nil {
		return nil
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return fmt.Errorf("invalid request body")
	}
	return decodeForm(form, v)
}

func decodeForm(form url.Values, v interface{}) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("expected struct pointer")
	}
	rv = rv.Elem()
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		val := form.Get(name)
		if val == "" {
			continue
		}
		fv := rv.Field(i)
		if !fv.CanSet() {
			continue
		}
		switch fv.Kind() {
		case reflect.String:
			fv.SetString(val)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid %s", name)
			}
			fv.SetInt(n)
		case reflect.Bool:
			fv.SetBool(val == "true" || val == "1")
		}
	}
	return nil
}

func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "obj": v})
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "msg": msg})
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats := loadServerStats()
	db, _ := loadPasswords()
	mongoOk := false
	nodesCount := 0
	nodesOnline := 0
	if m, err := paneldb.GetDefaultMongo(); err == nil && m != nil {
		mongoOk = true
		if nodes, err := m.ListNodes(r.Context()); err == nil {
			online := 0
			for _, n := range nodes {
				if n.IsLocal && !localNodeEnabled() {
					continue
				}
				nodesCount++
				if n.Status == "online" {
					online++
				}
			}
			nodesOnline = online
		}
	}

	jsonOK(w, map[string]interface{}{
		"wdtt_active":       localNodeEnabled() && serviceActive(wdttServiceUnit),
		"xray_active":       serviceActive(xrayServiceUnit),
		"mongo_active":      mongoOk,
		"nodes_count":       nodesCount,
		"nodes_online":      nodesOnline,
		"wdtt_iface":        getWdttIface(),
		"xray_version":      xrayVersion(),
		"xray_binary":       xrayBinary(),
		"stats":             stats,
		"users_count":       len(db.Passwords),
		"devices_count":     len(db.Devices),
		"server_ip":         a.serverIP(),
		"default_link_host": a.defaultLinkHost(),
		"panel_tls":         panelTLSEnabled(a.cfg),
	})
}

func (a *App) serverIP() string {
	out, _ := runCmd("curl", "-4", "-s", "--max-time", "3", "ifconfig.me")
	if out != "" {
		return strings.TrimSpace(out)
	}
	out, _ = runCmd("hostname", "-I")
	if fields := strings.Fields(out); len(fields) > 0 {
		return fields[0]
	}
	return "127.0.0.1"
}

func (a *App) enrichUserSub(u map[string]interface{}, entry *PasswordEntry) {
	if u == nil || entry == nil {
		return
	}
	u["sub_id"] = entry.SubID
	u["sub_url"] = a.buildSubURL(entry.SubID)
}

func userRowMatchesQuery(row map[string]interface{}, query string) bool {
	for _, field := range []string{"password", "password_key", "comment", "vk_hash", "device_id"} {
		if strings.Contains(strings.ToLower(fmt.Sprint(row[field])), query) {
			return true
		}
	}
	return false
}

func (a *App) handleUsersList(w http.ResponseWriter, r *http.Request) {
	// Подтянуть live vk_calls / снять finishing; ручные хеши не затираем (#23).
	reconcileUserSessionFields()
	db, err := loadPasswords()
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	inbound, _ := loadWdttInbound()
	linkHost := a.resolveLinkHost(inbound)
	stats := loadServerStats()
	mainSub := ""
	if mainEntry, ok := db.Passwords[db.MainPassword]; ok && mainEntry != nil {
		mainSub = a.buildSubURL(mainEntry.SubID)
	}
	mainRow := mainUserRow(db, stats, inbound, linkHost, a.cfg.SubTitle, mainSub)
	if mainEntry, ok := db.Passwords[db.MainPassword]; ok {
		a.enrichUserSub(mainRow, mainEntry)
	}
	counts := map[string]int{"all": 1, "active": 0, "blocked": 0}
	if mainRow["active"] == true {
		counts["active"]++
	}
	if mainRow["is_deactivated"] == true {
		counts["blocked"]++
	}
	keys := make([]string, 0, len(db.Passwords))
	for pass, entry := range db.Passwords {
		if pass == db.MainPassword {
			continue
		}
		if entry == nil {
			continue
		}
		keys = append(keys, pass)
		counts["all"]++
		if entry.IsDeactivated {
			counts["blocked"]++
		}
		if !entry.IsDeactivated && !isPasswordExpired(entry) && !trafficExceeded(entry) {
			counts["active"]++
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		ci := strings.ToLower(strings.TrimSpace(db.Passwords[keys[i]].Comment))
		cj := strings.ToLower(strings.TrimSpace(db.Passwords[keys[j]].Comment))
		if ci != cj {
			return ci < cj
		}
		mi, mj := maskPassword(keys[i]), maskPassword(keys[j])
		if mi != mj {
			return mi < mj
		}
		return keys[i] < keys[j]
	})
	selected := make([]string, 0, len(keys)+1)
	selected = append(selected, db.MainPassword)
	selected = append(selected, keys...)
	totalUsers := len(selected)
	if _, paged := r.URL.Query()["limit"]; paged {
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil || limit < 1 || limit > 200 {
			jsonError(w, "limit must be between 1 and 200", http.StatusBadRequest)
			return
		}
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil || offset < 0 {
			jsonError(w, "offset must be non-negative", http.StatusBadRequest)
			return
		}
		query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		status := r.URL.Query().Get("status")
		if status != "" && status != "all" && status != "active" && status != "blocked" {
			jsonError(w, "invalid status", http.StatusBadRequest)
			return
		}
		filtered := make([]string, 0, len(selected))
		for _, pass := range selected {
			if pass == db.MainPassword {
				if (status == "active" && mainRow["active"] != true) || (status == "blocked" && mainRow["is_deactivated"] != true) {
					continue
				}
				if query != "" && !userRowMatchesQuery(mainRow, query) {
					continue
				}
			} else {
				entry := db.Passwords[pass]
				if (status == "active" && (entry.IsDeactivated || isPasswordExpired(entry) || trafficExceeded(entry))) || (status == "blocked" && !entry.IsDeactivated) {
					continue
				}
				if query != "" {
					normalizeEntryDevices(entry)
					matched := false
					for _, value := range []string{pass, maskPassword(pass), entry.Comment, entry.VkHash, deviceIDsDisplay(entry)} {
						if strings.Contains(strings.ToLower(value), query) {
							matched = true
							break
						}
					}
					if !matched {
						continue
					}
				}
			}
			filtered = append(filtered, pass)
		}
		totalUsers = len(filtered)
		if offset > totalUsers {
			offset = totalUsers
		}
		end := offset + limit
		if end > totalUsers {
			end = totalUsers
		}
		selected = filtered[offset:end]
	}
	users := make([]map[string]interface{}, 0, len(selected))
	for _, pass := range selected {
		if pass == db.MainPassword {
			users = append(users, mainRow)
			continue
		}
		entry := db.Passwords[pass]
		used := trafficUsed(entry)
		dtlsPort, wgPort, clientPort := resolveUserPorts(entry, inbound)
		normalizeEntryDevices(entry)
		u := map[string]interface{}{
			"password":         maskPassword(pass),
			"password_key":     pass,
			"device_id":        deviceIDsDisplay(entry),
			"device_ids":       entry.DeviceIDs,
			"devices_bound":    len(entry.DeviceIDs),
			"max_devices":      entryMaxDevices(db, pass, entry),
			"comment":          entry.Comment,
			"expires_at":       entry.ExpiresAt,
			"expires":          passwordExpiry(entry),
			"up":               formatBytes(entry.UpBytes),
			"down":             formatBytes(entry.DownBytes),
			"up_bytes":         entry.UpBytes,
			"down_bytes":       entry.DownBytes,
			"total_bytes":      entry.TotalBytes,
			"total_gb":         bytesToGB(entry.TotalBytes),
			"max_down_mbps":    entry.MaxDownMBps,
			"max_up_mbps":      entry.MaxUpMBps,
			"traffic_used":     used,
			"traffic_used_fmt": formatBytes(used),
			"traffic_exceeded": trafficExceeded(entry),
			"is_expired":       isPasswordExpired(entry),
			"is_deactivated":   entry.IsDeactivated,
			"active":           !entry.IsDeactivated && !isPasswordExpired(entry) && !trafficExceeded(entry),
			"online":           userOnline(pass, deviceIDsDisplay(entry), false, stats, entry.NodeOnlineAt),
			"last_seen_at":     entry.LastSeenAt,
			"ports":            entry.Ports,
			"dtls_port":        dtlsPort,
			"wg_port":          wgPort,
			"client_port":      clientPort,
			"link":             buildWdttLink(linkHost, pass, a.cfg.SubTitle, entry.VkHash, entry, inbound, a.buildSubURL(entry.SubID)),
			"vk_hash":          entry.VkHash,
		}
		a.enrichUserSub(u, entry)
		users = append(users, u)
	}
	devices := []map[string]interface{}{}
	if _, paged := r.URL.Query()["limit"]; !paged {
		for id, dev := range db.Devices {
			devices = append(devices, map[string]interface{}{
				"device_id": id,
				"ip":        dev.IP,
			})
		}
	}
	var clusterNodes []*paneldb.Node
	if m, err := paneldb.GetDefaultMongo(); err == nil && m != nil {
		clusterNodes, _ = m.ListNodes(r.Context())
		filtered := clusterNodes[:0]
		for _, node := range clusterNodes {
			if node.IsActive && (!node.IsLocal || localNodeEnabled()) {
				filtered = append(filtered, node)
			}
		}
		clusterNodes = filtered
	}
	jsonOK(w, map[string]interface{}{
		"users":   users,
		"total":   totalUsers,
		"counts":  counts,
		"devices": devices,
		"nodes":   clusterNodes,
		"api_key": a.cfg.ApiKey,
		"inbound": map[string]interface{}{
			"tag":               inbound.Tag,
			"sub_title":         a.cfg.SubTitle,
			"remark":            inbound.Remark,
			"listen_host":       inbound.ListenHost,
			"server_host":       inbound.ServerHost,
			"default_link_host": a.defaultLinkHost(),
			"panel_tls":         panelTLSEnabled(a.cfg),
			"dtls_port":         inbound.DtlsPort,
			"wg_port":           inbound.WgPort,
			"client_port":       inbound.ClientPort,
			"dns":               inbound.DNS,
			"max_users":         inbound.MaxUsers,
		},
	})
}

type userAPIReq struct {
	Password       string    `json:"password"`
	DeviceID       string    `json:"device_id"`
	DeviceIDs      *[]string `json:"device_ids"`
	MaxDevices     int       `json:"max_devices"`
	Comment        string    `json:"comment"`
	ExpiresAt      int64     `json:"expires_at"`
	TotalGB        float64   `json:"total_gb"`
	MaxDownMBps    float64   `json:"max_down_mbps"`
	MaxUpMBps      float64   `json:"max_up_mbps"`
	Active         *bool     `json:"active"`
	Ports          string    `json:"ports"`
	DtlsPort       int       `json:"dtls_port"`
	WgPort         int       `json:"wg_port"`
	ClientPort     int       `json:"client_port"`
	UseCustomPorts bool      `json:"use_custom_ports"`
	VkHash         string    `json:"vk_hash"`
	Count          int       `json:"count"`
}

func passwordEntryFromReq(req userAPIReq) *PasswordEntry {
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	entry := &PasswordEntry{
		Comment:       strings.TrimSpace(req.Comment),
		ExpiresAt:     req.ExpiresAt,
		TotalBytes:    gbToBytes(req.TotalGB),
		MaxDownMBps:   req.MaxDownMBps,
		MaxUpMBps:     req.MaxUpMBps,
		IsDeactivated: !active,
		Ports:         portsFromReq(req),
		MaxDevices:    req.MaxDevices,
		VkHash:        vkhash.Normalize(req.VkHash),
	}
	if req.DeviceIDs != nil {
		// Поле передано явно (в т.ч. пустой список = отвязать все устройства).
		entry.DeviceIDs = append([]string(nil), (*req.DeviceIDs)...)
	} else if id := strings.TrimSpace(req.DeviceID); id != "" {
		entry.DeviceIDs = []string{id}
	}
	normalizeEntryDevices(entry)
	return entry
}

func portsFromReq(req userAPIReq) string {
	if req.Ports != "" {
		return strings.TrimSpace(req.Ports)
	}
	if !req.UseCustomPorts {
		return ""
	}
	if req.DtlsPort > 0 && req.WgPort > 0 && req.ClientPort > 0 {
		return strconv.Itoa(req.DtlsPort) + "," + strconv.Itoa(req.WgPort) + "," + strconv.Itoa(req.ClientPort)
	}
	return ""
}

func (a *App) handleUserAdd(w http.ResponseWriter, r *http.Request) {
	var req userAPIReq
	if err := readJSON(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	if err := validateUserLimits(req.TotalGB, req.MaxDownMBps, req.MaxUpMBps); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	if req.Password == "" && req.DeviceID == "" && req.Comment == "" && req.ExpiresAt == 0 && req.TotalGB == 0 && req.Active == nil {
		if req.Count < 1 {
			req.Count = 1
		}
		created, err := addUserPasswords(req.Count)
		if err != nil {
			jsonError(w, err.Error(), 400)
			return
		}
		jsonOK(w, map[string]interface{}{"passwords": created})
		return
	}
	entry := passwordEntryFromReq(req)
	pw, err := createUser(strings.TrimSpace(req.Password), entry)
	if err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	jsonOK(w, map[string]interface{}{"password": pw})
}

func (a *App) handleUserUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		userAPIReq
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	manageDevices := req.DeviceIDs != nil || strings.TrimSpace(req.DeviceID) != ""
	if err := updateUser(req.OldPassword, strings.TrimSpace(req.Password), req.userAPIReq, manageDevices); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	jsonOK(w, nil)
}

func (a *App) handleUserResetTraffic(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	if req.Password == "" {
		jsonError(w, "пароль не указан", 400)
		return
	}
	if err := resetUserTraffic(req.Password); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	jsonOK(w, nil)
}

func (a *App) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	if req.Password == "" {
		jsonError(w, "пароль не указан", 400)
		return
	}
	if err := deleteUserPassword(req.Password); err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, nil)
}

func (a *App) handleMainPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	if !validPassword(req.Password) {
		jsonError(w, "пароль должен быть от 1 до 128 символов", 400)
		return
	}
	if err := updateWdttPassword(req.Password); err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, nil)
}

func (a *App) handleXrayVersions(w http.ResponseWriter, r *http.Request) {
	tags, err := xrayVersionTagList()
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, map[string]interface{}{
		"current":  xrayVersion(),
		"versions": tags,
	})
}

func (a *App) handleXrayInstall(w http.ResponseWriter, r *http.Request) {
	tag := strings.TrimPrefix(r.URL.Path, a.cfg.basePath()+"panel/api/xray/install/")
	if tag == "" {
		jsonError(w, "версия не указана", 400)
		return
	}
	if err := installXrayVersion(tag); err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, map[string]string{"version": tag})
}

func (a *App) handleXrayGeofiles(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, a.cfg.basePath()+"panel/api/xray/geofiles/")
	updated, restartXray, err := updateGeofilesOp(name)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	if restartXray {
		serviceRestart(xrayServiceUnit)
	}
	if len(updated) == 1 {
		jsonOK(w, map[string]string{"updated": updated[0]})
		return
	}
	jsonOK(w, map[string]interface{}{"updated": updated})
}

func (a *App) handleXrayConfig(w http.ResponseWriter, r *http.Request) {
	raw, err := loadXrayConfigRaw()
	if err != nil {
		jsonError(w, err.Error(), 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(raw))
}

func (a *App) handlePanelPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
		NewUsername string `json:"new_username"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(a.cfg.PasswordHash), []byte(req.OldPassword)) != nil {
		jsonError(w, "неверный текущий пароль", 401)
		return
	}
	next := *a.cfg
	if req.NewPassword != "" {
		if len([]byte(req.NewPassword)) > 72 {
			jsonError(w, "Пароль не должен превышать 72 байта", 400)
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			jsonError(w, err.Error(), 500)
			return
		}
		next.PasswordHash = string(hash)
		next.SessionKey = randomHex(32)
	}
	if req.NewUsername != "" {
		next.Username = strings.TrimSpace(req.NewUsername)
	}
	if err := saveAdminConfig(&next); err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	*a.cfg = next
	jsonOK(w, nil)
}

func (a *App) handleLogs(w http.ResponseWriter, r *http.Request) {
	n := 50
	if v := r.URL.Query().Get("n"); v != "" {
		n, _ = strconv.Atoi(v)
	}
	svc := r.URL.Query().Get("service")
	if svc == "" {
		svc = "wdtt"
	}
	level := r.URL.Query().Get("level")
	syslog := r.URL.Query().Get("syslog") == "true"
	lines := fetchServiceLogLines(n, svc, level, syslog)
	jsonOK(w, map[string]interface{}{"logs": lines})
}

func writeStatic(w http.ResponseWriter, data []byte, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}
