package panel

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	texttmpl "text/template"
	"time"

	"github.com/ildarmaga/wdtt/pkg/paneldb"
)

//go:embed node-install.sh.tmpl node-cli.sh node-mtu-rules.sh
var nodeInstallFiles embed.FS

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func localNodeEnabled() bool {
	return os.Getenv("WDTT_INSTALL_MODE") != "panel"
}

var installerHostPattern = regexp.MustCompile(`^[A-Za-z0-9.:[\]-]+$`)
var installerPathPattern = regexp.MustCompile(`^/[A-Za-z0-9/_-]*/$`)
var certFingerprintPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var nodeVersionPattern = regexp.MustCompile(`^[A-Za-z0-9._+\-]{0,64}$`)
var buildCommitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

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
	srvIP := strings.TrimSpace(a.cfg.panelDomain())
	if srvIP == "" {
		if host, _, err := net.SplitHostPort(r.Host); err == nil {
			srvIP = host
		} else {
			srvIP = r.Host
		}
	}
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
				if n.LastSeenAt == 0 && n.InstallRequestedAt > 0 && now-n.InstallRequestedAt < 600 {
					n.Status = "installing"
				} else {
					n.Status = "offline"
				}
			}
		}
	}

	jsonOK(w, map[string]interface{}{
		"nodes":         nodes,
		"server_ip":     srvIP,
		"panel_version": FormatPanelVersion(),
		"panel_commit":  FormatPanelBuildCommit(),
		"cert_pin": func() string {
			if r.TLS == nil || a.cfg.WebCertFile == "" {
				return ""
			}
			pin, _ := panelPublicKeyPin(a.cfg.WebCertFile)
			return pin
		}(),
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
	if req.DtlsPort > 65535 || req.RawPort > 65535 || req.DtlsPort == req.RawPort {
		jsonError(w, "Порты ноды должны быть разными и находиться в диапазоне 1–65535", 400)
		return
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
		if existing.ApiKey == "" {
			existing.ApiKey = "key_" + randomToken(16)
		}
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

func nodeBearerToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return r.URL.Query().Get("token") // legacy nodes
}

func authorizedNode(ctx context.Context, r *http.Request) (*paneldb.Node, error) {
	token := nodeBearerToken(r)
	if token == "" {
		return nil, fmt.Errorf("missing node API key")
	}
	m, err := paneldb.GetDefaultMongo()
	if err != nil {
		return nil, err
	}
	nodes, err := m.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		if node.IsLocal || !node.IsActive {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(node.ApiKey), []byte(token)) == 1 {
			return node, nil
		}
	}
	return nil, fmt.Errorf("invalid or disabled node API key")
}

func panelPublicKeyPin(certPath string) (string, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return "", fmt.Errorf("certificate PEM missing")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	// Public CA certificates use normal trust roots, so routine renewal does
	// not strand a node on a stale public-key pin.
	if !bytes.Equal(cert.RawSubject, cert.RawIssuer) ||
		cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature) != nil {
		return "", nil
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return "sha256//" + base64.StdEncoding.EncodeToString(sum[:]), nil
}

// handleNodeSync — endpoint для внешних нод для получения актуального списка паролей и настроек
func (a *App) handleNodeSync(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	matchedNode, err := authorizedNode(ctx, r)
	if err != nil {
		jsonError(w, "node authentication failed", http.StatusForbidden)
		return
	}
	m, err := paneldb.GetDefaultMongo()
	if err != nil {
		jsonError(w, "db error", 500)
		return
	}

	store, err := m.LoadStore(ctx)
	if err != nil {
		jsonError(w, "db error", 500)
		return
	}

	inbound, err := m.LoadInbound(ctx)
	if err != nil || inbound == nil {
		inbound = &paneldb.Inbound{DNS: "1.1.1.1", MTU: 1280}
	}

	passwords := make([]string, 0, len(store.Users))
	users := make([]map[string]interface{}, 0, len(store.Users))
	for p, u := range store.Users {
		if u != nil && !u.IsDeactivated {
			passwords = append(passwords, p)
			users = append(users, map[string]interface{}{
				"password":      p,
				"max_down_mbps": u.MaxDownMBps,
				"max_up_mbps":   u.MaxUpMBps,
			})
		}
	}

	jsonOK(w, map[string]interface{}{
		"node_id":   matchedNode.ID,
		"dtls_port": matchedNode.DtlsPort,
		"raw_port":  matchedNode.RawPort,
		"passwords": passwords,
		"users":     users,
		"dns":       inbound.DNS,
		"mtu":       inbound.MTU,
	})
}

// handleNodeHeartbeat — endpoint для внешних нод: отчет о здоровье и трафике
func (a *App) handleNodeHeartbeat(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	node, err := authorizedNode(ctx, r)
	if err != nil {
		jsonError(w, "node authentication failed", http.StatusForbidden)
		return
	}

	var req struct {
		OnlineUsers int    `json:"online_users"`
		TrafficUp   int64  `json:"traffic_up"`
		TrafficDown int64  `json:"traffic_down"`
		CertSHA256  string `json:"cert_sha256"`
		Version     string `json:"version"`
		BuildCommit string `json:"build_commit"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, "bad request", 400)
		return
	}
	if req.OnlineUsers < 0 || req.TrafficUp < 0 || req.TrafficDown < 0 ||
		(req.CertSHA256 != "" && !certFingerprintPattern.MatchString(req.CertSHA256)) ||
		!nodeVersionPattern.MatchString(req.Version) ||
		(req.BuildCommit != "" && !buildCommitPattern.MatchString(req.BuildCommit)) {
		jsonError(w, "invalid node status", http.StatusBadRequest)
		return
	}

	m, err := paneldb.GetDefaultMongo()
	if err != nil {
		jsonError(w, "db error", 500)
		return
	}

	if err := m.UpdateNodeHeartbeat(ctx, node.ApiKey, req.OnlineUsers, req.TrafficUp, req.TrafficDown, req.CertSHA256, req.Version, req.BuildCommit); err != nil {
		jsonError(w, err.Error(), 404)
		return
	}

	jsonOK(w, map[string]bool{"ok": true})
}

// handleNodeBinary — отдаёт скомпилированный бинарник WDTT для подключаемых VPS-нод
func (a *App) handleNodeBinary(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if _, err := authorizedNode(ctx, r); err != nil {
		jsonError(w, "node authentication failed", http.StatusForbidden)
		return
	}
	arch := r.URL.Query().Get("arch")
	if arch == "" {
		arch = runtime.GOARCH
	}
	if arch != "amd64" && arch != "arm64" {
		jsonError(w, "unsupported architecture", http.StatusBadRequest)
		return
	}
	if arch != runtime.GOARCH {
		for _, path := range []string{"/etc/wdtt/bin/wdtt-linux-" + arch, "wdtt-linux-" + arch} {
			if fi, err := os.Stat(path); err == nil && !fi.IsDir() && fi.Size() > 500000 {
				w.Header().Set("Content-Type", "application/octet-stream")
				http.ServeFile(w, r, path)
				return
			}
		}
		jsonError(w, "binary for "+arch+" not available on panel", http.StatusConflict)
		return
	}
	candidates := []string{
		"/usr/local/bin/wdtt-app",
		"/etc/wdtt/bin/wdtt",
		"wdtt-linux-" + arch,
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
	jsonError(w, "binary not available", http.StatusNotFound)
}

func (a *App) handleNodeCLI(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if _, err := authorizedNode(ctx, r); err != nil {
		jsonError(w, "node authentication failed", http.StatusForbidden)
		return
	}
	data, err := nodeInstallFiles.ReadFile("node-cli.sh")
	if err != nil {
		jsonError(w, "node CLI unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (a *App) handleNodeMTURules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if _, err := authorizedNode(ctx, r); err != nil {
		jsonError(w, "node authentication failed", http.StatusForbidden)
		return
	}
	data, err := nodeInstallFiles.ReadFile("node-mtu-rules.sh")
	if err != nil {
		jsonError(w, "node MTU rules unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// handleNodeInstallScript — возвращает скрипт подключения для нового VPS
func (a *App) handleNodeInstallScript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !installerHostPattern.MatchString(r.Host) {
		jsonError(w, "invalid host", http.StatusBadRequest)
		return
	}
	if !installerPathPattern.MatchString(a.cfg.basePath()) {
		jsonError(w, "invalid panel base path for node installer", http.StatusBadRequest)
		return
	}
	nodeID := r.URL.Query().Get("id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	node, err := authorizedNode(ctx, r)
	if err != nil || (nodeID != "" && node.ID != nodeID) {
		jsonError(w, "invalid or disabled node", http.StatusForbidden)
		return
	}
	if r.TLS == nil && !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		jsonError(w, "node installation requires HTTPS on the panel", http.StatusBadRequest)
		return
	}
	masterURL := fmt.Sprintf("https://%s%spanel/api", r.Host, a.cfg.basePath())
	certPin := ""
	if r.TLS != nil && a.cfg.WebCertFile != "" {
		certPin, err = panelPublicKeyPin(a.cfg.WebCertFile)
		if err != nil {
			jsonError(w, "panel certificate unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	data, err := nodeInstallFiles.ReadFile("node-install.sh.tmpl")
	if err != nil {
		jsonError(w, "installer unavailable", 500)
		return
	}
	tmpl, err := texttmpl.New("node-install").Funcs(texttmpl.FuncMap{"quote": shellQuote}).Parse(string(data))
	if err != nil {
		jsonError(w, "installer template invalid", 500)
		return
	}
	var script bytes.Buffer
	err = tmpl.Execute(&script, map[string]interface{}{
		"MasterURL": masterURL, "Token": node.ApiKey, "ID": node.ID,
		"Name": node.Name, "CertPin": certPin, "BuildCommit": FormatPanelBuildCommit(),
		"DtlsPort": node.DtlsPort, "RawPort": node.RawPort,
	})
	if err != nil {
		jsonError(w, "installer generation failed", 500)
		return
	}
	m, err := paneldb.GetDefaultMongo()
	if err != nil {
		jsonError(w, "database unavailable", 503)
		return
	}
	node.InstallRequestedAt = time.Now().Unix()
	if err := m.SaveNode(ctx, node); err != nil {
		jsonError(w, "database unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Disposition", "attachment; filename=\"wdtt-node-install.sh\"")
	_, _ = w.Write(script.Bytes())
}
