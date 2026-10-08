package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ildarmaga/wdtt/pkg/paneldb"
	"github.com/ildarmaga/wdtt/pkg/vkhash"
)

func wdttAdminBaseURL() string {
	cfg, err := loadWdttInbound()
	addr := "127.0.0.1:2861"
	if err == nil && strings.TrimSpace(cfg.AdminAddr) != "" {
		addr = strings.TrimSpace(cfg.AdminAddr)
	}
	return "http://" + addr
}

func wdttAdminPost(path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, wdttAdminBaseURL()+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg, err := loadPanelConfig(); err == nil && cfg != nil {
		if key := strings.TrimSpace(cfg.SessionKey); key != "" {
			req.Header.Set("X-WDTT-Admin-Token", key)
		}
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("admin API: unauthorized")
	}
	var out struct {
		OK  bool   `json:"ok"`
		Msg string `json:"msg"`
	}
	if json.Unmarshal(raw, &out) == nil && !out.OK {
		if out.Msg != "" {
			return fmt.Errorf("%s", out.Msg)
		}
		return fmt.Errorf("admin API error")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("admin API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func serverApplyUserUpdate(oldPassword, newPassword string, req userAPIReq, manageDevices bool) error {
	body := panelUserUpdateReq{
		OldPassword:    oldPassword,
		Password:       newPassword,
		DeviceID:       req.DeviceID,
		DeviceIDs:      req.DeviceIDs,
		MaxDevices:     req.MaxDevices,
		Comment:        req.Comment,
		ExpiresAt:      req.ExpiresAt,
		TotalGB:        req.TotalGB,
		MaxDownMBps:    req.MaxDownMBps,
		MaxUpMBps:      req.MaxUpMBps,
		Active:         req.Active,
		Ports:          req.Ports,
		DtlsPort:       req.DtlsPort,
		WgPort:         req.WgPort,
		ClientPort:     req.ClientPort,
		UseCustomPorts: req.UseCustomPorts,
		VkHash:         req.VkHash,
	}
	if !manageDevices {
		body.DeviceIDs = nil
	}
	err := wdttAdminPost("/admin/users/update", body)
	if err == nil {
		return nil
	}
	// Fallback to direct DB update if admin HTTP daemon is offline or restarting
	return updateUserDirectDB(oldPassword, newPassword, req, manageDevices)
}

func updateUserDirectDB(oldPassword, newPassword string, req userAPIReq, manageDevices bool) error {
	db, err := loadPasswords()
	if err != nil {
		return err
	}
	cur, ok := db.Passwords[oldPassword]
	if !ok || cur == nil {
		return fmt.Errorf("пользователь не найден")
	}

	entry := *cur
	if req.Comment != "" || req.Comment != cur.Comment {
		entry.Comment = strings.TrimSpace(req.Comment)
	}
	if req.ExpiresAt != 0 || req.ExpiresAt != cur.ExpiresAt {
		entry.ExpiresAt = req.ExpiresAt
	}
	if req.TotalGB >= 0 {
		entry.TotalBytes = gbToBytes(req.TotalGB)
	}
	if req.MaxDownMBps > 0 {
		entry.MaxDownMBps = req.MaxDownMBps
	}
	if req.MaxUpMBps > 0 {
		entry.MaxUpMBps = req.MaxUpMBps
	}
	if req.Active != nil {
		entry.IsDeactivated = !*req.Active
	}
	if req.MaxDevices > 0 {
		entry.MaxDevices = req.MaxDevices
	}
	if req.VkHash != "" {
		entry.VkHash = vkhash.Normalize(req.VkHash)
	}
	if req.Ports != "" {
		entry.Ports = strings.TrimSpace(req.Ports)
	}

	if manageDevices {
		if req.DeviceIDs != nil {
			entry.DeviceIDs = append([]string(nil), (*req.DeviceIDs)...)
		} else if id := strings.TrimSpace(req.DeviceID); id != "" {
			entry.DeviceIDs = []string{id}
		}
	}
	normalizeEntryDevices(&entry)

	if newPassword != oldPassword {
		if _, exists := db.Passwords[newPassword]; exists {
			return fmt.Errorf("пароль уже существует")
		}
		delete(db.Passwords, oldPassword)
		db.Passwords[newPassword] = &entry
		if m, err := paneldb.GetDefaultMongo(); err == nil && m != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = m.RenameUser(ctx, oldPassword, newPassword, userEntryToPaneldb(&entry))
		}
		if panelDBEnabled() {
			_ = paneldb.RenameUserPassword(panelDB, oldPassword, newPassword)
		}
	} else {
		db.Passwords[oldPassword] = &entry
	}

	if err := upsertUserNorm(db, newPassword, &entry); err != nil {
		return err
	}
	applyWdttConfigChange()
	return nil
}

func serverDeleteUser(pass string) error {
	_ = wdttAdminPost("/admin/users/delete", map[string]string{"password": pass})
	db, err := loadPasswords()
	if err == nil && db != nil {
		delete(db.Passwords, pass)
	}
	if m, err := paneldb.GetDefaultMongo(); err == nil && m != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = m.DeleteUser(ctx, pass)
	}
	if panelDBEnabled() {
		_ = paneldb.DeleteUser(panelDB, pass, nil)
		invalidatePasswordsCache()
	}
	applyWdttConfigChange()
	return nil
}

type panelUserUpdateReq struct {
	OldPassword    string    `json:"old_password"`
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
}
