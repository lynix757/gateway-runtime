package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gateway-runtime/internal/policy"
	"gateway-runtime/internal/session"
)

func TestRequirePermission(t *testing.T) {
	sessions := session.NewMemoryStore()
	if err := sessions.Put(context.Background(), session.Session{
		ID: "sid", Subject: "user-1", Roles: []string{"admin"},
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	evaluator := policy.Static{
		RolePermissions: map[string][]string{"admin": {"asset.write"}},
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := RequirePermission("asset.write", sessions, evaluator, next)

	req := httptest.NewRequest(http.MethodPost, "/api/assets", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-bff_session", Value: "sid"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestRequirePermissionRejectsMissingPermission(t *testing.T) {
	sessions := session.NewMemoryStore()
	if err := sessions.Put(context.Background(), session.Session{
		ID: "sid", Subject: "user-1", Roles: []string{"viewer"},
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	evaluator := policy.Static{
		RolePermissions: map[string][]string{"viewer": {"asset.read"}},
	}
	h := RequirePermission("asset.write", sessions, evaluator, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	req := httptest.NewRequest(http.MethodPost, "/api/assets", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-bff_session", Value: "sid"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
