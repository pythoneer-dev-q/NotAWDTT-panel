package panel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRestCredentialsRequireAuthentication(t *testing.T) {
	if err := initI18n(); err != nil {
		t.Fatal(err)
	}
	app := &App{cfg: &PanelConfig{ApiKey: "test-secret", SessionKey: "test-session-key", WebBasePath: "/wdtt/"}}
	handler := app.requireAuthCSRF(app.handleRestCredentials)
	req := httptest.NewRequest(http.MethodGet, "/wdtt/panel/api/rest-credentials", nil)
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "test-secret") {
		t.Fatalf("unauthenticated response: %d %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/wdtt/panel/api/rest-credentials", nil)
	req.Header.Set("X-API-Key", "test-secret")
	w = httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "test-secret") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("authenticated response: %d %s", w.Code, w.Body.String())
	}
}
