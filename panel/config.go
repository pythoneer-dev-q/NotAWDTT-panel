package panel

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ildarmaga/wdtt/pkg/paneldb"
	"golang.org/x/crypto/bcrypt"
)

var (
	panelConfigPath   = "/etc/wdtt/panel.json"
	panelXrayMetaPath = "/etc/wdtt/panel-xray.json"
	wdttConfigDir     = "/etc/wdtt"
)

const (
	// WDTT VPN-сервер
	wdttServerBin     = "/usr/local/bin/wdtt-app"
	wdttXrayRulesPath = "/usr/local/bin/wdtt-xray-rules.sh"
	wdttServiceUnit   = "wdtt.service"

	// WDTT xray
	xrayConfigDir   = "/etc/wdtt-xray"
	xrayConfigPath  = "/etc/wdtt-xray/config.json"
	xrayBinDir      = "/usr/local/wdtt-xray/bin"
	xrayAssetDir    = "/usr/local/wdtt-xray/bin"
	xrayLogDir      = "/var/log/wdtt-xray"
	xrayServiceUnit = "wdtt-xray.service"

	panelServiceUnit = "wdtt-panel.service"
)

type PanelConfig struct {
	Username           string `json:"username"`
	PasswordHash       string `json:"password_hash"`
	Port               int    `json:"port"`
	WebBasePath        string `json:"web_base_path"`
	SessionKey         string `json:"session_key"`
	ApiKey             string `json:"apiKey,omitempty"`
	WebListen          string `json:"webListen,omitempty"`
	WebDomain          string `json:"webDomain,omitempty"`
	WebCertFile        string `json:"webCertFile,omitempty"`
	WebKeyFile         string `json:"webKeyFile,omitempty"`
	SessionMaxAge      int    `json:"sessionMaxAge,omitempty"`
	PageSize           int    `json:"pageSize,omitempty"`
	RemarkModel        string `json:"remarkModel,omitempty"`
	BlockPing          bool   `json:"blockPing,omitempty"`
	SubEnable          bool   `json:"subEnable,omitempty"`
	SubListen          string `json:"subListen,omitempty"`
	SubPort            int    `json:"subPort,omitempty"`
	SubPath            string `json:"subPath,omitempty"`
	SubDomain          string `json:"subDomain,omitempty"`
	SubCertFile        string `json:"subCertFile,omitempty"`
	SubKeyFile         string `json:"subKeyFile,omitempty"`
	SubEncrypt         bool   `json:"subEncrypt,omitempty"`
	SubTitle           string `json:"subTitle,omitempty"`
	SubSupportURL      string `json:"subSupportUrl,omitempty"`
	SubProfileURL      string `json:"subProfileUrl,omitempty"`
	SubAnnounce        string `json:"subAnnounce,omitempty"`
	SubURI             string `json:"subURI,omitempty"`
	SubShowInfo        bool   `json:"subShowInfo,omitempty"`
	DashboardPollSec   int    `json:"dashboardPollSec,omitempty"`
	UsersPollSec       int    `json:"usersPollSec,omitempty"`
	ConnectionsPollSec int    `json:"connectionsPollSec,omitempty"`
}

func loadPanelConfig() (*PanelConfig, error) {
	if !panelDBEnabled() {
		return nil, fmt.Errorf("panel database not available")
	}
	cfg, err := loadPanelConfigFromDB()
	if err == nil {
		return finalizePanelConfig(cfg)
	}
	if errors.Is(err, os.ErrNotExist) {
		return createDefaultPanelConfig()
	}
	return nil, err
}

// ResetAdminCredentials is used by the local CLI; the password is never passed as a process argument.
func ResetAdminCredentials(username, password string) error {
	username = strings.TrimSpace(username)
	if password == "" {
		return fmt.Errorf("password is required")
	}
	if len([]byte(password)) > 72 {
		return fmt.Errorf("password is too long (bcrypt limit: 72 bytes)")
	}
	cfg, err := loadPanelConfig()
	if err != nil {
		return err
	}
	if username == "" {
		username = cfg.Username
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	cfg.Username = username
	cfg.PasswordHash = string(hash)
	cfg.SessionKey = randomHex(32)
	if err := saveAdminConfig(cfg); err != nil {
		return err
	}
	stored, err := loadPanelConfigFromDB()
	if err != nil {
		return fmt.Errorf("verify admin password: %w", err)
	}
	if stored.Username != username || bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte(password)) != nil {
		return fmt.Errorf("admin password was not persisted")
	}
	return nil
}

func saveAdminConfig(cfg *PanelConfig) error {
	if strings.TrimSpace(os.Getenv("MONGODB_URI")) != "" || strings.TrimSpace(os.Getenv("MONGO_URI")) != "" || strings.TrimSpace(os.Getenv("DATABASE_URL")) != "" {
		if _, err := paneldb.GetDefaultMongo(); err != nil {
			return fmt.Errorf("MongoDB unavailable; admin credentials were not changed: %w", err)
		}
	}
	if err := savePanelConfig(cfg); err != nil {
		return err
	}
	stored, err := loadPanelConfigFromDB()
	if err != nil {
		return fmt.Errorf("verify saved admin config: %w", err)
	}
	if stored.Username != cfg.Username || stored.PasswordHash != cfg.PasswordHash || stored.SessionKey != cfg.SessionKey {
		return fmt.Errorf("admin credentials were not persisted")
	}
	return nil
}

func finalizePanelConfig(cfg *PanelConfig) (*PanelConfig, error) {
	if cfg.Port == 0 {
		cfg.Port = 2860
	}
	if cfg.WebBasePath == "" {
		cfg.WebBasePath = "/wdtt/"
	}
	if cfg.SessionKey == "" {
		cfg.SessionKey = randomHex(32)
		if err := savePanelConfig(cfg); err != nil {
			return nil, err
		}
	}
	if cfg.ApiKey == "" {
		cfg.ApiKey = randomHex(24)
		_ = savePanelConfig(cfg)
	}
	normalizePanelConfig(cfg)
	syncPanelPollDefaults(cfg)
	return cfg, nil
}

func normalizePanelConfig(cfg *PanelConfig) {
	if envPort := strings.TrimSpace(os.Getenv("WDTT_PANEL_PORT")); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil && p > 0 {
			cfg.Port = p
		}
	} else if envPort := strings.TrimSpace(os.Getenv("PORT")); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil && p > 0 {
			cfg.Port = p
		}
	}
	if envListen := strings.TrimSpace(os.Getenv("WDTT_PANEL_LISTEN")); envListen != "" {
		cfg.WebListen = envListen
	}
	if envSubPort := strings.TrimSpace(os.Getenv("WDTT_SUB_PORT")); envSubPort != "" {
		if p, err := strconv.Atoi(envSubPort); err == nil && p > 0 {
			cfg.SubPort = p
		}
	}
	if cfg.Port == 0 {
		cfg.Port = 2860
	}
	if cfg.WebBasePath == "" {
		cfg.WebBasePath = "/wdtt/"
	}
	if cfg.SessionMaxAge <= 0 {
		cfg.SessionMaxAge = 60
	}
	if cfg.PageSize <= 0 {
		cfg.PageSize = 50
	}
	if cfg.RemarkModel == "" {
		cfg.RemarkModel = "-ieo"
	}
	cfg.RemarkModel = normalizeRemarkModel(cfg.RemarkModel)
	normalizeSubConfig(cfg)
	cfg.DashboardPollSec = clampDashboardPollSec(cfg.DashboardPollSec)
	if cfg.DashboardPollSec == 0 {
		cfg.DashboardPollSec = defaultDashboardPollSec
	}
	cfg.UsersPollSec = clampPagePollSec(cfg.UsersPollSec)
	if cfg.UsersPollSec == 0 {
		cfg.UsersPollSec = defaultUsersPollSec
	}
	cfg.ConnectionsPollSec = clampPagePollSec(cfg.ConnectionsPollSec)
	if cfg.ConnectionsPollSec == 0 {
		cfg.ConnectionsPollSec = defaultConnectionsPollSec
	}
}

func normalizeSubConfig(cfg *PanelConfig) {
	if cfg == nil {
		return
	}
	cfg.SubPath = normalizeSubPath(cfg.SubPath)
	if cfg.SubPort <= 0 {
		cfg.SubPort = 2096
	}
}

// sanitizePanelCertPaths сбрасывает пути, если файлы удалены (иначе save settings падает на validatePanelTLS).
func sanitizePanelCertPaths(cfg *PanelConfig) bool {
	if cfg == nil {
		return false
	}
	certFile := strings.TrimSpace(cfg.WebCertFile)
	keyFile := strings.TrimSpace(cfg.WebKeyFile)
	if certFile == "" && keyFile == "" {
		return false
	}
	if certFile != "" {
		if _, err := os.Stat(certFile); err != nil {
			cfg.WebCertFile = ""
			cfg.WebKeyFile = ""
			return true
		}
	}
	if keyFile != "" {
		if _, err := os.Stat(keyFile); err != nil {
			cfg.WebCertFile = ""
			cfg.WebKeyFile = ""
			return true
		}
	}
	return false
}

// syncSubCertFromPanel прописывает TLS подписки из сертификата панели, если свои пути пусты.
func syncSubCertFromPanel(cfg *PanelConfig) bool {
	if cfg == nil {
		return false
	}
	cert := strings.TrimSpace(cfg.WebCertFile)
	key := strings.TrimSpace(cfg.WebKeyFile)
	if cert == "" || key == "" {
		return false
	}
	changed := false
	if strings.TrimSpace(cfg.SubCertFile) == "" {
		cfg.SubCertFile = cert
		changed = true
	}
	if strings.TrimSpace(cfg.SubKeyFile) == "" {
		cfg.SubKeyFile = key
		changed = true
	}
	return changed
}

func panelCertPanelMap(cfg *PanelConfig) map[string]interface{} {
	if cfg == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"webCertFile": cfg.WebCertFile,
		"webKeyFile":  cfg.WebKeyFile,
		"webDomain":   cfg.WebDomain,
		"subCertFile": cfg.SubCertFile,
		"subKeyFile":  cfg.SubKeyFile,
		"tlsActive":   panelTLSEnabled(cfg),
	}
}

func normalizeRemarkModel(s string) string {
	if len(s) < 2 {
		return "-ieo"
	}
	sep := s[0]
	seen := make(map[byte]bool)
	parts := make([]byte, 0, len(s)-1)
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c != 'i' && c != 'e' && c != 'o' {
			continue
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		parts = append(parts, c)
	}
	if len(parts) == 0 {
		return "-ieo"
	}
	return string(append([]byte{sep}, parts...))
}

func createDefaultPanelConfig() (*PanelConfig, error) {
	user := "admin"
	if envUser := strings.TrimSpace(os.Getenv("WDTT_PANEL_USER")); envUser != "" {
		user = envUser
	}
	pass := "wdtt"
	if envPass := strings.TrimSpace(os.Getenv("WDTT_PANEL_PASSWORD")); envPass != "" {
		pass = envPass
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	port := 2860
	if envPort := strings.TrimSpace(os.Getenv("WDTT_PANEL_PORT")); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil && p > 0 {
			port = p
		}
	} else if envPort := strings.TrimSpace(os.Getenv("PORT")); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil && p > 0 {
			port = p
		}
	}
	cfg := &PanelConfig{
		Username:           user,
		PasswordHash:       string(hash),
		Port:               port,
		WebBasePath:        "/wdtt/",
		SessionKey:         randomHex(32),
		SessionMaxAge:      60,
		PageSize:           50,
		RemarkModel:        "-ieo",
		SubEnable:          true,
		SubPort:            2096,
		SubPath:            "/sub/",
		SubEncrypt:         true,
		SubShowInfo:        true,
		DashboardPollSec:   defaultDashboardPollSec,
		UsersPollSec:       defaultUsersPollSec,
		ConnectionsPollSec: defaultConnectionsPollSec,
	}
	if err := savePanelConfig(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func savePanelConfig(cfg *PanelConfig) error {
	if !panelDBEnabled() {
		return fmt.Errorf("panel database not available")
	}
	return savePanelConfigToDB(cfg)
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (c *PanelConfig) sessionDuration() time.Duration {
	if c.SessionMaxAge > 0 {
		return time.Duration(c.SessionMaxAge) * time.Minute
	}
	return 24 * time.Hour
}

func panelSettingsMap(cfg *PanelConfig) map[string]interface{} {
	return map[string]interface{}{
		"webListen":          cfg.WebListen,
		"webDomain":          cfg.WebDomain,
		"webPort":            cfg.Port,
		"webBasePath":        cfg.WebBasePath,
		"sessionMaxAge":      cfg.SessionMaxAge,
		"pageSize":           cfg.PageSize,
		"remarkModel":        cfg.RemarkModel,
		"webCertFile":        cfg.WebCertFile,
		"webKeyFile":         cfg.WebKeyFile,
		"twoFactorEnable":    false,
		"blockPing":          cfg.BlockPing,
		"blockPingLive":      ufwPingBlocked(),
		"ufwInstalled":       ufwInstalled(),
		"ufwActive":          ufwActive(),
		"sshPort":            detectSSHPort(),
		"dbEnabled":          panelDBEnabled(),
		"dbPath":             panelDBPath,
		"subEnable":          cfg.SubEnable,
		"subListen":          cfg.SubListen,
		"subPort":            cfg.SubPort,
		"subPath":            cfg.SubPath,
		"subDomain":          cfg.SubDomain,
		"subCertFile":        cfg.SubCertFile,
		"subKeyFile":         cfg.SubKeyFile,
		"subEncrypt":         cfg.SubEncrypt,
		"subTitle":           cfg.SubTitle,
		"subSupportUrl":      cfg.SubSupportURL,
		"subProfileUrl":      cfg.SubProfileURL,
		"subAnnounce":        cfg.SubAnnounce,
		"subURI":             cfg.SubURI,
		"subShowInfo":        cfg.SubShowInfo,
		"dashboardPollSec":   cfg.DashboardPollSec,
		"usersPollSec":       cfg.UsersPollSec,
		"connectionsPollSec": cfg.ConnectionsPollSec,
	}
}

func (c *PanelConfig) basePath() string {
	p := c.WebBasePath
	if p == "" {
		return "/wdtt/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	if p[len(p)-1] != '/' {
		p += "/"
	}
	return p
}
