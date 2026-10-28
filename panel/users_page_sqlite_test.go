package panel

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestSQLiteUsersPageIsBoundedAndSearchable(t *testing.T) {
	app, _ := setupTestApp(t)
	if _, err := panelDB.Exec(`DELETE FROM wdtt_users`); err != nil {
		t.Fatal(err)
	}
	for _, pass := range []string{"alice", "bob", "charlie", "david", "eve"} {
		if _, err := panelDB.Exec(`INSERT INTO wdtt_users (password, comment, is_deactivated) VALUES (?, ?, ?)`, pass, "user "+pass, pass == "david"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := panelDB.Exec(`INSERT INTO wdtt_user_devices (password, device_id, sort_order) VALUES ('charlie', 'phone-123', 0)`); err != nil {
		t.Fatal(err)
	}
	read := func(query string) (int, []map[string]interface{}) {
		t.Helper()
		w := httptest.NewRecorder()
		app.handleSQLiteUsersPage(w, httptest.NewRequest("GET", "/?"+query, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body.String())
		}
		var response struct {
			Obj struct {
				Total int                      `json:"total"`
				Users []map[string]interface{} `json:"users"`
			} `json:"obj"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response.Obj.Total, response.Obj.Users
	}
	total, first := read("limit=2&offset=0")
	_, second := read("limit=2&offset=2")
	if total != 5 || len(first) != 2 || len(second) != 2 || first[0]["password_key"] != "alice" || second[0]["password_key"] != "charlie" {
		t.Fatalf("unexpected pages: total %d, %v, %v", total, first, second)
	}
	total, search := read("limit=2&offset=0&q=phone-123")
	if total != 1 || len(search) != 1 || search[0]["password_key"] != "charlie" || search[0]["devices_bound"] != float64(1) {
		t.Fatalf("unexpected device search: %v", search)
	}
	total, blocked := read("limit=2&offset=0&status=blocked")
	if total != 1 || len(blocked) != 1 || blocked[0]["password_key"] != "david" {
		t.Fatalf("unexpected blocked users: %v", blocked)
	}
}
