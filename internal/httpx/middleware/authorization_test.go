package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gateway-runtime/internal/audit"
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

func TestAuthorizationDeniedAuditIncludesActorClientAndStatus(t *testing.T) {
	sessions := session.NewMemoryStore()
	if err := sessions.Put(context.Background(), session.Session{
		ID: "sid", Subject: "user-1", Username: "alice", Roles: []string{"viewer"},
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	sink := &audit.MemorySink{}
	evaluator := policy.Static{RolePermissions: map[string][]string{"viewer": {"asset.read"}}}
	proxy, err := ParseTrustedProxyCIDRs([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	h := RequestMetadata(proxy, RequirePermissionWithAudit(
		"asset.write", sessions, evaluator, sink, nil,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	))
	req := httptest.NewRequest(http.MethodPost, "/api/assets", nil)
	req.RemoteAddr = "10.1.2.3:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	req.AddCookie(&http.Cookie{Name: "__Host-bff_session", Value: "sid"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	events := sink.Snapshot()
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	e := events[0]
	if e.Actor != "user-1" || e.ActorUsername != "alice" || e.ClientIP != "203.0.113.10" {
		t.Fatalf("unexpected audit identity: %+v", e)
	}
	if e.Action != "authorization.deny" || e.Outcome != "deny" || e.HTTPStatus != http.StatusForbidden {
		t.Fatalf("unexpected audit result: %+v", e)
	}
}

func TestUnauthenticatedAuthorizationAttemptIsAudited(t *testing.T) {
	sink := &audit.MemorySink{}
	proxy, err := ParseTrustedProxyCIDRs([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	h := RequestMetadata(proxy, RequirePermissionWithAudit(
		"asset.write", session.NewMemoryStore(), policy.Static{}, sink, nil,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	))
	req := httptest.NewRequest(http.MethodPost, "/api/assets", nil)
	req.RemoteAddr = "10.1.2.3:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	events := sink.Snapshot()
	if len(events) != 1 || events[0].Action != "authorization.deny" || events[0].ClientIP != "203.0.113.10" || events[0].HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("unexpected audit events: %+v", events)
	}
}

func TestAuthorizationAllowIsAuditedSeparately(t *testing.T) {
	sessions := session.NewMemoryStore()
	if err := sessions.Put(context.Background(), session.Session{
		ID: "sid", Subject: "user-1", Username: "alice", Roles: []string{"admin"},
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	sink := &audit.MemorySink{}
	evaluator := policy.Static{RolePermissions: map[string][]string{"admin": {"asset.write"}}}
	h := RequirePermissionWithAudit(
		"asset.write", sessions, evaluator, sink, nil,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }),
	)
	req := httptest.NewRequest(http.MethodPost, "/api/assets/a-1", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-bff_session", Value: "sid"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	events := sink.Snapshot()
	if len(events) != 1 || events[0].Action != "authorization.allow" || events[0].Outcome != "allow" {
		t.Fatalf("unexpected audit events: %+v", events)
	}
}
