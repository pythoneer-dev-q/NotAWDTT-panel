package panel

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ildarmaga/wdtt/pkg/paneldb"
)

type wdttMergeConflict struct {
	User   string `json:"user"`
	Reason string `json:"reason"`
}

type wdttMergePreview struct {
	SourceUsers     int                 `json:"source_users"`
	ImportableUsers int                 `json:"importable_users"`
	Conflicts       []wdttMergeConflict `json:"conflicts"`
	SourceSHA256    string              `json:"source_sha256"`
	PlanSHA256      string              `json:"plan_sha256"`
	TargetRevision  int64               `json:"target_revision"`
}

type wdttMergePlan struct {
	preview wdttMergePreview
	users   map[string]*paneldb.User
	devices map[string]*paneldb.Device
}

func planWDTTUserMerge(source, target *paneldb.Store, sha string, revision int64) wdttMergePlan {
	plan := wdttMergePlan{
		preview: wdttMergePreview{Conflicts: []wdttMergeConflict{}, SourceSHA256: sha, TargetRevision: revision},
		users:   map[string]*paneldb.User{},
		devices: map[string]*paneldb.Device{},
	}
	usedSubIDs := map[string]bool{}
	usedIPs := map[string]bool{}
	usedDeviceIDs := map[string]bool{}
	for _, user := range target.Users {
		if user != nil && user.SubID != "" {
			usedSubIDs[user.SubID] = true
		}
	}
	for _, device := range target.Devices {
		if device != nil && device.IP != "" {
			usedIPs[device.IP] = true
		}
	}
	for _, user := range target.Users {
		if user != nil {
			for _, id := range paneldb.AllDeviceIDs(user) {
				usedDeviceIDs[id] = true
			}
		}
	}
	passwords := make([]string, 0, len(source.Users))
	for password := range source.Users {
		if password != source.MainPassword && password != target.MainPassword {
			passwords = append(passwords, password)
		}
	}
	sort.Strings(passwords)
	plan.preview.SourceUsers = len(passwords)
	for _, password := range passwords {
		user := source.Users[password]
		if user == nil {
			continue
		}
		reason := ""
		if target.Users[password] != nil {
			reason = "пароль уже существует"
		} else if user.SubID != "" && usedSubIDs[user.SubID] {
			reason = "ID подписки уже существует"
		}
		for _, id := range paneldb.AllDeviceIDs(user) {
			if reason != "" {
				break
			}
			if usedDeviceIDs[id] {
				reason = "устройство уже существует"
				break
			}
			if device := source.Devices[id]; device != nil && device.IP != "" && usedIPs[device.IP] {
				reason = "адрес устройства уже занят"
			}
		}
		if reason != "" {
			plan.preview.Conflicts = append(plan.preview.Conflicts, wdttMergeConflict{User: password, Reason: reason})
			continue
		}
		plan.users[password] = user
		if user.SubID != "" {
			usedSubIDs[user.SubID] = true
		}
		for _, id := range paneldb.AllDeviceIDs(user) {
			usedDeviceIDs[id] = true
			if device := source.Devices[id]; device != nil {
				plan.devices[id] = device
				if device.IP != "" {
					usedIPs[device.IP] = true
				}
			}
		}
	}
	plan.preview.ImportableUsers = len(plan.users)
	planHash := sha256.New()
	for _, password := range passwords {
		if plan.users[password] != nil {
			fmt.Fprintf(planHash, "import:%s\n", password)
		} else {
			fmt.Fprintf(planHash, "skip:%s\n", password)
		}
	}
	plan.preview.PlanSHA256 = hex.EncodeToString(planHash.Sum(nil))
	return plan
}

func readWDTTMergeSource(r *http.Request) (*sql.DB, string, func(), error) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return nil, "", nil, err
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return nil, "", nil, err
	}
	defer file.Close()
	tmp, err := os.CreateTemp("", "wdtt-merge-*.db")
	if err != nil {
		return nil, "", nil, err
	}
	path := tmp.Name()
	cleanup := func() { _ = os.Remove(path) }
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(file, (128<<20)+1))
	closeErr := tmp.Close()
	if err != nil || closeErr != nil || n > 128<<20 {
		cleanup()
		return nil, "", nil, fmt.Errorf("файл базы превышает 128 МБ или не читается")
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		cleanup()
		return nil, "", nil, err
	}
	var integrity string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
		db.Close()
		cleanup()
		return nil, "", nil, fmt.Errorf("повреждённый SQLite-файл")
	}
	return db, hex.EncodeToString(hash.Sum(nil)), func() { db.Close(); cleanup() }, nil
}

func (a *App) handleWDTTMerge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !panelDBEnabled() {
		jsonError(w, "database unavailable", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 129<<20)
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	sourceDB, sha, cleanup, err := readWDTTMergeSource(r)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer cleanup()
	source, err := paneldb.LoadStore(sourceDB)
	if err != nil {
		jsonError(w, "неподдерживаемый формат WDTT panel.db: "+err.Error(), http.StatusBadRequest)
		return
	}
	target, err := paneldb.LoadStore(panelDB)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var mongo *paneldb.MongoDB
	if m, mongoErr := paneldb.GetDefaultMongo(); mongoErr == nil && m != nil {
		mongo = m
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		mongoStore, loadErr := m.LoadStore(ctx)
		cancel()
		if loadErr != nil {
			jsonError(w, "could not inspect MongoDB users: "+loadErr.Error(), http.StatusServiceUnavailable)
			return
		}
		for password, user := range mongoStore.Users {
			target.Users[password] = user
		}
		for id, device := range mongoStore.Devices {
			target.Devices[id] = device
		}
	}
	revision, err := paneldb.LoadUsersRev(panelDB)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	plan := planWDTTUserMerge(source, target, sha, revision)
	if r.FormValue("apply") != "true" {
		jsonOK(w, plan.preview)
		return
	}
	if r.FormValue("source_sha256") != sha || r.FormValue("target_revision") != fmt.Sprint(revision) || r.FormValue("plan_sha256") != plan.preview.PlanSHA256 {
		jsonError(w, "база или файл изменились; повторите предпросмотр", http.StatusConflict)
		return
	}
	if err := applyWDTTUserMerge(panelDB, plan); err != nil {
		jsonError(w, err.Error(), http.StatusConflict)
		return
	}
	if mongo != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		for password, user := range plan.users {
			if err := mongo.UpsertUser(ctx, password, user); err != nil {
				jsonError(w, "SQLite import succeeded, MongoDB user sync failed: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
		for _, device := range plan.devices {
			if err := mongo.UpsertDevice(ctx, device); err != nil {
				jsonError(w, "SQLite import succeeded, MongoDB device sync failed: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
	}
	invalidatePasswordsCache()
	jsonOK(w, plan.preview)
}

func applyWDTTUserMerge(db *sql.DB, plan wdttMergePlan) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE wdtt_global SET users_rev = users_rev + 1 WHERE id = 1 AND users_rev = ?`, plan.preview.TargetRevision)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("пользователи изменились; повторите предпросмотр")
	}
	passwords := make([]string, 0, len(plan.users))
	for password := range plan.users {
		passwords = append(passwords, password)
	}
	sort.Strings(passwords)
	for _, password := range passwords {
		u := plan.users[password]
		deactivated := 0
		if u.IsDeactivated {
			deactivated = 1
		}
		_, err = tx.Exec(`INSERT INTO wdtt_users (password, device_id, max_devices, expires_at, down_bytes, up_bytes, total_bytes, max_down_mbps, max_up_mbps, is_deactivated, comment, ports, vk_hash, sub_id, last_seen_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			password, u.DeviceID, u.MaxDevices, u.ExpiresAt, u.DownBytes, u.UpBytes, u.TotalBytes, u.MaxDownMBps, u.MaxUpMBps, deactivated, u.Comment, u.Ports, u.VkHash, u.SubID, u.LastSeenAt)
		if err != nil {
			return err
		}
		for index, id := range paneldb.AllDeviceIDs(u) {
			if _, err := tx.Exec(`INSERT INTO wdtt_user_devices (password, device_id, sort_order) VALUES (?,?,?)`, password, id, index); err != nil {
				return err
			}
			if device := plan.devices[id]; device != nil {
				if _, err := tx.Exec(`INSERT INTO wdtt_devices (device_id, ip, priv_key, pub_key) VALUES (?,?,?,?)`, id, device.IP, device.PrivKey, device.PubKey); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}
