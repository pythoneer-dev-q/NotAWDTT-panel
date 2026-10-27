package panel

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ildarmaga/wdtt/pkg/sharelink"
)

func TestTelegramCardTrafficAndNodeLimit(t *testing.T) {
	b := &telegramBot{cfg: telegramBotConfig{CopyNodeLimit: 5}, userRefs: map[string]string{}}
	u := botUser{Comment: "tg_123", Active: true, Online: true, UpBytes: 1024, DownBytes: 2048, TrafficUsed: 3072, TotalBytes: 10240, SubURL: "https://example.org/sub/abc"}
	for i := 0; i < 7; i++ {
		u.NodeLinks = append(u.NodeLinks, botNodeLink{NodeName: fmt.Sprintf("node-%d", i), Link: fmt.Sprintf("wdtt://%d", i)})
	}
	text, markup := b.userCard(u, false)
	if !strings.Contains(text, "3.0 КБ") || !strings.Contains(text, "30.0%") || !strings.Contains(text, "онлайн") {
		t.Fatalf("missing traffic or online: %s", text)
	}
	count := 0
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			if button.CopyText != nil && strings.HasPrefix(button.CopyText.Text, "wdtt://") {
				count++
			}
		}
	}
	if count != 5 || !strings.Contains(text, "<b>2</b>") {
		t.Fatalf("got %d node copy buttons and text %q", count, text)
	}
}

func TestTelegramNodeLinkCompactPreservesCredentials(t *testing.T) {
	original, err := sharelink.Encode(sharelink.Payload{IP: "example.org", Pass: "secret", Dtls: 56000, Sub: "https://example.org/" + strings.Repeat("a", 200)})
	if err != nil {
		t.Fatal(err)
	}
	compact := compactBotNodeLink(original)
	decoded, err := sharelink.Decode(compact)
	if err != nil || len(compact) > 256 || decoded.Pass != "secret" || decoded.IP != "example.org" || decoded.Sub != "" {
		t.Fatalf("invalid compact link: %#v, %v", decoded, err)
	}
}

func TestTelegramPanelClientAndExactMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "key" {
			t.Error("missing API key")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("offset") == "100" {
			fmt.Fprint(w, `{"success":true,"data":{"total":101,"users":[{"password_key":"random-secret","comment":"tg_123"}]}}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"total":101,"users":[`)
		for i := 0; i < 100; i++ {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprint(w, `{"password_key":"other","comment":"tg_1234"}`)
		}
		fmt.Fprint(w, `]}`)
	}))
	defer server.Close()
	c := &botPanelClient{baseURL: server.URL, apiKey: "key", http: server.Client()}
	u, found, err := c.telegramUser(context.Background(), "tg_123")
	if err != nil || !found || u.PasswordKey != "random-secret" {
		t.Fatalf("got %#v, found=%v, err=%v", u, found, err)
	}
}

func TestTelegramConfigRequiresAdminAndClampsNodeLimit(t *testing.T) {
	t.Setenv("WDTT_BOT_ENABLED", "true")
	t.Setenv("WDTT_BOT_TOKEN", "test")
	t.Setenv("WDTT_BOT_ADMIN_IDS", "")
	if _, err := loadTelegramBotConfig(&PanelConfig{ApiKey: "key"}); err == nil {
		t.Fatal("expected missing admin error")
	}
	t.Setenv("WDTT_BOT_ADMIN_IDS", "123,456")
	t.Setenv("WDTT_BOT_COPY_NODE_LIMIT", "10")
	cfg, err := loadTelegramBotConfig(&PanelConfig{ApiKey: "key"})
	if err != nil || cfg.CopyNodeLimit != 5 || len(cfg.AdminIDs) != 2 {
		t.Fatalf("unexpected config %#v, %v", cfg, err)
	}
}

func TestPanelOnlyDoesNotReloadLocalVPN(t *testing.T) {
	t.Setenv("WDTT_INSTALL_MODE", "panel")
	old := wdttHotReloadFn
	defer func() { wdttHotReloadFn = old }()
	wdttHotReloadFn = func() error { t.Fatal("local reload called in panel-only mode"); return nil }
	if err := applyWdttConfigChange(); err != nil {
		t.Fatal(err)
	}
	if err := controlService("wdtt", "restart"); err == nil {
		t.Fatal("restart of absent VPN should fail")
	}
	if strings.Contains(acmeCronScript(), "\nrenew_dtls_if_needed\n") {
		t.Fatal("panel-only cron must not restart the panel to renew a local DTLS certificate")
	}
}
