package panel

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ildarmaga/wdtt/pkg/paneldb"
)

func TestSubAppearanceValidation(t *testing.T) {
	valid := subAppearance{Color: "#f46b17", Radius: 16, Theme: "dark", Logo: "https://example.com/logo.png"}
	if err := validateSubAppearance(&valid); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []subAppearance{
		{Color: "red", Radius: 16, Theme: "dark"},
		{Color: "#f46b17", Radius: 33, Theme: "dark"},
		{Color: "#f46b17", Radius: 16, Theme: "light", Logo: "javascript:alert(1)"},
	} {
		if validateSubAppearance(&invalid) == nil {
			t.Fatalf("accepted invalid settings: %+v", invalid)
		}
	}
}

func TestSubscriptionBuilderTemplatesParse(t *testing.T) {
	if err := initTemplates(); err != nil {
		t.Fatal(err)
	}
	if htmlTemplates.Lookup("settings/panel/subscription") == nil {
		t.Fatal("subscription builder template missing")
	}
}

func TestSubscriptionAppearanceSavedInSQLite(t *testing.T) {
	if m, _ := paneldb.GetDefaultMongo(); m != nil {
		t.Skip("requires isolated SQLite backend")
	}
	app, _ := setupTestApp(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"color":"#123456","radius":12,"theme":"dark","logo":"","background":"","surface":"","text":"","heading":"Hello","instruction":"Connect"}`))
	app.handleSubAppearance(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	appearance, err := readSubAppearance()
	if err != nil {
		t.Fatal(err)
	}
	if appearance["heading"] != "Hello" || appearance["color"] != "#123456" {
		t.Fatalf("not persisted: %v", appearance)
	}
}
