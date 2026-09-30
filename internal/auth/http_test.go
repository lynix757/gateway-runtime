package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gateway-runtime/internal/audit"
	"gateway-runtime/internal/cache"
	"gateway-runtime/internal/httpx/middleware"
	"gateway-runtime/internal/policy"
	"gateway-runtime/internal/session"
	"gateway-runtime/internal/token"
	"gateway-runtime/internal/usercontext"
)

type failingUserContextResolver struct{}

func (failingUserContextResolver) Resolve(context.Context, string) (usercontext.Context, error) {
	return usercontext.Context{}, errors.New("policy unavailable")
}

func TestMeReturnsDerivedUserContext(t *testing.T) {
	sessions := session.NewMemoryStore()
	if err := sessions.Put(context.Background(), session.Session{
		ID:          "sid",
		Subject:     "user-1",
		Issuer:      "issuer",
		DisplayName: "Alice",
		Email:       "alice@example.com",
		Roles:       []string{"admin"},
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	resolver := usercontext.SessionResolver{
		Sessions: sessions,
		Policy: policy.Static{
			RolePermissions: map[string][]string{"admin": {"asset.read"}},
		},
	}
	h := Handler{
		Service: &Service{
			Provider:     UnconfiguredProvider{},
			Flows:        NewMemoryFlowStore(),
			Sessions:     sessions,
			Tokens:       token.NewMemoryStore(),
			LogoutReturn: "https://example.com/",
		},
		UserContext: resolver,
		Cache:       cache.NewMemory(),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "sid"})
	rec := httptest.NewRecorder()

	h.me(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("expected no-store")
	}
	var got usercontext.Context
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Subject != "user-1" || len(got.Permissions) != 1 {
		t.Fatalf("unexpected context: %+v", got)
	}
}

func TestLogoutInvalidatesUserContextCache(t *testing.T) {
	sessions := session.NewMemoryStore()
	tokens := token.NewMemoryStore()
	c := cache.NewMemory()
	expires := time.Now().Add(time.Hour)
	if err := sessions.Put(context.Background(), session.Session{
		ID: "sid", Subject: "user-1", Issuer: "issuer", ExpiresAt: expires,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tokens.Put(context.Background(), "sid", token.Set{}, expires); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(context.Background(), usercontext.CacheKey("sid"), usercontext.Context{Subject: "user-1"}, time.Minute); err != nil {
		t.Fatal(err)
	}

	h := Handler{
		Service: &Service{
			Provider:     UnconfiguredProvider{},
			Flows:        NewMemoryFlowStore(),
			Sessions:     sessions,
			Tokens:       tokens,
			LogoutReturn: "https://example.com/",
		},
		UserContext: usercontext.SessionResolver{Sessions: sessions},
		Cache:       c,
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "sid"})
	rec := httptest.NewRecorder()

	h.logout(rec, req)

	var cached usercontext.Context
	ok, err := c.Get(context.Background(), usercontext.CacheKey("sid"), &cached)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected user context cache invalidated")
	}
}

func TestLogoutAuditIncludesUsernameClientAndOutcome(t *testing.T) {
	sessions := session.NewMemoryStore()
	tokens := token.NewMemoryStore()
	expires := time.Now().Add(time.Hour)
	if err := sessions.Put(context.Background(), session.Session{
		ID: "sid", Subject: "user-1", Username: "alice", Issuer: "issuer", ExpiresAt: expires,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tokens.Put(context.Background(), "sid", token.Set{}, expires); err != nil {
		t.Fatal(err)
	}
	sink := &audit.MemorySink{}
	h := Handler{
		Service: &Service{
			Provider: UnconfiguredProvider{}, Sessions: sessions, Tokens: tokens,
			LogoutReturn: "https://example.com/",
		},
		UserContext: usercontext.SessionResolver{Sessions: sessions},
		Audit:       sink,
	}
	proxy, err := middleware.ParseTrustedProxyCIDRs([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	handler := middleware.RequestMetadata(proxy, http.HandlerFunc(h.logout))
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.RemoteAddr = "10.1.2.3:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "sid"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	events := sink.Snapshot()
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	e := events[0]
	if e.Actor != "user-1" || e.ActorUsername != "alice" || e.ClientIP != "203.0.113.10" {
		t.Fatalf("unexpected audit identity: %+v", e)
	}
	if e.Action != "auth.logout" || e.Outcome != "success" || e.HTTPStatus != http.StatusSeeOther {
		t.Fatalf("unexpected audit result: %+v", e)
	}
}

func TestLoginAuditIncludesOIDCIdentityAndClient(t *testing.T) {
	provider := &fakeProvider{}
	service, sessions, _ := newTestService(provider)
	start, err := service.StartLogin(context.Background(), "/dashboard")
	if err != nil || start.URL == "" {
		t.Fatalf("start login: %v", err)
	}
	sink := &audit.MemorySink{}
	h := Handler{
		Service: service,
		UserContext: usercontext.SessionResolver{
			Sessions: sessions,
		},
		Audit: sink,
	}
	proxy, err := middleware.ParseTrustedProxyCIDRs([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	handler := middleware.RequestMetadata(proxy, http.HandlerFunc(h.callback))
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+provider.lastState+"&code=code-1", nil)
	req.RemoteAddr = "10.1.2.3:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	events := sink.Snapshot()
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	e := events[0]
	if e.Actor != "user-1" || e.ActorUsername != "alice" || e.ClientIP != "203.0.113.10" {
		t.Fatalf("unexpected audit identity: %+v", e)
	}
	if e.Action != "auth.login" || e.Outcome != "success" || e.HTTPStatus != http.StatusFound {
		t.Fatalf("unexpected audit result: %+v", e)
	}
}

func TestLoginAuditIdentityDoesNotDependOnUserContextResolution(t *testing.T) {
	provider := &fakeProvider{}
	service, _, _ := newTestService(provider)
	if _, err := service.StartLogin(context.Background(), "/"); err != nil {
		t.Fatal(err)
	}
	sink := &audit.MemorySink{}
	h := Handler{Service: service, UserContext: failingUserContextResolver{}, Audit: sink}
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+provider.lastState+"&code=code-1", nil)
	rec := httptest.NewRecorder()
	h.callback(rec, req)

	events := sink.Snapshot()
	if len(events) != 1 || events[0].Actor != "user-1" || events[0].ActorUsername != "alice" {
		t.Fatalf("unexpected audit events: %+v", events)
	}
}

func TestLogoutAuditIdentityDoesNotDependOnUserContextResolution(t *testing.T) {
	sessions := session.NewMemoryStore()
	tokens := token.NewMemoryStore()
	expires := time.Now().Add(time.Hour)
	if err := sessions.Put(context.Background(), session.Session{
		ID: "sid", Subject: "user-1", Username: "alice", ExpiresAt: expires,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tokens.Put(context.Background(), "sid", token.Set{}, expires); err != nil {
		t.Fatal(err)
	}
	sink := &audit.MemorySink{}
	h := Handler{
		Service:     &Service{Provider: UnconfiguredProvider{}, Sessions: sessions, Tokens: tokens, LogoutReturn: "https://example.com/"},
		UserContext: failingUserContextResolver{},
		Audit:       sink,
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "sid"})
	rec := httptest.NewRecorder()
	h.logout(rec, req)

	events := sink.Snapshot()
	if len(events) != 1 || events[0].Actor != "user-1" || events[0].ActorUsername != "alice" {
		t.Fatalf("unexpected audit events: %+v", events)
	}
}
