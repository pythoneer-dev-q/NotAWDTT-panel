package panel

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleSQLiteUsersPage keeps both the user and device queries bounded by limit.
func (a *App) handleSQLiteUsersPage(w http.ResponseWriter, r *http.Request) {
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
	status := r.URL.Query().Get("status")
	if status != "" && status != "all" && status != "active" && status != "blocked" {
		jsonError(w, "invalid status", http.StatusBadRequest)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 256 {
		jsonError(w, "query too long", http.StatusBadRequest)
		return
	}
	active := `(is_deactivated = 0 AND (expires_at <= 0 OR expires_at > ?) AND (total_bytes <= 0 OR up_bytes + down_bytes < total_bytes))`
	now := time.Now().Unix()
	where := "1=1"
	args := []interface{}{}
	if status == "active" {
		where += " AND " + active
		args = append(args, now)
	}
	if status == "blocked" {
		where += " AND is_deactivated != 0"
	}
	if q != "" {
		where += ` AND (instr(lower(password), ?) > 0 OR instr(lower(comment), ?) > 0 OR instr(lower(vk_hash), ?) > 0 OR instr(lower(device_id), ?) > 0 OR EXISTS (SELECT 1 FROM wdtt_user_devices d WHERE d.password = wdtt_users.password AND instr(lower(d.device_id), ?) > 0))`
		for i := 0; i < 5; i++ {
			args = append(args, strings.ToLower(q))
		}
	}
	var total, all, activeCount, blocked int64
	if err = panelDB.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM wdtt_users WHERE "+where, args...).Scan(&total); err != nil {
		jsonError(w, err.Error(), 503)
		return
	}
	if err = panelDB.QueryRowContext(r.Context(), `SELECT COUNT(*), COALESCE(SUM(CASE WHEN `+active+` THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN is_deactivated != 0 THEN 1 ELSE 0 END),0) FROM wdtt_users`, now).Scan(&all, &activeCount, &blocked); err != nil {
		jsonError(w, err.Error(), 503)
		return
	}
	db := &PasswordsDB{Passwords: make(map[string]*PasswordEntry)}
	_ = panelDB.QueryRowContext(r.Context(), `SELECT main_password FROM wdtt_global WHERE id=1`).Scan(&db.MainPassword)
	pageArgs := append(append([]interface{}{}, args...), limit, offset)
	cur, err := panelDB.QueryContext(r.Context(), `SELECT password, device_id, max_devices, expires_at, down_bytes, up_bytes, total_bytes, max_down_mbps, max_up_mbps, is_deactivated, comment, ports, vk_hash, sub_id, last_seen_at FROM wdtt_users WHERE `+where+` ORDER BY password LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		jsonError(w, err.Error(), 503)
		return
	}
	keys := make([]string, 0, limit)
	for cur.Next() {
		var pass string
		var deactivated int
		entry := &PasswordEntry{}
		if err = cur.Scan(&pass, &entry.DeviceID, &entry.MaxDevices, &entry.ExpiresAt, &entry.DownBytes, &entry.UpBytes, &entry.TotalBytes, &entry.MaxDownMBps, &entry.MaxUpMBps, &deactivated, &entry.Comment, &entry.Ports, &entry.VkHash, &entry.SubID, &entry.LastSeenAt); err != nil {
			break
		}
		entry.IsDeactivated = deactivated != 0
		db.Passwords[pass] = entry
		keys = append(keys, pass)
	}
	if err == nil {
		err = cur.Err()
	}
	cur.Close()
	if err != nil {
		jsonError(w, err.Error(), 503)
		return
	}
	if len(keys) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(keys)), ",")
		deviceArgs := make([]interface{}, len(keys))
		for i, k := range keys {
			deviceArgs[i] = k
		}
		devices, e := panelDB.QueryContext(r.Context(), `SELECT password, device_id FROM wdtt_user_devices WHERE password IN (`+placeholders+`) ORDER BY password, sort_order`, deviceArgs...)
		if e != nil {
			jsonError(w, e.Error(), 503)
			return
		}
		for devices.Next() {
			var pass, id string
			if e = devices.Scan(&pass, &id); e != nil {
				break
			}
			db.Passwords[pass].DeviceIDs = append(db.Passwords[pass].DeviceIDs, id)
		}
		if e == nil {
			e = devices.Err()
		}
		devices.Close()
		if e != nil {
			jsonError(w, e.Error(), 503)
			return
		}
	}
	inbound, _ := loadWdttInbound()
	host := a.resolveLinkHost(inbound)
	subURL := a.pageSubURLBuilder()
	stats := loadServerStats()
	rows := make([]map[string]interface{}, 0, len(keys))
	for _, pass := range keys {
		entry := db.Passwords[pass]
		if pass == db.MainPassword {
			row := mainUserRow(db, stats, inbound, host, a.cfg.SubTitle, subURL(entry.SubID))
			row["sub_id"], row["sub_url"] = entry.SubID, subURL(entry.SubID)
			rows = append(rows, row)
			continue
		}
		normalizeEntryDevices(entry)
		dtls, wg, client := resolveUserPorts(entry, inbound)
		rows = append(rows, map[string]interface{}{
			"password": maskPassword(pass), "password_key": pass, "device_id": deviceIDsDisplay(entry), "device_ids": entry.DeviceIDs,
			"devices_bound": len(entry.DeviceIDs), "max_devices": entryMaxDevices(db, pass, entry), "comment": entry.Comment,
			"expires_at": entry.ExpiresAt, "expires": passwordExpiry(entry), "up": formatBytes(entry.UpBytes), "down": formatBytes(entry.DownBytes),
			"up_bytes": entry.UpBytes, "down_bytes": entry.DownBytes, "total_bytes": entry.TotalBytes, "total_gb": bytesToGB(entry.TotalBytes),
			"max_down_mbps": entry.MaxDownMBps, "max_up_mbps": entry.MaxUpMBps, "traffic_used": trafficUsed(entry), "traffic_used_fmt": formatBytes(trafficUsed(entry)),
			"traffic_exceeded": trafficExceeded(entry), "is_expired": isPasswordExpired(entry), "is_deactivated": entry.IsDeactivated,
			"active": !entry.IsDeactivated && !isPasswordExpired(entry) && !trafficExceeded(entry), "online": userOnline(pass, deviceIDsDisplay(entry), false, stats, entry.NodeOnlineAt), "last_seen_at": entry.LastSeenAt,
			"ports": entry.Ports, "dtls_port": dtls, "wg_port": wg, "client_port": client, "vk_hash": entry.VkHash,
			"sub_id": entry.SubID, "sub_url": subURL(entry.SubID), "link": buildWdttLink(host, pass, a.cfg.SubTitle, entry.VkHash, entry, inbound, subURL(entry.SubID)),
		})
	}
	jsonOK(w, map[string]interface{}{"users": rows, "total": total, "counts": map[string]int64{"all": all, "active": activeCount, "blocked": blocked}, "nodes": []interface{}{}, "inbound": map[string]interface{}{"tag": inbound.Tag, "sub_title": a.cfg.SubTitle, "remark": inbound.Remark, "listen_host": inbound.ListenHost, "server_host": inbound.ServerHost, "default_link_host": a.defaultLinkHost(), "panel_tls": panelTLSEnabled(a.cfg), "dtls_port": inbound.DtlsPort, "wg_port": inbound.WgPort, "client_port": inbound.ClientPort, "dns": inbound.DNS, "max_users": inbound.MaxUsers}})
}
