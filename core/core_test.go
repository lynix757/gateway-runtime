package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gateway-runtime/internal/app"
	"gateway-runtime/internal/audit"
	"gateway-runtime/internal/httpx/middleware"
	"gateway-runtime/internal/policy"
	"gateway-runtime/internal/session"
)

func TestProtectedRouteEmitsCorrelatedBusinessAuditEvent(t *testing.T) {
	sessions := session.NewMemoryStore()
	if err := sessions.Put(context.Background(), session.Session{
		ID: "sid", Subject: "user-1", Username: "alice", Roles: []string{"admin"},
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	sink := &audit.MemorySink{}
	router := &Router{deps: app.RouteDeps{
		Sessions: sessions,
		Policy: policy.Static{RolePermissions: map[string][]string{
			"admin": {"asset.read"},
		}},
		Audit:             sink,
		SessionCookieName: authSessionCookieName,
	}}
	router.HandleFunc("GET /api/assets/{id}", "asset.read", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux := http.NewServeMux()
	router.Register(mux)
	proxy, err := middleware.ParseTrustedProxyCIDRs([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	h := middleware.RequestID(middleware.TraceContext(middleware.RequestMetadata(proxy, mux)))
	req := httptest.NewRequest(http.MethodGet, "/api/assets/a-1", nil)
	req.RemoteAddr = "10.1.2.3:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	req.Header.Set("CF-Ray", "8f1234567890abcd-BKK")
	req.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: "sid"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	events := sink.Snapshot()
	if len(events) != 2 {
		t.Fatalf("audit events = %d, want authorization and business events: %+v", len(events), events)
	}
	e := events[1]
	if e.Actor != "user-1" || e.ActorUsername != "alice" || e.ClientIP != "203.0.113.10" {
		t.Fatalf("unexpected audit identity: %+v", e)
	}
	if e.Action != "asset.read" || e.Target != "asset" || e.ResourceID != "a-1" || e.Outcome != "success" || e.HTTPStatus != http.StatusOK {
		t.Fatalf("unexpected business audit event: %+v", e)
	}
	if e.CorrelationID == "" || e.TraceID == "" || e.CFRay != "8f1234567890abcd-BKK" || e.CorrelationID != events[0].CorrelationID || e.TraceID != events[0].TraceID {
		t.Fatalf("audit events are not correlated: %+v", events)
	}
}

const authSessionCookieName = "__Host-bff_session"
