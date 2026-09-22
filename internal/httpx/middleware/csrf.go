package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
)

const (
	CSRFCookieName         = "__Host-bff_csrf"
	InsecureCSRFCookieName = "bff_csrf"
	CSRFHeaderName         = "X-CSRF-Token"
)

type CSRFConfig struct {
	PublicURL  string
	Secure     bool
	CookieName string
}

func CSRF(cfg CSRFConfig, next http.Handler) http.Handler {
	origin, _ := url.Parse(cfg.PublicURL)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookieName := cfg.CookieName
		if cookieName == "" {
			cookieName = CSRFCookieName
		}
		ensureCSRFCookie(w, r, cfg.Secure, cookieName)

		if !isUnsafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		if !sameOriginRequest(r, origin) {
			http.Error(w, "csrf origin rejected", http.StatusForbidden)
			return
		}

		cookie, err := r.Cookie(cookieName)
		if err != nil || cookie.Value == "" {
			http.Error(w, "csrf token missing", http.StatusForbidden)
			return
		}
		header := r.Header.Get(CSRFHeaderName)
		if !constantTimeEqual(cookie.Value, header) {
			http.Error(w, "csrf token invalid", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func ensureCSRFCookie(w http.ResponseWriter, r *http.Request, secure bool, cookieName string) {
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		return
	}
	token := randomToken(32)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		Secure:   secure,
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	})
}

func sameOriginRequest(r *http.Request, expected *url.URL) bool {
	if expected == nil {
		return false
	}
	raw := r.Header.Get("Origin")
	if raw == "" {
		raw = r.Header.Get("Referer")
	}
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, expected.Scheme) &&
		strings.EqualFold(u.Host, expected.Host)
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func randomToken(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func CSRFCookieNameFor(secure bool) string {
	if secure {
		return CSRFCookieName
	}
	return InsecureCSRFCookieName
}
