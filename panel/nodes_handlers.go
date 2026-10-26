package panel

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ildarmaga/wdtt/pkg/paneldb"
)

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func localNodeEnabled() bool {
	return os.Getenv("WDTT_INSTALL_MODE") != "panel"
}

var installerHostPattern = regexp.MustCompile(`^[A-Za-z0-9.:[\]-]+$`)

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (a *App) serveNodesPage(w http.ResponseWriter, r *http.Request) {
	a.renderHTML(w, r, "nodes.html", "Ноды (VPS)", pageData{
		"request_uri": a.cfg.basePath() + "panel/nodes",
		"base_path":   a.cfg.basePath(),
	})
}

func (a *App) handleNodesList(w http.ResponseWriter, r *http.Request) {
	m, err := paneldb.GetDefaultMongo()
	if err != nil {
		jsonError(w, "MongoDB недоступна: "+err.Error(), 500)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	nodes, err := m.ListNodes(ctx)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	if !localNodeEnabled() {
		filtered := nodes[:0]
		for _, n := range nodes {
			if !n.IsLocal {
				filtered = append(filtered, n)
			}
		}
		nodes = filtered
	}

	// Обновляем статус локальной ноды
	srvIP := a.serverIP()
	now := time.Now().Unix()
	for _, n := range nodes {
		if !n.IsActive {
			n.Status = "disabled"
			continue
		}
		if n.IsLocal {
			if n.Host == "" {
				n.Host = srvIP
			}
			n.LastSeenAt = now
			n.Status = "online"
		} else {
			// Если нода не слала пинг более 60 секунд - помечаем offline
			if now-n.LastSeenAt > 60 {
				n.Status = "offline"
			}
		}
	}

	jsonOK(w, map[string]interface{}{
		"nodes":     nodes,
		"server_ip": srvIP,
	})
}

type nodeSaveReq struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	DtlsPort int    `json:"dtls_port"`
	RawPort  int    `json:"raw_port"`
	ApiKey   string `json:"api_key"`
	IsActive bool   `json:"is_active"`
}

func (a *App) handleNodeSave(w http.ResponseWriter, r *http.Request) {
	var req nodeSaveReq
	if err := readJSON(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Host = strings.TrimSpace(req.Host)
	if req.Name == "" {
		jsonError(w, "Укажите имя ноды", 400)
		return
	}
	if req.Host == "" {
		jsonError(w, "Укажите IP или домен ноды", 400)
		return
	}
	if req.DtlsPort <= 0 {
		req.DtlsPort = 56000
	}
	if req.RawPort <= 0 {
		req.RawPort = 56003
	}

	m, err := paneldb.GetDefaultMongo()
	if err != nil {
		jsonError(w, "MongoDB недоступна: "+err.Error(), 500)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	id := strings.TrimSpace(req.ID)
	var existing *paneldb.Node
	if id != "" {
		existing, _ = m.GetNode(ctx, id)
	}

	if existing == nil {
		// Создание новой ноды
		id = "node_" + randomToken(4)
		apiKey := "key_" + randomToken(16)
		node := &paneldb.Node{
			ID:          id,
			Name:        req.Name,
			Host:        req.Host,
			DtlsPort:    req.DtlsPort,
			RawPort:     req.RawPort,
			ApiKey:      apiKey,
			IsActive:    true,
			IsLocal:     false,
			LastSeenAt:  0,
			Status:      "offline",
			OnlineUsers: 0,
			CreatedAt:   time.Now().Unix(),
		}
		if err := m.SaveNode(ctx, node); err != nil {
			jsonError(w, err.Error(), 500)
			return
		}
		jsonOK(w, node)
		return
	}

	// Обновление существующей
	if !existing.IsLocal {
		existing.Name = req.Name
		existing.Host = req.Host
		existing.DtlsPort = req.DtlsPort
		existing.RawPort = req.RawPort
		existing.IsActive = req.IsActive
	} else {
		existing.Name = req.Name
		existing.DtlsPort = req.DtlsPort
		existing.RawPort = req.RawPort
	}

	if err := m.SaveNode(ctx, existing); err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, existing)
}

func (a *App) handleNodeDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}

	if req.ID == "local" || req.ID == "" {
		jsonError(w, "Нельзя удалить мастер-ноду", 400)
		return
	}

	m, err := paneldb.GetDefaultMongo()
	if err != nil {
		jsonError(w, "MongoDB недоступна: "+err.Error(), 500)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := m.DeleteNode(ctx, req.ID); err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, "Нода удалена")
}

func (a *App) handleNodePing(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}

	host := strings.TrimSpace(req.Host)
	port := req.Port
	if port <= 0 {
		port = 56000
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	_, err := net.ResolveUDPAddr("udp", addr)

	if err != nil {
		jsonOK(w, map[string]interface{}{
			"resolved": false,
			"error":    err.Error(),
		})
		return
	}
	jsonOK(w, map[string]interface{}{
		"resolved": true,
	})
}

// handleNodeSync — endpoint для внешних нод для получения актуального списка паролей и настроек
func (a *App) handleNodeSync(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	m, err := paneldb.GetDefaultMongo()
	if err != nil {
		http.Error(w, "db error", 500)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	nodes, err := m.ListNodes(ctx)
	if err != nil {
		http.Error(w, "db error", 500)
		return
	}

	var matchedNode *paneldb.Node
	for _, n := range nodes {
		if n.ApiKey == token && n.IsActive {
			matchedNode = n
			break
		}
	}
	if matchedNode == nil {
		http.Error(w, "invalid or disabled node token", http.StatusForbidden)
		return
	}

	store, err := m.LoadStore(ctx)
	if err != nil {
		http.Error(w, "db error", 500)
		return
	}

	inbound, _ := m.LoadInbound(ctx)

	passwords := make([]string, 0, len(store.Users))
	for p, u := range store.Users {
		if u != nil && !u.IsDeactivated {
			passwords = append(passwords, p)
		}
	}

	jsonOK(w, map[string]interface{}{
		"node_id":   matchedNode.ID,
		"dtls_port": matchedNode.DtlsPort,
		"raw_port":  matchedNode.RawPort,
		"passwords": passwords,
		"dns":       inbound.DNS,
		"mtu":       inbound.MTU,
	})
}

// handleNodeHeartbeat — endpoint для внешних нод: отчет о здоровье и трафике
func (a *App) handleNodeHeartbeat(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		OnlineUsers int   `json:"online_users"`
		TrafficUp   int64 `json:"traffic_up"`
		TrafficDown int64 `json:"traffic_down"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}

	m, err := paneldb.GetDefaultMongo()
	if err != nil {
		http.Error(w, "db error", 500)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := m.UpdateNodeHeartbeat(ctx, token, req.OnlineUsers, req.TrafficUp, req.TrafficDown); err != nil {
		http.Error(w, err.Error(), 404)
		return
	}

	jsonOK(w, map[string]bool{"ok": true})
}

// handleNodeBinary — отдаёт скомпилированный бинарник WDTT для подключаемых VPS-нод
func (a *App) handleNodeBinary(w http.ResponseWriter, r *http.Request) {
	candidates := []string{
		"/usr/local/bin/wdtt-app",
		"/usr/local/bin/wdtt",
		"/etc/wdtt/bin/wdtt",
		"wdtt-linux-amd64",
		os.Args[0],
	}
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Size() > 500000 {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", "attachment; filename=\"wdtt\"")
			http.ServeFile(w, r, p)
			return
		}
	}
	http.Error(w, "binary not available", http.StatusNotFound)
}

// handleNodeInstallScript — возвращает скрипт подключения для нового VPS
func (a *App) handleNodeInstallScript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !installerHostPattern.MatchString(r.Host) {
		http.Error(w, "invalid host", http.StatusBadRequest)
		return
	}
	token := r.URL.Query().Get("token")
	nodeID := r.URL.Query().Get("id")
	if token == "" || nodeID == "" {
		http.Error(w, "missing node credentials", http.StatusBadRequest)
		return
	}
	m, err := paneldb.GetDefaultMongo()
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	node, err := m.GetNode(ctx, nodeID)
	if err != nil || node == nil || node.IsLocal || !node.IsActive || subtle.ConstantTimeCompare([]byte(node.ApiKey), []byte(token)) != 1 {
		http.Error(w, "invalid or disabled node", http.StatusForbidden)
		return
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	masterURL := fmt.Sprintf("%s://%s%spanel/api", scheme, r.Host, a.cfg.basePath())

	script := fmt.Sprintf(`#!/usr/bin/env bash
set -e
echo "═══════════════════════════════════════════════════════"
echo "    Подключение VPS-ноды к панели WDTT"
echo "═══════════════════════════════════════════════════════"

NODE_TOKEN=%s
NODE_ID=%s
MASTER_URL=%s

echo "[1/4] Обновление пакетов и установка зависимостей..."
apt-get update -y && apt-get install -y curl iptables tar ca-certificates

echo "[2/4] Загрузка бинарника WDTT..."
mkdir -p /etc/wdtt /usr/local/bin
curl -fsSL "${MASTER_URL}/node/binary" -o /usr/local/bin/wdtt.new
chmod +x /usr/local/bin/wdtt.new
mv /usr/local/bin/wdtt.new /usr/local/bin/wdtt

echo "[3/4] Создание конфигурации ноды..."
cat <<EOF > /etc/wdtt/node.conf
MASTER_URL=${MASTER_URL}
NODE_TOKEN=${NODE_TOKEN}
NODE_ID=${NODE_ID}
WDTT_DTLS_PORT=%d
WDTT_RAW_PORT=%d
EOF
chmod 600 /etc/wdtt/node.conf

echo "[4/4] Настройка службы wdtt-node..."
cat <<EOF > /etc/systemd/system/wdtt-node.service
[Unit]
Description=WDTT Node Worker
After=network.target

[Service]
Type=simple
EnvironmentFile=/etc/wdtt/node.conf
ExecStart=/usr/local/bin/wdtt -no-panel -node-config /etc/wdtt/node.conf
Restart=always
RestartSec=3
LimitNOFILE=65535
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE CAP_NET_RAW

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable wdtt-node.service
systemctl restart wdtt-node.service

echo "✓ Нода успешно подключена и запущена!"
`, shellQuote(token), shellQuote(nodeID), shellQuote(masterURL), node.DtlsPort, node.RawPort)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(script))
}
