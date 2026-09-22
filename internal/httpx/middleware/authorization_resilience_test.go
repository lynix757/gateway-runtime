package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"gateway-runtime/internal/policy"
	"gateway-runtime/internal/session"
)

type failingSessionBackend struct{}

func (failingSessionBackend) Get(context.Context, string) (session.Session, error) {
	return session.Session{}, errors.New("redis unavailable")
}
func (failingSessionBackend) Put(context.Context, session.Session) error {
	return errors.New("redis unavailable")
}
func (failingSessionBackend) Delete(context.Context, string) error {
	return errors.New("redis unavailable")
}

func TestRequirePermissionReturns503OnSessionBackendFailure(t *testing.T) {
	h := RequirePermission(
		"asset.read",
		failingSessionBackend{},
		policy.Static{},
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/assets/1", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-bff_session", Value: "sid"})
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
