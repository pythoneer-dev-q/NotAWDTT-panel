package panel

import (
	"bytes"
	"database/sql"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ildarmaga/wdtt/pkg/paneldb"
)

func TestWDTTMergePreservesTrafficAndSkipsConflicts(t *testing.T) {
	source := paneldb.NewStore()
	target := paneldb.NewStore()
	source.MainPassword = "source-admin"
	target.MainPassword = "target-admin"
	source.Users["source-admin"] = &paneldb.User{}
	source.Users["alice"] = &paneldb.User{DeviceID: "device-a", DeviceIDs: []string{"device-a"}, MaxDevices: 2, DownBytes: 123, UpBytes: 45, TotalBytes: 500, SubID: "alice-sub"}
	source.Devices["device-a"] = &paneldb.Device{DeviceID: "device-a", IP: "10.66.66.3", PrivKey: "private", PubKey: "public"}
	source.Users["bob"] = &paneldb.User{SubID: "taken-sub"}
	target.Users["existing"] = &paneldb.User{SubID: "taken-sub"}
	plan := planWDTTUserMerge(source, target, "hash", 3)
	if plan.preview.SourceUsers != 2 || plan.preview.ImportableUsers != 1 || len(plan.preview.Conflicts) != 1 {
		t.Fatalf("unexpected preview: %+v", plan.preview)
	}
	db, err := sql.Open("sqlite", "file:wdtt_merge_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE wdtt_global (id INTEGER PRIMARY KEY, users_rev INTEGER);
		INSERT INTO wdtt_global (id, users_rev) VALUES (1, 3);
		CREATE TABLE wdtt_users (password TEXT PRIMARY KEY, device_id TEXT, max_devices INTEGER, expires_at INTEGER, down_bytes INTEGER, up_bytes INTEGER, total_bytes INTEGER, max_down_mbps REAL, max_up_mbps REAL, is_deactivated INTEGER, comment TEXT, ports TEXT, vk_hash TEXT, sub_id TEXT, last_seen_at INTEGER);
		CREATE TABLE wdtt_user_devices (password TEXT, device_id TEXT, sort_order INTEGER);
		CREATE TABLE wdtt_devices (device_id TEXT PRIMARY KEY, ip TEXT, priv_key TEXT, pub_key TEXT);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyWDTTUserMerge(db, plan); err != nil {
		t.Fatal(err)
	}
	var down, up, total int64
	if err := db.QueryRow(`SELECT down_bytes, up_bytes, total_bytes FROM wdtt_users WHERE password = 'alice'`).Scan(&down, &up, &total); err != nil {
		t.Fatal(err)
	}
	if down != 123 || up != 45 || total != 500 {
		t.Fatalf("traffic changed: down=%d up=%d total=%d", down, up, total)
	}
	if err := applyWDTTUserMerge(db, plan); err == nil {
		t.Fatal("stale preview was accepted")
	}
}

func TestReadWDTTMergeSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE wdtt_global (id INTEGER PRIMARY KEY, main_password TEXT, admin_id TEXT, bot_token TEXT);
		INSERT INTO wdtt_global (id, main_password, admin_id, bot_token) VALUES (1, 'main', '', '');
		CREATE TABLE wdtt_users (password TEXT PRIMARY KEY, device_id TEXT, max_devices INTEGER, expires_at INTEGER, down_bytes INTEGER, up_bytes INTEGER, total_bytes INTEGER, max_down_mbps REAL, max_up_mbps REAL, is_deactivated INTEGER, comment TEXT, ports TEXT, vk_hash TEXT, sub_id TEXT, last_seen_at INTEGER);
		CREATE TABLE wdtt_user_devices (password TEXT, device_id TEXT, sort_order INTEGER);
		CREATE TABLE wdtt_devices (device_id TEXT PRIMARY KEY, ip TEXT, priv_key TEXT, pub_key TEXT);`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "source.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(file); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/merge", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	source, hash, cleanup, err := readWDTTMergeSource(req)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if hash == "" {
		t.Fatal("source checksum missing")
	}
	store, err := paneldb.LoadStore(source)
	if err != nil || store.MainPassword != "main" {
		t.Fatalf("source database not loaded: %+v %v", store, err)
	}
}
