package panel

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type telegramBotSettings struct {
	Enabled          bool    `json:"enabled"`
	Token            string  `json:"token"`
	AdminIDs         string  `json:"admin_ids"`
	DefaultDays      int     `json:"default_days"`
	DefaultTrafficGB float64 `json:"default_traffic_gb"`
	DefaultDevices   int     `json:"default_devices"`
	Public           bool    `json:"public"`
}

var telegramRuntime struct {
	mu         sync.Mutex
	cancel     context.CancelFunc
	generation uint64
	running    bool
	startedAt  time.Time
	lastError  string
}

func telegramSettingsTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS wdtt_bot_settings (id INTEGER PRIMARY KEY CHECK(id = 1), config_json TEXT NOT NULL)`)
	return err
}

func loadSavedTelegramBotSettings() (telegramBotSettings, bool, error) {
	var settings telegramBotSettings
	if !panelDBEnabled() {
		return settings, false, nil
	}
	if err := telegramSettingsTable(panelDB); err != nil {
		return settings, false, err
	}
	var raw string
	err := panelDB.QueryRow(`SELECT config_json FROM wdtt_bot_settings WHERE id = 1`).Scan(&raw)
	if err == sql.ErrNoRows {
		return settings, false, nil
	}
	if err != nil {
		return settings, false, err
	}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return settings, false, err
	}
	return settings, true, nil
}

func parseTelegramAdminIDs(raw string) map[int64]struct{} {
	ids := map[int64]struct{}{}
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t' }) {
		id, err := strconv.ParseInt(field, 10, 64)
		if err == nil && id > 0 {
			ids[id] = struct{}{}
		}
	}
	return ids
}

func validateTelegramBotSettings(s telegramBotSettings) error {
	if s.DefaultDays < 0 || s.DefaultDays > 3650 || s.DefaultTrafficGB < 0 || s.DefaultTrafficGB > 100000 || s.DefaultDevices < 1 || s.DefaultDevices > 20 {
		return fmt.Errorf("invalid default limits")
	}
	for _, field := range strings.FieldsFunc(s.AdminIDs, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t' }) {
		id, err := strconv.ParseInt(field, 10, 64)
		if err != nil || id <= 0 {
			return fmt.Errorf("invalid Telegram admin ID: %q", field)
		}
	}
	if s.Enabled && (s.Token == "" || len(parseTelegramAdminIDs(s.AdminIDs)) == 0) {
		return fmt.Errorf("token and at least one admin ID are required to start")
	}
	return nil
}

func stopTelegramBot() {
	telegramRuntime.mu.Lock()
	if telegramRuntime.cancel != nil {
		telegramRuntime.cancel()
	}
	telegramRuntime.generation++
	telegramRuntime.cancel = nil
	telegramRuntime.running = false
	telegramRuntime.mu.Unlock()
}

func (a *App) handleTelegramBotSettings(w http.ResponseWriter, r *http.Request) {
	if !panelDBEnabled() {
		jsonError(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodGet {
		s, saved, err := loadSavedTelegramBotSettings()
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !saved {
			s = telegramBotSettings{
				Enabled: envBool("WDTT_BOT_ENABLED", false), Token: strings.TrimSpace(os.Getenv("WDTT_BOT_TOKEN")),
				AdminIDs: os.Getenv("WDTT_BOT_ADMIN_IDS"), DefaultDays: envInt("WDTT_BOT_DEFAULT_DAYS", 30),
				DefaultTrafficGB: envFloat("WDTT_BOT_DEFAULT_TRAFFIC_GB", 100), DefaultDevices: envInt("WDTT_BOT_DEFAULT_DEVICES", 2),
				Public: envBool("WDTT_BOT_PUBLIC", true),
			}
		}
		telegramRuntime.mu.Lock()
		status := map[string]any{"running": telegramRuntime.running, "started_at": telegramRuntime.startedAt, "last_error": telegramRuntime.lastError}
		telegramRuntime.mu.Unlock()
		jsonOK(w, map[string]any{
			"enabled": s.Enabled, "token_present": s.Token != "", "admin_ids": s.AdminIDs,
			"default_days": s.DefaultDays, "default_traffic_gb": s.DefaultTrafficGB,
			"default_devices": s.DefaultDevices, "public": s.Public, "status": status,
		})
		return
	}
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var next telegramBotSettings
	if err := readJSON(r, &next); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	previous, saved, err := loadSavedTelegramBotSettings()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if next.Token == "" {
		if saved {
			next.Token = previous.Token
		} else {
			next.Token = strings.TrimSpace(os.Getenv("WDTT_BOT_TOKEN"))
		}
	}
	if err := validateTelegramBotSettings(next); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	raw, err := json.Marshal(next)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := panelDB.Exec(`INSERT INTO wdtt_bot_settings (id, config_json) VALUES (1, ?) ON CONFLICT(id) DO UPDATE SET config_json = excluded.config_json`, string(raw)); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	startTelegramBot(a.cfg)
	jsonOK(w, map[string]any{"enabled": next.Enabled, "token_present": next.Token != ""})
}
