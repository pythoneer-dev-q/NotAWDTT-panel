package panel

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubscriptionWantsHTML(t *testing.T) {
	cases := []struct {
		name   string
		accept string
		ua     string
		query  string
		want   bool
	}{
		{"browser", "text/html,application/xhtml+xml", "Mozilla/5.0 Chrome/120", "", true},
		{"vpn client", "*/*", "HiddifyNext/1.0", "", false},
		{"okhttp", "*/*", "okhttp/4.9.0", "", false},
		{"explicit html", "*/*", "", "html=1", true},
		{"explicit raw", "text/html", "Mozilla/5.0", "format=raw", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/sub/test?"+tc.query, nil)
			if tc.accept != "" {
				r.Header.Set("Accept", tc.accept)
			}
			if tc.ua != "" {
				r.Header.Set("User-Agent", tc.ua)
			}
			if got := subscriptionWantsHTML(r); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestSubscriptionPageAppearanceValidatesConfiguration(t *testing.T) {
	t.Setenv("WDTT_SUB_PAGE_ACCENT", "#a1b2c3")
	t.Setenv("WDTT_SUB_PAGE_RADIUS", "8")
	t.Setenv("WDTT_SUB_PAGE_THEME", "light")
	t.Setenv("WDTT_SUB_PAGE_LOGO_URL", "https://example.org/logo.png")
	got := subscriptionPageAppearance()
	if got["color"] != "#a1b2c3" || got["radius"] != 8 || got["theme"] != "light" || got["logo"] != "https://example.org/logo.png" {
		t.Fatalf("invalid appearance: %#v", got)
	}
	t.Setenv("WDTT_SUB_PAGE_ACCENT", "red; display:none")
	t.Setenv("WDTT_SUB_PAGE_RADIUS", "999")
	t.Setenv("WDTT_SUB_PAGE_LOGO_URL", "javascript:alert(1)")
	got = subscriptionPageAppearance()
	if got["color"] != "#f46b17" || got["radius"] != 16 || got["logo"] != "" {
		t.Fatalf("unsafe appearance: %#v", got)
	}
}
