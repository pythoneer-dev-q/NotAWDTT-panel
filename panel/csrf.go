package panel

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const csrfCookie = "wdtt-csrf"

func (a *App) csrfTokenForSession(sess *sessionData) string {
	if sess == nil || a.cfg == nil {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(a.cfg.SessionKey))
	mac.Write([]byte(fmt.Sprintf("csrf|%s|%d", sess.User, sess.ExpiresAt)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:18])
}

func (a *App) validCSRF(sess *sessionData, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" || sess == nil {
		return false
	}
	expected := a.csrfTokenForSession(sess)
	return expected != "" && hmac.Equal([]byte(token), []byte(expected))
}

func (a *App) setCSRFCookie(w http.ResponseWriter, sess *sessionData) {
	token := a.csrfTokenForSession(sess)
	if token == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookie,
		Value:    token,
		Path:     a.cfg.basePath(),
		Secure:   a.sessionCookieSecure(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(a.cfg.sessionDuration().Seconds()),
	})
}

func (a *App) clearCSRFCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookie,
		Value:    "",
		Path:     a.cfg.basePath(),
		MaxAge:   -1,
	})
}

func (a *App) requireAuthCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. API Token/Key via Authorization or X-API-Key header bypasses CSRF
		if sess := a.authFromHeaders(r); sess != nil {
			next(w, r)
			return
		}
		// 2. Cookie session check
		sess := a.parseSession(r)
		if sess == nil {
			base := ""
			if a.cfg != nil {
				base = a.cfg.basePath()
			}
			if isAjax(r) || strings.Contains(r.URL.Path, "/api/") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"msg":     i18nWeb("pages.login.loginAgain"),
				})
				return
			}
			http.Redirect(w, r, base, http.StatusFound)
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if !a.validCSRF(sess, r.Header.Get("X-CSRF-Token")) {
				jsonError(w, "invalid csrf token", http.StatusForbidden)
				return
			}
		}
		a.setCSRFCookie(w, sess)
		next(w, r)
	}
}

// requireRestAuth is a dedicated middleware for REST API endpoints.
// Accepts Bearer Token, X-API-Key, or cookie session. Bypasses CSRF on header tokens.
func (a *App) requireRestAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if sess := a.authFromHeaders(r); sess != nil {
			next(w, r)
			return
		}
		sess := a.parseSession(r)
		if sess == nil {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "unauthorized: missing or invalid Bearer token or X-API-Key",
			})
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if !a.validCSRF(sess, r.Header.Get("X-CSRF-Token")) {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"error":   "invalid csrf token for cookie session; use Bearer token or X-API-Key to bypass csrf",
				})
				return
			}
		}
		next(w, r)
	}
}
