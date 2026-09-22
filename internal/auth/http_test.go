package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gateway-runtime/internal/cache"
	"gateway-runtime/internal/policy"
	"gateway-runtime/internal/session"
	"gateway-runtime/internal/token"
	"gateway-runtime/internal/usercontext"
)

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
