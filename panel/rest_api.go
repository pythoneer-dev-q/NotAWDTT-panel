package panel

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ildarmaga/wdtt/pkg/paneldb"
	"golang.org/x/crypto/bcrypt"
)

// RestUserItem represents a user in the Remnawave-style REST API.
type RestUserItem struct {
	Password        string         `json:"password"`
	PasswordKey     string         `json:"password_key"`
	Comment         string         `json:"comment"`
	Active          bool           `json:"active"`
	IsDeactivated   bool           `json:"is_deactivated"`
	ExpiresAt       int64          `json:"expires_at"`
	ExpiresLabel    string         `json:"expires_label"`
	IsExpired       bool           `json:"is_expired"`
	TotalBytes      int64          `json:"total_bytes"`
	TotalGB         float64        `json:"total_gb"`
	UpBytes         int64          `json:"up_bytes"`
	DownBytes       int64          `json:"down_bytes"`
	TrafficUsed     int64          `json:"traffic_used"`
	TrafficUsedFmt  string         `json:"traffic_used_fmt"`
	TrafficExceeded bool           `json:"traffic_exceeded"`
	DeviceIDs       []string       `json:"device_ids"`
	DevicesBound    int            `json:"devices_bound"`
	MaxDevices      int            `json:"max_devices"`
	Online          bool           `json:"online"`
	LastSeenAt      int64          `json:"last_seen_at"`
	VkHash          string         `json:"vk_hash,omitempty"`
	Ports           string         `json:"ports,omitempty"`
	DtlsPort        int            `json:"dtls_port"`
	RawPort         int            `json:"raw_port"`
	ClientPort      int            `json:"client_port"`
	SubID           string         `json:"sub_id"`
	SubURL          string         `json:"sub_url"`
	Link            string         `json:"link"`
	NodeLinks       []RestNodeLink `json:"node_links,omitempty"`
	IsMain          bool           `json:"is_main,omitempty"`
}

type RestNodeLink struct {
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
	Host     string `json:"host"`
	Link     string `json:"link"`
}

// setRestCORS adds standard CORS headers for REST API consumption.
func setRestCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, X-API-Key, X-CSRF-Token, Content-Type, Accept")
}

// restJSON sends a JSON response with status code.
func restJSON(w http.ResponseWriter, status int, data interface{}) {
	setRestCORS(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func restError(w http.ResponseWriter, status int, message string) {
	restJSON(w, status, map[string]interface{}{
		"success": false,
		"error":   message,
	})
}

// registerRestAPI registers RESTful endpoints on mux.
func registerRestAPI(mux *http.ServeMux, app *App, base string) {
	mounts := []string{base + "api/"}
	if base != "/" && base != "" {
		mounts = append(mounts, "/api/")
	}

	for _, prefix := range mounts {
		p := strings.TrimRight(prefix, "/") + "/"

		// Auth
		mux.HandleFunc(p+"auth/token", app.handleRestAuthToken)
		mux.HandleFunc(p+"auth/login", app.handleRestAuthToken)
		mux.HandleFunc(p+"auth/me", app.requireRestAuth(app.handleRestAuthMe))

		// Users
		mux.HandleFunc(p+"users", app.handleRestUsersRoot)
		mux.HandleFunc(p+"users/", func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			idx := strings.Index(path, "/users/")
			if idx != -1 {
				sub := strings.Trim(path[idx+len("/users/"):], "/")
				if sub == "" {
					app.handleRestUsersRoot(w, r)
					return
				}
			}
			app.handleRestUserItem(w, r)
		})

		// Nodes
		mux.HandleFunc(p+"nodes", app.handleRestNodesRoot)
		mux.HandleFunc(p+"nodes/", func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			idx := strings.Index(path, "/nodes/")
			if idx != -1 {
				sub := strings.Trim(path[idx+len("/nodes/"):], "/")
				if sub == "" {
					app.handleRestNodesRoot(w, r)
					return
				}
			}
			app.handleRestNodeItem(w, r)
		})

		// System
		mux.HandleFunc(p+"status", app.requireRestAuth(app.handleRestStatus))
		mux.HandleFunc(p+"system/restart", app.requireRestAuth(app.handleRestRestart))
	}
}

// handleRestAuthToken handles POST /api/auth/token and POST /api/auth/login.
func (a *App) handleRestAuthToken(w http.ResponseWriter, r *http.Request) {
	setRestCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		restError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		restError(w, http.StatusBadRequest, "invalid json payload: "+err.Error())
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = a.cfg.Username
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(a.cfg.PasswordHash), []byte(req.Password)); err != nil {
		restError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, exp := a.createToken(username, 30*24*time.Hour)
	restJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"token":      token,
		"token_type": "Bearer",
		"expires_at": exp,
		"api_key":    a.cfg.ApiKey,
		"user":       username,
	})
}

// handleRestAuthMe handles GET /api/auth/me.
func (a *App) handleRestAuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		setRestCORS(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	restJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"username": a.cfg.Username,
		"api_key":  a.cfg.ApiKey,
		"role":     "admin",
	})
}

// handleRestUsersRoot handles GET /api/users (search/filter) and POST /api/users (create).
func (a *App) handleRestUsersRoot(w http.ResponseWriter, r *http.Request) {
	setRestCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Require auth
	authWrapper := a.requireRestAuth(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			a.restListUsers(w, r)
		case http.MethodPost:
			a.restCreateUser(w, r)
		default:
			restError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
	authWrapper(w, r)
}

// restListUsers handles user search, status filter, and pagination.
func (a *App) restListUsers(w http.ResponseWriter, r *http.Request) {
	db, err := loadPasswords()
	if err != nil {
		restError(w, http.StatusInternalServerError, "failed to load users: "+err.Error())
		return
	}

	inbound, _ := loadWdttInbound()
	linkHost := a.resolveLinkHost(inbound)
	stats := loadServerStats()
	allUsers := a.buildRestUserItems(r.Context(), db, inbound, linkHost, stats)

	// Query params
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	filtered := make([]RestUserItem, 0, len(allUsers))
	for _, u := range allUsers {
		// Filter by search query
		if q != "" {
			match := strings.Contains(strings.ToLower(u.Password), q) ||
				strings.Contains(strings.ToLower(u.PasswordKey), q) ||
				strings.Contains(strings.ToLower(u.Comment), q) ||
				strings.Contains(strings.ToLower(u.VkHash), q) ||
				strings.Contains(strings.ToLower(u.SubID), q)
			if !match {
				for _, did := range u.DeviceIDs {
					if strings.Contains(strings.ToLower(did), q) {
						match = true
						break
					}
				}
			}
			if !match {
				continue
			}
		}

		// Filter by status
		if status != "" && status != "all" {
			switch status {
			case "active":
				if !u.Active {
					continue
				}
			case "blocked", "disabled", "deactivated":
				if !u.IsDeactivated {
					continue
				}
			case "expired":
				if !u.IsExpired {
					continue
				}
			case "exceeded":
				if !u.TrafficExceeded {
					continue
				}
			}
		}

		filtered = append(filtered, u)
	}

	total := len(filtered)
	offset := 0
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}
	if offset > total {
		offset = total
	}

	paged := filtered[offset:]
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			if l < len(paged) {
				paged = paged[:l]
			}
		}
	}

	restJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"total":   total,
		"count":   len(paged),
		"users":   paged,
	})
}

// restCreateUser creates a user with optional parameters (all fields default safely).
func (a *App) restCreateUser(w http.ResponseWriter, r *http.Request) {
	var req userAPIReq
	if err := readJSON(r, &req); err != nil {
		restError(w, http.StatusBadRequest, "invalid json payload: "+err.Error())
		return
	}

	entry := passwordEntryFromReq(req)
	// Default: if total_gb is 0 -> 0 bytes (unlimited)
	// Default: if expires_at is 0 -> 0 (no expiration)
	// Default: if max_devices is 0 -> 1 slot
	if entry.MaxDevices <= 0 {
		entry.MaxDevices = 1
	}

	password := strings.TrimSpace(req.Password)
	pw, err := createUser(password, entry)
	if err != nil {
		restError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Load created item
	db, _ := loadPasswords()
	inbound, _ := loadWdttInbound()
	linkHost := a.resolveLinkHost(inbound)
	stats := loadServerStats()
	userItem := a.buildSingleRestUser(r.Context(), db, pw, inbound, linkHost, stats)

	restJSON(w, http.StatusCreated, map[string]interface{}{
		"success": true,
		"message": "user created successfully",
		"user":    userItem,
	})
}

// handleRestUserItem handles /api/users/{id} and /api/users/{id}/{action}.
func (a *App) handleRestUserItem(w http.ResponseWriter, r *http.Request) {
	setRestCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	authWrapper := a.requireRestAuth(func(w http.ResponseWriter, r *http.Request) {
		// Extract path after /users/
		path := r.URL.Path
		idx := strings.Index(path, "/users/")
		if idx == -1 {
			restError(w, http.StatusNotFound, "not found")
			return
		}
		sub := strings.Trim(path[idx+len("/users/"):], "/")
		parts := strings.Split(sub, "/")
		if len(parts) == 0 || parts[0] == "" {
			restError(w, http.StatusBadRequest, "user identifier required")
			return
		}

		userKey := parts[0]
		action := ""
		if len(parts) > 1 {
			action = parts[1]
		}

		db, err := loadPasswords()
		if err != nil {
			restError(w, http.StatusInternalServerError, "failed to load db: "+err.Error())
			return
		}

		// Resolve actual password if userKey is a sub_id
		actualPass := a.resolveUserPassword(db, userKey)
		if actualPass == "" {
			restError(w, http.StatusNotFound, "user not found")
			return
		}

		inbound, _ := loadWdttInbound()
		linkHost := a.resolveLinkHost(inbound)
		stats := loadServerStats()

		switch action {
		case "activate":
			if r.Method != http.MethodPost {
				restError(w, http.StatusMethodNotAllowed, "POST required for activate")
				return
			}
			active := true
			req := userAPIReq{Active: &active}
			if err := updateUser(actualPass, actualPass, req, false); err != nil {
				restError(w, http.StatusInternalServerError, "failed to activate: "+err.Error())
				return
			}
			db, _ = loadPasswords()
			restJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"message": "user activated",
				"user":    a.buildSingleRestUser(r.Context(), db, actualPass, inbound, linkHost, stats),
			})

		case "block", "deactivate":
			if r.Method != http.MethodPost {
				restError(w, http.StatusMethodNotAllowed, "POST required for block")
				return
			}
			active := false
			req := userAPIReq{Active: &active}
			if err := updateUser(actualPass, actualPass, req, false); err != nil {
				restError(w, http.StatusInternalServerError, "failed to block: "+err.Error())
				return
			}
			db, _ = loadPasswords()
			restJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"message": "user blocked",
				"user":    a.buildSingleRestUser(r.Context(), db, actualPass, inbound, linkHost, stats),
			})

		case "reset-traffic":
			if r.Method != http.MethodPost {
				restError(w, http.StatusMethodNotAllowed, "POST required for reset-traffic")
				return
			}
			if err := resetUserTraffic(actualPass); err != nil {
				restError(w, http.StatusInternalServerError, "failed to reset traffic: "+err.Error())
				return
			}
			db, _ = loadPasswords()
			restJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"message": "traffic reset successfully",
				"user":    a.buildSingleRestUser(r.Context(), db, actualPass, inbound, linkHost, stats),
			})

		case "":
			switch r.Method {
			case http.MethodGet:
				u := a.buildSingleRestUser(r.Context(), db, actualPass, inbound, linkHost, stats)
				restJSON(w, http.StatusOK, map[string]interface{}{
					"success": true,
					"user":    u,
				})

			case http.MethodPut, http.MethodPatch:
				var updateReq userAPIReq
				if err := readJSON(r, &updateReq); err != nil {
					restError(w, http.StatusBadRequest, "invalid json payload: "+err.Error())
					return
				}
				manageDevs := updateReq.DeviceIDs != nil || strings.TrimSpace(updateReq.DeviceID) != ""
				newPass := strings.TrimSpace(updateReq.Password)
				if newPass == "" {
					newPass = actualPass
				}
				if err := updateUser(actualPass, newPass, updateReq, manageDevs); err != nil {
					restError(w, http.StatusBadRequest, err.Error())
					return
				}
				db, _ = loadPasswords()
				restJSON(w, http.StatusOK, map[string]interface{}{
					"success": true,
					"message": "user updated successfully",
					"user":    a.buildSingleRestUser(r.Context(), db, newPass, inbound, linkHost, stats),
				})

			case http.MethodDelete:
				if actualPass == db.MainPassword {
					restError(w, http.StatusBadRequest, "cannot delete main admin password")
					return
				}
				if err := deleteUserPassword(actualPass); err != nil {
					restError(w, http.StatusInternalServerError, "failed to delete: "+err.Error())
					return
				}
				restJSON(w, http.StatusOK, map[string]interface{}{
					"success": true,
					"message": "user deleted successfully",
				})

			default:
				restError(w, http.StatusMethodNotAllowed, "method not allowed")
			}

		default:
			restError(w, http.StatusNotFound, "unknown action: "+action)
		}
	})
	authWrapper(w, r)
}

// resolveUserPassword returns actual password string matching either password or sub_id.
func (a *App) resolveUserPassword(db *PasswordsDB, key string) string {
	if db == nil {
		return ""
	}
	if _, ok := db.Passwords[key]; ok {
		return key
	}
	for pass, entry := range db.Passwords {
		if entry != nil && entry.SubID != "" && entry.SubID == key {
			return pass
		}
	}
	return ""
}

// buildRestUserItems builds full REST user objects for all users in DB.
func (a *App) buildRestUserItems(ctx context.Context, db *PasswordsDB, inbound WdttInboundConfig, linkHost string, stats *ServerStats) []RestUserItem {
	var nodes []*paneldb.Node
	if m, err := paneldb.GetDefaultMongo(); err == nil && m != nil {
		nodes, _ = m.ListNodes(ctx)
	}

	list := make([]RestUserItem, 0, len(db.Passwords)+1)

	// Main user
	if db.MainPassword != "" {
		mainItem := a.buildSingleRestUserWithNodes(db, db.MainPassword, inbound, linkHost, stats, nodes)
		mainItem.IsMain = true
		list = append(list, mainItem)
	}

	for pass := range db.Passwords {
		if pass == db.MainPassword {
			continue
		}
		item := a.buildSingleRestUserWithNodes(db, pass, inbound, linkHost, stats, nodes)
		list = append(list, item)
	}

	return list
}

func (a *App) buildSingleRestUser(ctx context.Context, db *PasswordsDB, pass string, inbound WdttInboundConfig, linkHost string, stats *ServerStats) RestUserItem {
	var nodes []*paneldb.Node
	if m, err := paneldb.GetDefaultMongo(); err == nil && m != nil {
		nodes, _ = m.ListNodes(ctx)
	}
	return a.buildSingleRestUserWithNodes(db, pass, inbound, linkHost, stats, nodes)
}

func (a *App) buildSingleRestUserWithNodes(db *PasswordsDB, pass string, inbound WdttInboundConfig, linkHost string, stats *ServerStats, nodes []*paneldb.Node) RestUserItem {
	entry := db.Passwords[pass]
	if entry == nil {
		entry = &PasswordEntry{}
	}
	normalizeEntryDevices(entry)

	used := trafficUsed(entry)
	dtlsPort, _, clientPort := resolveUserPorts(entry, inbound)
	rawPort := inbound.EffectiveRawDirectPort()
	subURL := a.buildSubURL(entry.SubID)
	email := strings.TrimSpace(entry.Comment)
	if email == "" {
		email = pass
	}

	nodeLinks := make([]RestNodeLink, 0, len(nodes))
	for _, n := range nodes {
		if !n.IsActive {
			continue
		}
		h := n.Host
		if h == "" {
			h = linkHost
		}
		nodeInbound := inbound
		nodeInbound.DtlsPort = n.DtlsPort
		nodeInbound.RawDirectPort = n.RawPort
		title := n.Name
		if title == "" {
			title = a.cfg.SubTitle
		}
		nl, _ := buildWdttShareLink(h, pass, email, title, "", entry.VkHash, entry, nodeInbound, subURL)
		nodeLinks = append(nodeLinks, RestNodeLink{
			NodeID:   n.ID,
			NodeName: n.Name,
			Host:     h,
			Link:     nl,
		})
	}

	mainLink := buildWdttLink(linkHost, pass, a.cfg.SubTitle, entry.VkHash, entry, inbound, subURL)

	return RestUserItem{
		Password:        maskPassword(pass),
		PasswordKey:     pass,
		Comment:         entry.Comment,
		Active:          !entry.IsDeactivated && !isPasswordExpired(entry) && !trafficExceeded(entry),
		IsDeactivated:   entry.IsDeactivated,
		ExpiresAt:       entry.ExpiresAt,
		ExpiresLabel:    passwordExpiry(entry),
		IsExpired:       isPasswordExpired(entry),
		TotalBytes:      entry.TotalBytes,
		TotalGB:         bytesToGB(entry.TotalBytes),
		UpBytes:         entry.UpBytes,
		DownBytes:       entry.DownBytes,
		TrafficUsed:     used,
		TrafficUsedFmt:  formatBytes(used),
		TrafficExceeded: trafficExceeded(entry),
		DeviceIDs:       entry.DeviceIDs,
		DevicesBound:    len(entry.DeviceIDs),
		MaxDevices:      entryMaxDevices(db, pass, entry),
		Online:          userOnlineFromStats(pass, deviceIDsDisplay(entry), pass == db.MainPassword, stats),
		LastSeenAt:      entry.LastSeenAt,
		VkHash:          entry.VkHash,
		Ports:           entry.Ports,
		DtlsPort:        dtlsPort,
		RawPort:         rawPort,
		ClientPort:      clientPort,
		SubID:           entry.SubID,
		SubURL:          subURL,
		Link:            mainLink,
		NodeLinks:       nodeLinks,
		IsMain:          pass == db.MainPassword,
	}
}

// handleRestNodesRoot handles GET /api/nodes and POST /api/nodes.
func (a *App) handleRestNodesRoot(w http.ResponseWriter, r *http.Request) {
	setRestCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	authWrapper := a.requireRestAuth(func(w http.ResponseWriter, r *http.Request) {
		m, err := paneldb.GetDefaultMongo()
		if err != nil || m == nil {
			restError(w, http.StatusServiceUnavailable, "MongoDB not configured for cluster nodes")
			return
		}

		switch r.Method {
		case http.MethodGet:
			nodes, err := m.ListNodes(r.Context())
			if err != nil {
				restError(w, http.StatusInternalServerError, err.Error())
				return
			}
			restJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"total":   len(nodes),
				"nodes":   nodes,
			})

		case http.MethodPost:
			var n paneldb.Node
			if err := readJSON(r, &n); err != nil {
				restError(w, http.StatusBadRequest, err.Error())
				return
			}
			if strings.TrimSpace(n.ID) == "" {
				n.ID = randomHex(8)
			}
			if strings.TrimSpace(n.ApiKey) == "" {
				n.ApiKey = randomHex(16)
			}
			if err := m.SaveNode(r.Context(), &n); err != nil {
				restError(w, http.StatusInternalServerError, err.Error())
				return
			}
			restJSON(w, http.StatusCreated, map[string]interface{}{
				"success": true,
				"message": "node saved",
				"node":    n,
			})

		default:
			restError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
	authWrapper(w, r)
}

// handleRestNodeItem handles DELETE /api/nodes/{id} and POST /api/nodes/{id}/ping.
func (a *App) handleRestNodeItem(w http.ResponseWriter, r *http.Request) {
	setRestCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	authWrapper := a.requireRestAuth(func(w http.ResponseWriter, r *http.Request) {
		m, err := paneldb.GetDefaultMongo()
		if err != nil || m == nil {
			restError(w, http.StatusServiceUnavailable, "MongoDB not available")
			return
		}

		path := r.URL.Path
		idx := strings.Index(path, "/nodes/")
		if idx == -1 {
			restError(w, http.StatusNotFound, "not found")
			return
		}
		sub := strings.Trim(path[idx+len("/nodes/"):], "/")
		parts := strings.Split(sub, "/")
		nodeID := parts[0]
		action := ""
		if len(parts) > 1 {
			action = parts[1]
		}

		if action == "ping" && r.Method == http.MethodPost {
			n, err := m.GetNode(r.Context(), nodeID)
			if err != nil || n == nil {
				restError(w, http.StatusNotFound, "node not found")
				return
			}
			conn, pingErr := net.DialTimeout("udp", net.JoinHostPort(n.Host, strconv.Itoa(n.DtlsPort)), 2*time.Second)
			reachable := pingErr == nil
			if conn != nil {
				_ = conn.Close()
			}
			restJSON(w, http.StatusOK, map[string]interface{}{
				"success":   true,
				"node_id":   n.ID,
				"host":      n.Host,
				"reachable": reachable,
			})
			return
		}

		if action == "" && r.Method == http.MethodDelete {
			if nodeID == "local" {
				restError(w, http.StatusBadRequest, "cannot delete local primary node")
				return
			}
			if err := m.DeleteNode(r.Context(), nodeID); err != nil {
				restError(w, http.StatusInternalServerError, err.Error())
				return
			}
			restJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"message": "node deleted",
			})
			return
		}

		restError(w, http.StatusNotFound, "not found")
	})
	authWrapper(w, r)
}

// handleRestStatus handles GET /api/status.
func (a *App) handleRestStatus(w http.ResponseWriter, r *http.Request) {
	stats := loadServerStats()
	inbound, _ := loadWdttInbound()
	db, _ := loadPasswords()

	activeUsers := 0
	if db != nil {
		activeUsers = countActivePasswords(db)
	}

	var nodeCount int
	if m, err := paneldb.GetDefaultMongo(); err == nil && m != nil {
		if nodes, err := m.ListNodes(r.Context()); err == nil {
			nodeCount = len(nodes)
		}
	}

	restJSON(w, http.StatusOK, map[string]interface{}{
		"success":           true,
		"version":           panelVersion,
		"uptime":            readOSUptime(),
		"active_users":      activeUsers,
		"sessions":          stats.Sessions,
		"dtls_port":         inbound.DtlsPort,
		"raw_port":          inbound.EffectiveRawDirectPort(),
		"client_port":       inbound.ClientPort,
		"server_ip":         a.defaultLinkHost(),
		"default_link_host": a.defaultLinkHost(),
		"cluster_nodes":     nodeCount,
		"api_key_enabled":   a.cfg.ApiKey != "",
	})
}

// handleRestRestart handles POST /api/system/restart.
func (a *App) handleRestRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		restError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	if err := controlService("wdtt", "restart"); err != nil {
		restError(w, http.StatusInternalServerError, err.Error())
		return
	}
	restJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "WDTT service restarted successfully",
	})
}
