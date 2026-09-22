package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRFIssuesCookieOnSafeRequest(t *testing.T) {
	h := CSRF(CSRFConfig{PublicURL: "https://example.com", Secure: true}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "https://example.com/api/me", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	var found bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == CSRFCookieName {
			found = true
			if !c.Secure || c.HttpOnly || c.Path != "/" {
				t.Fatalf("unexpected csrf cookie attributes: %+v", c)
			}
		}
	}
	if !found {
		t.Fatal("csrf cookie not issued")
	}
}

func TestCSRFRejectsUnsafeRequestWithoutToken(t *testing.T) {
	h := CSRF(CSRFConfig{PublicURL: "https://example.com", Secure: true}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "https://example.com/auth/logout", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestCSRFAcceptsMatchingTokenAndOrigin(t *testing.T) {
	h := CSRF(CSRFConfig{PublicURL: "https://example.com", Secure: true}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "https://example.com/auth/logout", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set(CSRFHeaderName, "abc")
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "abc"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}
