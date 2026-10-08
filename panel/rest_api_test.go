package panel

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func setupTestApp(t *testing.T) (*App, *http.ServeMux) {
	t.Helper()
	dir := t.TempDir()
	wdttConfigDir = dir
	panelConfigPath = filepath.Join(dir, "panel.json")
	panelDBPath = filepath.Join(dir, "panel.db")

	t.Cleanup(func() {
		if panelDB != nil {
			_ = panelDB.Close()
			panelDB = nil
		}
	})

	if err := initPanelDB(); err != nil {
		t.Fatal(err)
	}
	ensureDefaultWdttData()

	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &PanelConfig{
		Username:     "admin",
		PasswordHash: string(hash),
		Port:         2860,
		WebBasePath:  "/wdtt/",
		SessionKey:   "test-session-key-0123456789abcdef",
		ApiKey:       "test-api-key-xyz123",
	}

	app := &App{cfg: cfg}
	mux := http.NewServeMux()
	registerRestAPI(mux, app, cfg.WebBasePath)
	return app, mux
}

func TestRestAuthToken(t *testing.T) {
	_, mux := setupTestApp(t)

	// 1. Invalid credentials
	invalidBody := []byte(`{"username":"admin","password":"wrong"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/token", bytes.NewReader(invalidBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password, got %d", w.Code)
	}

	// 2. Valid credentials
	validBody := []byte(`{"username":"admin","password":"secret123"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/auth/token", bytes.NewReader(validBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid credentials, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Success bool   `json:"success"`
		Token   string `json:"token"`
		ApiKey  string `json:"api_key"`
		User    string `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.Success || res.Token == "" || res.ApiKey != "test-api-key-xyz123" || res.User != "admin" {
		t.Fatalf("unexpected auth response: %+v", res)
	}
}

func TestRestAuthHeaderBypassCSRF(t *testing.T) {
	app, mux := setupTestApp(t)

	// 1. No auth -> 401
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", w.Code)
	}

	// 2. Auth with X-API-Key
	req = httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("X-API-Key", app.cfg.ApiKey)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with X-API-Key, got %d: %s", w.Code, w.Body.String())
	}

	// 3. Auth with Bearer Token
	token, _ := app.createToken("admin", 1*time.Hour)
	req = httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with Bearer token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRestUserLifecycleAndLimits(t *testing.T) {
	app, mux := setupTestApp(t)
	token, _ := app.createToken("admin", 1*time.Hour)

	// 1. Create User via POST /api/users (with traffic limit and expiry)
	expiryTs := time.Now().Add(30 * 24 * time.Hour).Unix()
	createBody := map[string]interface{}{
		"comment":    "Test User REST",
		"total_gb":   50.0,
		"expires_at": expiryTs,
	}
	raw, _ := json.Marshal(createBody)

	req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var createResp struct {
		Success bool         `json:"success"`
		User    RestUserItem `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &createResp); err != nil {
		t.Fatal(err)
	}
	if !createResp.Success || createResp.User.PasswordKey == "" {
		t.Fatalf("user creation response invalid: %+v", createResp)
	}
	pass := createResp.User.PasswordKey

	// Verify limits are stored and returned
	if createResp.User.TotalGB != 50.0 {
		t.Fatalf("expected TotalGB=50, got %f", createResp.User.TotalGB)
	}
	if createResp.User.ExpiresAt != expiryTs {
		t.Fatalf("expected ExpiresAt=%d, got %d", expiryTs, createResp.User.ExpiresAt)
	}
	if !createResp.User.Active {
		t.Fatalf("expected user to be active initially")
	}

	// 2. Search users: GET /api/users?q=REST
	searchReq := httptest.NewRequest(http.MethodGet, "/api/users?q=REST", nil)
	searchReq.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, searchReq)
	if w.Code != http.StatusOK {
		t.Fatalf("search expected 200, got %d", w.Code)
	}
	var listResp struct {
		Success bool           `json:"success"`
		Total   int            `json:"total"`
		Users   []RestUserItem `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if listResp.Total == 0 || len(listResp.Users) == 0 {
		t.Fatalf("expected to find created user in search results")
	}

	// 3. Block user: POST /api/users/{id}/block
	blockReq := httptest.NewRequest(http.MethodPost, "/api/users/"+pass+"/block", nil)
	blockReq.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, blockReq)
	if w.Code != http.StatusOK {
		t.Fatalf("block expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify blocked in GET
	getReq := httptest.NewRequest(http.MethodGet, "/api/users/"+pass, nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, getReq)
	var getResp struct {
		Success bool         `json:"success"`
		User    RestUserItem `json:"user"`
	}
	json.Unmarshal(w.Body.Bytes(), &getResp)
	if !getResp.User.IsDeactivated {
		t.Fatalf("expected IsDeactivated=true after block")
	}

	// 4. Activate user: POST /api/users/{id}/activate
	actReq := httptest.NewRequest(http.MethodPost, "/api/users/"+pass+"/activate", nil)
	actReq.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, actReq)
	if w.Code != http.StatusOK {
		t.Fatalf("activate expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 5. Reset traffic: POST /api/users/{id}/reset-traffic
	resetReq := httptest.NewRequest(http.MethodPost, "/api/users/"+pass+"/reset-traffic", nil)
	resetReq.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, resetReq)
	if w.Code != http.StatusOK {
		t.Fatalf("reset-traffic expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 6. Delete user: DELETE /api/users/{id}
	delReq := httptest.NewRequest(http.MethodDelete, "/api/users/"+pass, nil)
	delReq.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, delReq)
	if w.Code != http.StatusOK {
		t.Fatalf("delete expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify deleted
	getReq = httptest.NewRequest(http.MethodGet, "/api/users/"+pass, nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, getReq)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after deletion, got %d", w.Code)
	}
}
