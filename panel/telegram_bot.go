package panel

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ildarmaga/wdtt/pkg/sharelink"
)

const (
	telegramMaxResponseBytes = 2 << 20
	botUsersPageSize         = 8
)

type telegramBotConfig struct {
	Enabled          bool
	Token            string
	AdminIDs         map[int64]struct{}
	PanelURL         string
	APIKey           string
	TelegramAPI      string
	UserPrefix       string
	Public           bool
	CopyNodeLimit    int
	RequestTimeout   time.Duration
	DefaultDays      int
	DefaultTrafficGB float64
	DefaultDevices   int
}

func loadTelegramBotConfig(panelCfg *PanelConfig) (telegramBotConfig, error) {
	cfg := telegramBotConfig{
		Enabled:          envBool("WDTT_BOT_ENABLED", false),
		Token:            strings.TrimSpace(os.Getenv("WDTT_BOT_TOKEN")),
		AdminIDs:         make(map[int64]struct{}),
		TelegramAPI:      strings.TrimRight(envString("WDTT_BOT_TELEGRAM_API", "https://api.telegram.org"), "/"),
		UserPrefix:       envString("WDTT_BOT_USER_PREFIX", "tg_"),
		Public:           envBool("WDTT_BOT_PUBLIC", true),
		CopyNodeLimit:    envInt("WDTT_BOT_COPY_NODE_LIMIT", 5),
		RequestTimeout:   time.Duration(envInt("WDTT_BOT_REQUEST_TIMEOUT_SEC", 15)) * time.Second,
		DefaultDays:      envInt("WDTT_BOT_DEFAULT_DAYS", 30),
		DefaultTrafficGB: envFloat("WDTT_BOT_DEFAULT_TRAFFIC_GB", 100),
		DefaultDevices:   envInt("WDTT_BOT_DEFAULT_DEVICES", 2),
	}
	for _, raw := range strings.FieldsFunc(os.Getenv("WDTT_BOT_ADMIN_IDS"), func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	}) {
		id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || id <= 0 {
			return cfg, fmt.Errorf("WDTT_BOT_ADMIN_IDS contains invalid Telegram ID %q", raw)
		}
		cfg.AdminIDs[id] = struct{}{}
	}

	panelURL := strings.TrimSpace(os.Getenv("WDTT_BOT_PANEL_URL"))
	if panelURL == "" {
		port := 2860
		if panelCfg != nil && panelCfg.Port > 0 {
			port = panelCfg.Port
		}
		panelURL = fmt.Sprintf("http://127.0.0.1:%d/api", port)
	}
	cfg.PanelURL = strings.TrimRight(panelURL, "/")
	cfg.APIKey = strings.TrimSpace(os.Getenv("WDTT_BOT_API_KEY"))
	if cfg.APIKey == "" && panelCfg != nil {
		cfg.APIKey = strings.TrimSpace(panelCfg.ApiKey)
	}

	if cfg.CopyNodeLimit < 1 {
		cfg.CopyNodeLimit = 1
	}
	if cfg.CopyNodeLimit > 5 {
		cfg.CopyNodeLimit = 5
	}
	if cfg.RequestTimeout < 3*time.Second {
		cfg.RequestTimeout = 3 * time.Second
	}
	if cfg.RequestTimeout > 60*time.Second {
		cfg.RequestTimeout = 60 * time.Second
	}
	if cfg.DefaultDays < 0 {
		cfg.DefaultDays = 0
	}
	if cfg.DefaultTrafficGB < 0 {
		cfg.DefaultTrafficGB = 0
	}
	if cfg.DefaultDevices < 0 {
		cfg.DefaultDevices = 0
	}

	if !cfg.Enabled {
		return cfg, nil
	}
	if cfg.Token == "" {
		return cfg, errors.New("WDTT_BOT_ENABLED=true, but WDTT_BOT_TOKEN is empty")
	}
	if len(cfg.AdminIDs) == 0 {
		return cfg, errors.New("WDTT_BOT_ENABLED=true, but WDTT_BOT_ADMIN_IDS is empty")
	}
	if cfg.APIKey == "" {
		return cfg, errors.New("panel API key is empty")
	}
	if err := validateHTTPURL(cfg.PanelURL, "WDTT_BOT_PANEL_URL"); err != nil {
		return cfg, err
	}
	if err := validateHTTPURL(cfg.TelegramAPI, "WDTT_BOT_TELEGRAM_API"); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envFloat(name string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func validateHTTPURL(raw, envName string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%s must be an absolute HTTP(S) URL", envName)
	}
	return nil
}

type botPanelClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

type botUser struct {
	Password        string        `json:"password"`
	PasswordKey     string        `json:"password_key"`
	Comment         string        `json:"comment"`
	Active          bool          `json:"active"`
	IsDeactivated   bool          `json:"is_deactivated"`
	ExpiresAt       int64         `json:"expires_at"`
	ExpiresLabel    string        `json:"expires_label"`
	IsExpired       bool          `json:"is_expired"`
	TotalBytes      int64         `json:"total_bytes"`
	TotalGB         float64       `json:"total_gb"`
	UpBytes         int64         `json:"up_bytes"`
	DownBytes       int64         `json:"down_bytes"`
	TrafficUsed     int64         `json:"traffic_used"`
	TrafficUsedFmt  string        `json:"traffic_used_fmt"`
	TrafficExceeded bool          `json:"traffic_exceeded"`
	DeviceIDs       []string      `json:"device_ids"`
	DevicesBound    int           `json:"devices_bound"`
	MaxDevices      int           `json:"max_devices"`
	MaxDownMBps     float64       `json:"max_down_mbps"`
	MaxUpMBps       float64       `json:"max_up_mbps"`
	Online          bool          `json:"online"`
	LastSeenAt      int64         `json:"last_seen_at"`
	SubURL          string        `json:"sub_url"`
	Link            string        `json:"link"`
	NodeLinks       []botNodeLink `json:"node_links"`
	IsMain          bool          `json:"is_main"`
}

type botNodeLink struct {
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
	Host     string `json:"host"`
	Link     string `json:"link"`
}

type botNode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	DtlsPort    int    `json:"dtls_port"`
	RawPort     int    `json:"raw_port"`
	IsActive    bool   `json:"is_active"`
	IsLocal     bool   `json:"is_local"`
	LastSeenAt  int64  `json:"last_seen_at"`
	Version     string `json:"version"`
	BuildCommit string `json:"build_commit"`
	Status      string `json:"status"`
	OnlineUsers int    `json:"online_users"`
	TrafficUp   int64  `json:"traffic_up"`
	TrafficDown int64  `json:"traffic_down"`
}

type botSystemStatus struct {
	Version       string  `json:"version"`
	Uptime        float64 `json:"uptime"`
	ActiveUsers   int     `json:"active_users"`
	Sessions      int     `json:"sessions"`
	DtlsPort      int     `json:"dtls_port"`
	RawPort       int     `json:"raw_port"`
	ClientPort    int     `json:"client_port"`
	ServerIP      string  `json:"server_ip"`
	ClusterNodes  int     `json:"cluster_nodes"`
	APIKeyEnabled bool    `json:"api_key_enabled"`
}

type botUsersResponse struct {
	Success bool      `json:"success"`
	Total   int       `json:"total"`
	Count   int       `json:"count"`
	Users   []botUser `json:"users"`
}

func (c *botPanelClient) do(ctx context.Context, method, path string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("панель недоступна: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, telegramMaxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > telegramMaxResponseBytes {
		return errors.New("ответ панели слишком большой")
	}
	var envelope struct {
		Success *bool           `json:"success"`
		Error   string          `json:"error"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("панель вернула не JSON (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || (envelope.Success != nil && !*envelope.Success) {
		message := strings.TrimSpace(envelope.Error)
		if message == "" {
			message = strings.TrimSpace(envelope.Message)
		}
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("API панели: %s (HTTP %d)", message, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		if err := json.Unmarshal(envelope.Data, out); err == nil {
			return nil
		}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("неверный JSON панели: %w", err)
	}
	return nil
}

func (c *botPanelClient) users(ctx context.Context, query string, offset, limit int) (botUsersResponse, error) {
	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("offset", strconv.Itoa(offset))
	if strings.TrimSpace(query) != "" {
		params.Set("q", strings.TrimSpace(query))
	}
	var response botUsersResponse
	err := c.do(ctx, http.MethodGet, "/users?"+params.Encode(), nil, &response)
	return response, err
}

func (c *botPanelClient) user(ctx context.Context, key string) (botUser, error) {
	var response struct {
		User botUser `json:"user"`
	}
	err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(key), nil, &response)
	return response.User, err
}

func (c *botPanelClient) telegramUser(ctx context.Context, marker string) (botUser, bool, error) {
	var found botUser
	matched := false
	for offset := 0; ; offset += 100 {
		page, err := c.users(ctx, marker, offset, 100)
		if err != nil {
			return botUser{}, false, err
		}
		for _, user := range page.Users {
			if user.Comment == marker {
				if matched {
					return botUser{}, false, errors.New("найдено несколько пользователей для этого Telegram ID")
				}
				found, matched = user, true
			}
		}
		if len(page.Users) == 0 || offset+len(page.Users) >= page.Total {
			return found, matched, nil
		}
	}
}

func (c *botPanelClient) createUser(ctx context.Context, payload map[string]any) (botUser, error) {
	var response struct {
		User botUser `json:"user"`
	}
	err := c.do(ctx, http.MethodPost, "/users", payload, &response)
	return response.User, err
}

func (c *botPanelClient) userAction(ctx context.Context, key, action string) (botUser, error) {
	var response struct {
		User botUser `json:"user"`
	}
	err := c.do(ctx, http.MethodPost, "/users/"+url.PathEscape(key)+"/"+action, nil, &response)
	return response.User, err
}

func (c *botPanelClient) resetDevices(ctx context.Context, key string) (botUser, error) {
	var response struct {
		User botUser `json:"user"`
	}
	err := c.do(ctx, http.MethodPatch, "/users/"+url.PathEscape(key), map[string]any{"device_ids": []string{}}, &response)
	return response.User, err
}

func (c *botPanelClient) deleteUser(ctx context.Context, key string) error {
	return c.do(ctx, http.MethodDelete, "/users/"+url.PathEscape(key), nil, nil)
}

func (c *botPanelClient) nodes(ctx context.Context) ([]botNode, error) {
	var response struct {
		Nodes []botNode `json:"nodes"`
	}
	err := c.do(ctx, http.MethodGet, "/nodes", nil, &response)
	return response.Nodes, err
}

func (c *botPanelClient) status(ctx context.Context) (botSystemStatus, error) {
	var status botSystemStatus
	err := c.do(ctx, http.MethodGet, "/status", nil, &status)
	return status, err
}

func (c *botPanelClient) restart(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/system/restart", nil, nil)
}

type tgUpdate struct {
	UpdateID      int64       `json:"update_id"`
	Message       *tgMessage  `json:"message"`
	CallbackQuery *tgCallback `json:"callback_query"`
}

type tgUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type tgChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type tgMessage struct {
	MessageID int64   `json:"message_id"`
	From      *tgUser `json:"from"`
	Chat      tgChat  `json:"chat"`
	Text      string  `json:"text"`
}

type tgCallback struct {
	ID      string     `json:"id"`
	From    tgUser     `json:"from"`
	Message *tgMessage `json:"message"`
	Data    string     `json:"data"`
}

type tgCopyText struct {
	Text string `json:"text"`
}

type tgInlineButton struct {
	Text         string      `json:"text"`
	CallbackData string      `json:"callback_data,omitempty"`
	URL          string      `json:"url,omitempty"`
	CopyText     *tgCopyText `json:"copy_text,omitempty"`
}

type tgReplyMarkup struct {
	InlineKeyboard [][]tgInlineButton `json:"inline_keyboard"`
}

type telegramBot struct {
	cfg       telegramBotConfig
	panel     *botPanelClient
	http      *http.Client
	refsMu    sync.RWMutex
	userRefs  map[string]string
	startedAt time.Time
}

func startTelegramBot(panelCfg *PanelConfig) {
	cfg, err := loadTelegramBotConfig(panelCfg)
	if err != nil {
		log.Printf("[BOT] Конфигурация: %v", err)
		return
	}
	if !cfg.Enabled {
		return
	}
	bot := &telegramBot{
		cfg: cfg,
		panel: &botPanelClient{
			baseURL: cfg.PanelURL,
			apiKey:  cfg.APIKey,
			http:    &http.Client{Timeout: cfg.RequestTimeout, CheckRedirect: rejectBotRedirect},
		},
		http:      &http.Client{Timeout: 65 * time.Second, CheckRedirect: rejectBotRedirect},
		userRefs:  make(map[string]string),
		startedAt: time.Now(),
	}
	go bot.run(context.Background())
}

func rejectBotRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

func (b *telegramBot) run(ctx context.Context) {
	log.Printf("[BOT] Telegram-бот запущен; REST API: %s", b.cfg.PanelURL)
	_ = b.telegramCall(ctx, "deleteWebhook", map[string]any{"drop_pending_updates": false}, nil)
	_ = b.telegramCall(ctx, "setMyCommands", map[string]any{
		"commands": []map[string]string{
			{"command": "start", "description": "Главное меню"},
			{"command": "access", "description": "Мой доступ и трафик"},
			{"command": "id", "description": "Мой Telegram ID"},
			{"command": "admin", "description": "Управление панелью"},
			{"command": "help", "description": "Краткая справка"},
		},
	}, nil)

	var offset int64
	backoff := time.Second
	for ctx.Err() == nil {
		var updates []tgUpdate
		err := b.telegramCall(ctx, "getUpdates", map[string]any{
			"offset":          offset,
			"timeout":         50,
			"allowed_updates": []string{"message", "callback_query"},
		}, &updates)
		if err != nil {
			log.Printf("[BOT] Telegram polling: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						log.Printf("[BOT] update panic: %v", recovered)
					}
				}()
				b.handleUpdate(ctx, update)
			}()
		}
	}
}

func (b *telegramBot) telegramCall(ctx context.Context, method string, payload, result any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.TelegramAPI+"/bot"+b.cfg.Token+"/"+method, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		// net/http errors contain the request URL, including the bot token.
		return errors.New("Telegram недоступен: ошибка сетевого соединения")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, telegramMaxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > telegramMaxResponseBytes {
		return errors.New("Telegram response is too large")
	}
	var envelope struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("Telegram returned invalid JSON (HTTP %d)", resp.StatusCode)
	}
	if !envelope.OK {
		return fmt.Errorf("Telegram API: %s", strings.TrimSpace(envelope.Description))
	}
	if result != nil && len(envelope.Result) > 0 {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return err
		}
	}
	return nil
}

func (b *telegramBot) handleUpdate(ctx context.Context, update tgUpdate) {
	if update.Message != nil && update.Message.From != nil {
		b.handleMessage(ctx, update.Message)
		return
	}
	if update.CallbackQuery != nil {
		b.handleCallback(ctx, update.CallbackQuery)
	}
}

func (b *telegramBot) handleMessage(ctx context.Context, message *tgMessage) {
	if message.Chat.Type != "private" {
		return
	}
	text := strings.TrimSpace(message.Text)
	if text == "" {
		return
	}
	command, args := splitBotCommand(text)
	admin := b.isAdmin(message.From.ID)
	if !admin && !b.cfg.Public {
		b.send(ctx, message.Chat.ID, "⛔ Бот доступен только администраторам.", tgReplyMarkup{})
		return
	}

	switch command {
	case "/start", "/menu":
		if admin {
			b.showAdminHome(ctx, message.Chat.ID, 0)
		} else {
			b.showOwnAccess(ctx, message.Chat.ID, message.From.ID, 0)
		}
	case "/access", "/my":
		b.showOwnAccess(ctx, message.Chat.ID, message.From.ID, 0)
	case "/id":
		b.send(ctx, message.Chat.ID, fmt.Sprintf("Ваш Telegram ID: <code>%d</code>", message.From.ID), tgReplyMarkup{})
	case "/help":
		b.showHelp(ctx, message.Chat.ID, admin)
	case "/admin":
		if !b.requireAdmin(ctx, message.Chat.ID, message.From.ID) {
			return
		}
		b.showAdminHome(ctx, message.Chat.ID, 0)
	case "/users":
		if !b.requireAdmin(ctx, message.Chat.ID, message.From.ID) {
			return
		}
		b.showUsers(ctx, message.Chat.ID, 0, strings.TrimSpace(args), 0)
	case "/find":
		if !b.requireAdmin(ctx, message.Chat.ID, message.From.ID) {
			return
		}
		if strings.TrimSpace(args) == "" {
			b.send(ctx, message.Chat.ID, "Формат: <code>/find запрос</code>", tgReplyMarkup{})
			return
		}
		b.showUsers(ctx, message.Chat.ID, 0, strings.TrimSpace(args), 0)
	case "/grant", "/new":
		if !b.requireAdmin(ctx, message.Chat.ID, message.From.ID) {
			return
		}
		b.grantUser(ctx, message.Chat.ID, args)
	case "/nodes":
		if !b.requireAdmin(ctx, message.Chat.ID, message.From.ID) {
			return
		}
		b.showNodes(ctx, message.Chat.ID, 0)
	case "/status":
		if !b.requireAdmin(ctx, message.Chat.ID, message.From.ID) {
			return
		}
		b.showSystemStatus(ctx, message.Chat.ID, 0)
	case "/restart":
		if !b.requireAdmin(ctx, message.Chat.ID, message.From.ID) {
			return
		}
		b.send(ctx, message.Chat.ID, "Перезапустить VPN-службу?", tgReplyMarkup{InlineKeyboard: [][]tgInlineButton{{
			{Text: "Перезапустить", CallbackData: "restart:confirm"},
			{Text: "Отмена", CallbackData: "admin"},
		}}})
	default:
		b.showHelp(ctx, message.Chat.ID, admin)
	}
}

func splitBotCommand(text string) (string, string) {
	parts := strings.SplitN(text, " ", 2)
	command := strings.ToLower(parts[0])
	if at := strings.IndexByte(command, '@'); at >= 0 {
		command = command[:at]
	}
	if len(parts) == 1 {
		return command, ""
	}
	return command, strings.TrimSpace(parts[1])
}

func (b *telegramBot) handleCallback(ctx context.Context, callback *tgCallback) {
	if callback.Message == nil || callback.Message.Chat.Type != "private" {
		b.answerCallback(ctx, callback.ID, "Сообщение больше недоступно", true)
		return
	}
	chatID := callback.Message.Chat.ID
	messageID := callback.Message.MessageID
	b.answerCallback(ctx, callback.ID, "", false)

	parts := strings.Split(callback.Data, ":")
	action := parts[0]
	switch action {
	case "my":
		b.showOwnAccess(ctx, chatID, callback.From.ID, messageID)
	case "help":
		b.showHelpEdit(ctx, chatID, messageID, b.isAdmin(callback.From.ID))
	case "admin":
		if b.requireAdmin(ctx, chatID, callback.From.ID) {
			b.showAdminHome(ctx, chatID, messageID)
		}
	case "users":
		if !b.requireAdmin(ctx, chatID, callback.From.ID) {
			return
		}
		offset := 0
		if len(parts) > 1 {
			offset, _ = strconv.Atoi(parts[1])
		}
		b.showUsers(ctx, chatID, offset, "", messageID)
	case "user":
		if !b.requireAdmin(ctx, chatID, callback.From.ID) || len(parts) < 2 {
			return
		}
		b.showAdminUser(ctx, chatID, messageID, parts[1])
	case "activate", "block", "reset":
		if !b.requireAdmin(ctx, chatID, callback.From.ID) || len(parts) < 2 {
			return
		}
		b.runUserAction(ctx, chatID, messageID, parts[1], action)
	case "devices":
		if !b.requireAdmin(ctx, chatID, callback.From.ID) || len(parts) < 2 {
			return
		}
		b.resetUserDevices(ctx, chatID, messageID, parts)
	case "delete":
		if !b.requireAdmin(ctx, chatID, callback.From.ID) || len(parts) < 2 {
			return
		}
		b.confirmOrDeleteUser(ctx, chatID, messageID, parts)
	case "nodes":
		if b.requireAdmin(ctx, chatID, callback.From.ID) {
			b.showNodes(ctx, chatID, messageID)
		}
	case "status":
		if b.requireAdmin(ctx, chatID, callback.From.ID) {
			b.showSystemStatus(ctx, chatID, messageID)
		}
	case "restart":
		if !b.requireAdmin(ctx, chatID, callback.From.ID) {
			return
		}
		if len(parts) > 1 && parts[1] == "confirm" {
			b.restartSystem(ctx, chatID, messageID)
			return
		}
		b.respond(ctx, chatID, messageID, "Перезапустить VPN-службу?", tgReplyMarkup{InlineKeyboard: [][]tgInlineButton{{
			{Text: "Перезапустить", CallbackData: "restart:confirm"},
			{Text: "Отмена", CallbackData: "status"},
		}}})
	}
}

func (b *telegramBot) isAdmin(userID int64) bool {
	_, ok := b.cfg.AdminIDs[userID]
	return ok
}

func (b *telegramBot) requireAdmin(ctx context.Context, chatID, userID int64) bool {
	if b.isAdmin(userID) {
		return true
	}
	b.send(ctx, chatID, "⛔ Недостаточно прав.", tgReplyMarkup{})
	return false
}

func (b *telegramBot) showOwnAccess(ctx context.Context, chatID, telegramID, messageID int64) {
	marker := b.cfg.UserPrefix + strconv.FormatInt(telegramID, 10)
	matched, found, err := b.panel.telegramUser(ctx, marker)
	if err != nil {
		b.respondError(ctx, chatID, messageID, err)
		return
	}
	if !found {
		text := "<b>Доступ ещё не выдан</b>\n\nПередайте администратору ваш ID: <code>" + strconv.FormatInt(telegramID, 10) + "</code>"
		markup := tgReplyMarkup{InlineKeyboard: [][]tgInlineButton{{{Text: "Обновить", CallbackData: "my"}}}}
		b.respond(ctx, chatID, messageID, text, markup)
		return
	}
	user, err := b.panel.user(ctx, matched.PasswordKey)
	if err != nil {
		b.respondError(ctx, chatID, messageID, err)
		return
	}
	text, markup := b.userCard(user, false)
	b.respond(ctx, chatID, messageID, text, markup)
}

func (b *telegramBot) showAdminHome(ctx context.Context, chatID, messageID int64) {
	status, err := b.panel.status(ctx)
	if err != nil {
		b.respondError(ctx, chatID, messageID, err)
		return
	}
	text := fmt.Sprintf("<b>WDTT · управление</b>\n\nПользователи: <b>%d</b> активных\nСессии: <b>%d</b>\nНоды: <b>%d</b>\nВерсия: <code>%s</code>",
		status.ActiveUsers, status.Sessions, status.ClusterNodes, h(status.Version))
	markup := tgReplyMarkup{InlineKeyboard: [][]tgInlineButton{
		{{Text: "Пользователи", CallbackData: "users:0"}, {Text: "Ноды", CallbackData: "nodes"}},
		{{Text: "Состояние", CallbackData: "status"}, {Text: "Мой доступ", CallbackData: "my"}},
		{{Text: "Справка", CallbackData: "help"}},
	}}
	b.respond(ctx, chatID, messageID, text, markup)
}

func (b *telegramBot) showUsers(ctx context.Context, chatID int64, offset int, query string, messageID int64) {
	if offset < 0 {
		offset = 0
	}
	response, err := b.panel.users(ctx, query, offset, botUsersPageSize)
	if err != nil {
		b.respondError(ctx, chatID, messageID, err)
		return
	}
	title := "<b>Пользователи</b>"
	if query != "" {
		title += "\nПоиск: <code>" + h(query) + "</code>"
	}
	text := fmt.Sprintf("%s\nНайдено: <b>%d</b> · показано %d–%d", title, response.Total, minInt(offset+1, response.Total), minInt(offset+len(response.Users), response.Total))
	rows := make([][]tgInlineButton, 0, len(response.Users)+2)
	for _, user := range response.Users {
		ref := b.rememberUser(user.PasswordKey)
		label := statusDot(user) + " " + userLabel(user)
		if user.TrafficUsed > 0 {
			label += " · " + formatBotBytes(user.TrafficUsed)
		}
		rows = append(rows, []tgInlineButton{{Text: truncateRunes(label, 58), CallbackData: "user:" + ref}})
	}
	if len(response.Users) == 0 {
		text += "\n\nНичего не найдено."
	}
	if query == "" {
		nav := make([]tgInlineButton, 0, 2)
		if offset > 0 {
			nav = append(nav, tgInlineButton{Text: "←", CallbackData: "users:" + strconv.Itoa(maxInt(0, offset-botUsersPageSize))})
		}
		if offset+len(response.Users) < response.Total {
			nav = append(nav, tgInlineButton{Text: "→", CallbackData: "users:" + strconv.Itoa(offset+botUsersPageSize)})
		}
		if len(nav) > 0 {
			rows = append(rows, nav)
		}
	}
	rows = append(rows, []tgInlineButton{{Text: "Назад", CallbackData: "admin"}})
	b.respond(ctx, chatID, messageID, text, tgReplyMarkup{InlineKeyboard: rows})
}

func (b *telegramBot) showAdminUser(ctx context.Context, chatID, messageID int64, ref string) {
	key, ok := b.resolveUserRef(ref)
	if !ok {
		b.respond(ctx, chatID, messageID, "Карточка устарела. Откройте список пользователей заново.", adminBackMarkup())
		return
	}
	user, err := b.panel.user(ctx, key)
	if err != nil {
		b.respondError(ctx, chatID, messageID, err)
		return
	}
	text, markup := b.userCard(user, true)
	b.respond(ctx, chatID, messageID, text, markup)
}

func (b *telegramBot) userCard(user botUser, admin bool) (string, tgReplyMarkup) {
	status := "активен"
	symbol := "🟢"
	switch {
	case user.IsDeactivated:
		status, symbol = "заблокирован", "🔴"
	case user.IsExpired:
		status, symbol = "истёк", "🟠"
	case user.TrafficExceeded:
		status, symbol = "лимит исчерпан", "🟠"
	case user.Online:
		status, symbol = "онлайн", "🟢"
	case !user.Active:
		status, symbol = "неактивен", "⚪"
	}
	name := userLabel(user)
	used := user.TrafficUsed
	if used <= 0 {
		used = user.UpBytes + user.DownBytes
	}
	total := "безлимит"
	progress := ""
	if user.TotalBytes > 0 {
		total = formatBotBytes(user.TotalBytes)
		percent := float64(used) * 100 / float64(user.TotalBytes)
		if percent > 999 {
			percent = 999
		}
		progress = fmt.Sprintf(" · %.1f%%", percent)
	}
	expires := "без срока"
	if user.ExpiresAt > 0 {
		expires = time.Unix(user.ExpiresAt, 0).In(time.Local).Format("02.01.2006 15:04")
	}
	devices := strconv.Itoa(user.DevicesBound)
	if user.MaxDevices > 0 {
		devices += "/" + strconv.Itoa(user.MaxDevices)
	} else {
		devices += "/∞"
	}
	lastSeen := "никогда"
	if user.Online {
		lastSeen = "сейчас"
	} else if user.LastSeenAt > 0 {
		lastSeen = time.Unix(user.LastSeenAt, 0).In(time.Local).Format("02.01.2006 15:04")
	}

	var text strings.Builder
	fmt.Fprintf(&text, "%s <b>%s</b>\n%s\n\n", symbol, h(name), h(status))
	fmt.Fprintf(&text, "📊 <b>%s</b> / %s%s\n", h(formatBotBytes(used)), h(total), progress)
	fmt.Fprintf(&text, "↓ %s   ↑ %s\n", h(formatBotBytes(user.DownBytes)), h(formatBotBytes(user.UpBytes)))
	fmt.Fprintf(&text, "Устройства: <b>%s</b>\n", h(devices))
	fmt.Fprintf(&text, "Действует до: <b>%s</b>\n", h(expires))
	fmt.Fprintf(&text, "Последний онлайн: <b>%s</b>", h(lastSeen))
	if user.MaxDownMBps > 0 || user.MaxUpMBps > 0 {
		text.WriteString("\nСкорость: ↓ ")
		text.WriteString(formatBotSpeed(user.MaxDownMBps))
		text.WriteString(" · ↑ ")
		text.WriteString(formatBotSpeed(user.MaxUpMBps))
	}

	links := append([]botNodeLink(nil), user.NodeLinks...)
	if len(links) == 0 && strings.TrimSpace(user.Link) != "" {
		links = []botNodeLink{{NodeName: "Основная", Link: user.Link}}
	}
	sort.SliceStable(links, func(i, j int) bool {
		return strings.ToLower(links[i].NodeName+links[i].Host) < strings.ToLower(links[j].NodeName+links[j].Host)
	})
	rows := make([][]tgInlineButton, 0, b.cfg.CopyNodeLimit+5)
	shown := 0
	for _, link := range links {
		if shown >= b.cfg.CopyNodeLimit {
			break
		}
		copyLink := compactBotNodeLink(link.Link)
		if copyLink == "" {
			continue
		}
		label := strings.TrimSpace(link.NodeName)
		if label == "" {
			label = strings.TrimSpace(link.Host)
		}
		if label == "" {
			label = fmt.Sprintf("Нода %d", shown+1)
		}
		rows = append(rows, []tgInlineButton{{
			Text:     "Скопировать · " + truncateRunes(label, 38),
			CopyText: &tgCopyText{Text: copyLink},
		}})
		shown++
	}
	if len(links) > shown {
		fmt.Fprintf(&text, "\n\nДругие ноды доступны в подписке: <b>%d</b>", len(links)-shown)
	}
	if strings.TrimSpace(user.SubURL) != "" {
		var subRow []tgInlineButton
		if len([]rune(user.SubURL)) <= 256 {
			subRow = append(subRow, tgInlineButton{Text: "Скопировать подписку", CopyText: &tgCopyText{Text: user.SubURL}})
		} else {
			text.WriteString("\n\nПодписка: <code>" + h(user.SubURL) + "</code>")
		}
		if validPublicURL(user.SubURL) {
			subRow = append(subRow, tgInlineButton{Text: "Открыть", URL: user.SubURL})
		}
		if len(subRow) > 0 {
			rows = append(rows, subRow)
		}
	}
	if admin {
		ref := b.rememberUser(user.PasswordKey)
		if user.IsDeactivated || !user.Active {
			rows = append(rows, []tgInlineButton{{Text: "Активировать", CallbackData: "activate:" + ref}, {Text: "Сбросить трафик", CallbackData: "reset:" + ref}})
		} else {
			rows = append(rows, []tgInlineButton{{Text: "Заблокировать", CallbackData: "block:" + ref}, {Text: "Сбросить трафик", CallbackData: "reset:" + ref}})
		}
		rows = append(rows, []tgInlineButton{{Text: "Отвязать устройства", CallbackData: "devices:" + ref}})
		if !user.IsMain {
			rows = append(rows, []tgInlineButton{{Text: "Удалить", CallbackData: "delete:" + ref}, {Text: "Назад", CallbackData: "users:0"}})
		} else {
			rows = append(rows, []tgInlineButton{{Text: "Назад", CallbackData: "users:0"}})
		}
	} else {
		rows = append(rows, []tgInlineButton{{Text: "Обновить", CallbackData: "my"}})
	}
	return text.String(), tgReplyMarkup{InlineKeyboard: rows}
}

func (b *telegramBot) resetUserDevices(ctx context.Context, chatID, messageID int64, parts []string) {
	ref := parts[1]
	key, ok := b.resolveUserRef(ref)
	if !ok {
		b.respond(ctx, chatID, messageID, "Карточка устарела. Откройте пользователя заново.", adminBackMarkup())
		return
	}
	if len(parts) < 3 || parts[2] != "confirm" {
		b.respond(ctx, chatID, messageID, "Отвязать все устройства пользователя?", tgReplyMarkup{InlineKeyboard: [][]tgInlineButton{{
			{Text: "Отвязать", CallbackData: "devices:" + ref + ":confirm"},
			{Text: "Отмена", CallbackData: "user:" + ref},
		}}})
		return
	}
	user, err := b.panel.resetDevices(ctx, key)
	if err != nil {
		b.respondError(ctx, chatID, messageID, err)
		return
	}
	text, markup := b.userCard(user, true)
	b.respond(ctx, chatID, messageID, text, markup)
}

// Telegram copy buttons allow at most 256 characters. A node link may embed
// the subscription, which is already offered separately in the same card.
func compactBotNodeLink(link string) string {
	link = strings.TrimSpace(link)
	if len([]rune(link)) <= 256 {
		return link
	}
	payload, err := sharelink.Decode(link)
	if err != nil {
		return ""
	}
	payload.Sub = ""
	compact, err := sharelink.Encode(payload)
	if err != nil || len([]rune(compact)) > 256 {
		return ""
	}
	return compact
}

func (b *telegramBot) runUserAction(ctx context.Context, chatID, messageID int64, ref, action string) {
	key, ok := b.resolveUserRef(ref)
	if !ok {
		b.respond(ctx, chatID, messageID, "Карточка устарела. Откройте пользователя заново.", adminBackMarkup())
		return
	}
	apiAction := action
	if action == "reset" {
		apiAction = "reset-traffic"
	}
	user, err := b.panel.userAction(ctx, key, apiAction)
	if err != nil {
		b.respondError(ctx, chatID, messageID, err)
		return
	}
	text, markup := b.userCard(user, true)
	b.respond(ctx, chatID, messageID, text, markup)
}

func (b *telegramBot) confirmOrDeleteUser(ctx context.Context, chatID, messageID int64, parts []string) {
	ref := parts[1]
	key, ok := b.resolveUserRef(ref)
	if !ok {
		b.respond(ctx, chatID, messageID, "Карточка устарела. Откройте пользователя заново.", adminBackMarkup())
		return
	}
	if len(parts) < 3 || parts[2] != "confirm" {
		text := "Удалить пользователя <code>" + h(key) + "</code>? Это действие нельзя отменить."
		markup := tgReplyMarkup{InlineKeyboard: [][]tgInlineButton{{
			{Text: "Удалить", CallbackData: "delete:" + ref + ":confirm"},
			{Text: "Отмена", CallbackData: "user:" + ref},
		}}}
		b.respond(ctx, chatID, messageID, text, markup)
		return
	}
	if err := b.panel.deleteUser(ctx, key); err != nil {
		b.respondError(ctx, chatID, messageID, err)
		return
	}
	b.respond(ctx, chatID, messageID, "Пользователь удалён.", tgReplyMarkup{InlineKeyboard: [][]tgInlineButton{{{Text: "К списку", CallbackData: "users:0"}}}})
}

func (b *telegramBot) grantUser(ctx context.Context, chatID int64, args string) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		text := "<b>Выдача доступа</b>\n\n<code>/grant telegram_id [дни] [ГБ] [устройства]</code>\n\nПо умолчанию: " +
			fmt.Sprintf("%d дн., %s ГБ, %d устр.\nДля срока и трафика 0 означает безлимит; для устройств — значение панели по умолчанию.", b.cfg.DefaultDays, formatBotNumber(b.cfg.DefaultTrafficGB), b.cfg.DefaultDevices)
		b.send(ctx, chatID, text, adminBackMarkup())
		return
	}
	rawID := strings.TrimSpace(fields[0])
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id <= 0 {
		b.send(ctx, chatID, "Укажите числовой Telegram ID.", adminBackMarkup())
		return
	}
	comment := b.cfg.UserPrefix + strconv.FormatInt(id, 10)
	_, exists, err := b.panel.telegramUser(ctx, comment)
	if err != nil {
		b.send(ctx, chatID, "Не удалось проверить пользователя: "+h(err.Error()), adminBackMarkup())
		return
	}
	if exists {
		b.send(ctx, chatID, "Доступ для этого Telegram ID уже существует.", adminBackMarkup())
		return
	}
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		b.send(ctx, chatID, "Не удалось создать ключ доступа.", adminBackMarkup())
		return
	}
	key := hex.EncodeToString(secret)
	days := b.cfg.DefaultDays
	trafficGB := b.cfg.DefaultTrafficGB
	devices := b.cfg.DefaultDevices
	if len(fields) > 1 {
		days, err = strconv.Atoi(fields[1])
		if err != nil || days < 0 || days > 36500 {
			b.send(ctx, chatID, "Дни должны быть целым числом от 0 до 36500.", adminBackMarkup())
			return
		}
	}
	if len(fields) > 2 {
		trafficGB, err = strconv.ParseFloat(strings.ReplaceAll(fields[2], ",", "."), 64)
		if err != nil || trafficGB < 0 || trafficGB > 1_000_000 {
			b.send(ctx, chatID, "Трафик должен быть числом от 0 до 1000000 ГБ.", adminBackMarkup())
			return
		}
	}
	if len(fields) > 3 {
		devices, err = strconv.Atoi(fields[3])
		if err != nil || devices < 0 || devices > 10000 {
			b.send(ctx, chatID, "Число устройств должно быть от 0 до 10000.", adminBackMarkup())
			return
		}
	}
	expiresAt := int64(0)
	if days > 0 {
		expiresAt = time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
	}
	payload := map[string]any{
		"name":        key,
		"comment":     comment,
		"total_gb":    trafficGB,
		"expires_at":  expiresAt,
		"max_devices": devices,
	}
	user, err := b.panel.createUser(ctx, payload)
	if err != nil {
		b.send(ctx, chatID, "Не удалось выдать доступ: "+h(err.Error()), adminBackMarkup())
		return
	}
	text, markup := b.userCard(user, true)
	b.send(ctx, chatID, "✅ Доступ создан\n\n"+text, markup)
}

func (b *telegramBot) showNodes(ctx context.Context, chatID, messageID int64) {
	nodes, err := b.panel.nodes(ctx)
	if err != nil {
		b.respondError(ctx, chatID, messageID, err)
		return
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return strings.ToLower(nodes[i].Name+nodes[i].Host) < strings.ToLower(nodes[j].Name+nodes[j].Host)
	})
	var text strings.Builder
	fmt.Fprintf(&text, "<b>Ноды · %d</b>", len(nodes))
	var totalUp, totalDown int64
	for _, node := range nodes {
		totalUp += node.TrafficUp
		totalDown += node.TrafficDown
		name := strings.TrimSpace(node.Name)
		if name == "" {
			name = node.Host
		}
		online := node.IsActive && strings.EqualFold(node.Status, "online") && node.LastSeenAt > 0 && time.Since(time.Unix(node.LastSeenAt, 0)) <= 45*time.Second
		dot := "🔴"
		state := "офлайн"
		if online {
			dot, state = "🟢", "онлайн"
		}
		fmt.Fprintf(&text, "\n\n%s <b>%s</b> · %s", dot, h(name), state)
		fmt.Fprintf(&text, "\n<code>%s</code> · DTLS %d · RAW %d", h(node.Host), node.DtlsPort, node.RawPort)
		fmt.Fprintf(&text, "\nПользователи: %d · ↓ %s · ↑ %s", node.OnlineUsers, h(formatBotBytes(node.TrafficDown)), h(formatBotBytes(node.TrafficUp)))
		if node.Version != "" {
			fmt.Fprintf(&text, "\nВерсия: <code>%s</code>", h(node.Version))
		}
	}
	fmt.Fprintf(&text, "\n\n<b>Всего трафика:</b> ↓ %s · ↑ %s", h(formatBotBytes(totalDown)), h(formatBotBytes(totalUp)))
	markup := tgReplyMarkup{InlineKeyboard: [][]tgInlineButton{
		{{Text: "Обновить", CallbackData: "nodes"}, {Text: "Назад", CallbackData: "admin"}},
	}}
	b.respond(ctx, chatID, messageID, text.String(), markup)
}

func (b *telegramBot) showSystemStatus(ctx context.Context, chatID, messageID int64) {
	status, err := b.panel.status(ctx)
	if err != nil {
		b.respondError(ctx, chatID, messageID, err)
		return
	}
	text := fmt.Sprintf("<b>Состояние панели</b>\n\nВерсия: <code>%s</code>\nАптайм: <b>%s</b>\nАктивные пользователи: <b>%d</b>\nСессии: <b>%d</b>\nНоды: <b>%d</b>\nПорты: DTLS %d · RAW %d · client %d",
		h(status.Version), h(formatBotDuration(status.Uptime)), status.ActiveUsers, status.Sessions, status.ClusterNodes, status.DtlsPort, status.RawPort, status.ClientPort)
	markup := tgReplyMarkup{InlineKeyboard: [][]tgInlineButton{
		{{Text: "Обновить", CallbackData: "status"}, {Text: "Перезапустить VPN", CallbackData: "restart:ask"}},
		{{Text: "Назад", CallbackData: "admin"}},
	}}
	b.respond(ctx, chatID, messageID, text, markup)
}

func (b *telegramBot) restartSystem(ctx context.Context, chatID, messageID int64) {
	if err := b.panel.restart(ctx); err != nil {
		b.respondError(ctx, chatID, messageID, err)
		return
	}
	b.respond(ctx, chatID, messageID, "✅ Команда перезапуска отправлена.", adminBackMarkup())
}

func (b *telegramBot) showHelp(ctx context.Context, chatID int64, admin bool) {
	text, markup := helpContent(admin)
	b.send(ctx, chatID, text, markup)
}

func (b *telegramBot) showHelpEdit(ctx context.Context, chatID, messageID int64, admin bool) {
	text, markup := helpContent(admin)
	b.respond(ctx, chatID, messageID, text, markup)
}

func helpContent(admin bool) (string, tgReplyMarkup) {
	text := "<b>WDTT</b>\n\n/access — доступ, ноды и трафик\n/id — ваш Telegram ID\n/start — главное меню"
	back := "my"
	if admin {
		text += "\n\n<b>Администратор</b>\n/admin — панель управления\n/users [поиск] — пользователи\n/find запрос — поиск\n/grant ID [дни] [ГБ] [устройства] — выдать доступ\n/nodes — все ноды и их трафик\n/status — состояние\n/restart — перезапуск VPN"
		back = "admin"
	}
	return text, tgReplyMarkup{InlineKeyboard: [][]tgInlineButton{{{Text: "Назад", CallbackData: back}}}}
}

func (b *telegramBot) send(ctx context.Context, chatID int64, text string, markup tgReplyMarkup) {
	payload := messagePayload(chatID, 0, text, markup)
	if err := b.telegramCall(ctx, "sendMessage", payload, nil); err != nil {
		log.Printf("[BOT] sendMessage chat=%d: %v", chatID, err)
	}
}

func (b *telegramBot) respond(ctx context.Context, chatID, messageID int64, text string, markup tgReplyMarkup) {
	if messageID == 0 {
		b.send(ctx, chatID, text, markup)
		return
	}
	payload := messagePayload(chatID, messageID, text, markup)
	if err := b.telegramCall(ctx, "editMessageText", payload, nil); err != nil {
		// Telegram returns an error when text/markup is unchanged; avoid duplicate messages then.
		if strings.Contains(strings.ToLower(err.Error()), "message is not modified") {
			return
		}
		b.send(ctx, chatID, text, markup)
	}
}

func messagePayload(chatID, messageID int64, text string, markup tgReplyMarkup) map[string]any {
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     truncateRunes(text, 4000),
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if messageID > 0 {
		payload["message_id"] = messageID
	}
	if len(markup.InlineKeyboard) > 0 {
		payload["reply_markup"] = markup
	}
	return payload
}

func (b *telegramBot) answerCallback(ctx context.Context, callbackID, text string, alert bool) {
	payload := map[string]any{"callback_query_id": callbackID}
	if text != "" {
		payload["text"] = truncateRunes(text, 190)
		payload["show_alert"] = alert
	}
	_ = b.telegramCall(ctx, "answerCallbackQuery", payload, nil)
}

func (b *telegramBot) respondError(ctx context.Context, chatID, messageID int64, err error) {
	b.respond(ctx, chatID, messageID, "Не удалось выполнить запрос:\n<code>"+h(err.Error())+"</code>", adminBackMarkup())
}

func (b *telegramBot) rememberUser(key string) string {
	sum := sha256.Sum256([]byte(key))
	ref := hex.EncodeToString(sum[:8])
	b.refsMu.Lock()
	b.userRefs[ref] = key
	b.refsMu.Unlock()
	return ref
}

func (b *telegramBot) resolveUserRef(ref string) (string, bool) {
	b.refsMu.RLock()
	key, ok := b.userRefs[ref]
	b.refsMu.RUnlock()
	return key, ok
}

func adminBackMarkup() tgReplyMarkup {
	return tgReplyMarkup{InlineKeyboard: [][]tgInlineButton{{{Text: "Назад", CallbackData: "admin"}}}}
}

func statusDot(user botUser) string {
	switch {
	case user.IsDeactivated:
		return "🔴"
	case user.IsExpired || user.TrafficExceeded:
		return "🟠"
	case user.Online:
		return "🟢"
	default:
		return "⚪"
	}
}

func userLabel(user botUser) string {
	if value := strings.TrimSpace(user.Comment); value != "" {
		return value
	}
	if value := strings.TrimSpace(user.PasswordKey); value != "" {
		return value
	}
	return strings.TrimSpace(user.Password)
}

func formatBotBytes(value int64) string {
	if value < 0 {
		value = 0
	}
	units := []string{"Б", "КБ", "МБ", "ГБ", "ТБ", "ПБ"}
	n := float64(value)
	unit := 0
	for n >= 1024 && unit < len(units)-1 {
		n /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", value, units[unit])
	}
	if n >= 100 {
		return fmt.Sprintf("%.0f %s", n, units[unit])
	}
	return fmt.Sprintf("%.1f %s", n, units[unit])
}

func formatBotSpeed(value float64) string {
	if value <= 0 {
		return "∞"
	}
	return formatBotNumber(value) + " Мбит/с"
}

func formatBotNumber(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', 1, 64)
}

func formatBotDuration(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	d := time.Duration(seconds * float64(time.Second))
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	hours := d / time.Hour
	d -= hours * time.Hour
	minutes := d / time.Minute
	if days > 0 {
		return fmt.Sprintf("%d д. %d ч.", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%d ч. %d мин.", hours, minutes)
	}
	return fmt.Sprintf("%d мин.", minutes)
}

func validPublicURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

func h(value string) string { return html.EscapeString(value) }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
