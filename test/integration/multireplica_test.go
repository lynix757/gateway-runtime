package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"gateway-runtime/internal/auth"
	"gateway-runtime/internal/cache"
	"gateway-runtime/internal/httpx"
	"gateway-runtime/internal/observability"
	"gateway-runtime/internal/policy"
	"gateway-runtime/internal/session"
	redisstore "gateway-runtime/internal/store/redis"
	"gateway-runtime/internal/usercontext"
)

func TestRedisSessionContinuityAcrossReplicas(t *testing.T) {
	if os.Getenv("REBFF_INTEGRATION") != "1" {
		t.Skip("set REBFF_INTEGRATION=1 to run integration tests")
	}

	const redisURL = "redis://localhost:16379/0"
	const prefix = "rebff-it-test"

	clientA, err := redisstore.New(redisURL, prefix)
	if err != nil {
		t.Fatal(err)
	}
	defer clientA.Close()

	clientB, err := redisstore.New(redisURL, prefix)
	if err != nil {
		t.Fatal(err)
	}
	defer clientB.Close()

	ctx := context.Background()
	if err := clientA.Ping(ctx); err != nil {
		t.Fatalf("redis unavailable: %v", err)
	}
	if err := clientA.RDB.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}

	sessionsA := redisstore.SessionStore{Client: clientA}
	sessionsB := redisstore.SessionStore{Client: clientB}
	cacheA := redisstore.Cache{Client: clientA}
	cacheB := redisstore.Cache{Client: clientB}

	evaluator := policy.Static{
		RolePermissions: map[string][]string{
			"admin": {"managed-item.read", "storage.upload"},
		},
		RoleCapabilities: map[string][]string{
			"admin": {"asset-admin"},
		},
	}

	replicaA := newReplica(t, sessionsA, cacheA, evaluator)
	replicaB := newReplica(t, sessionsB, cacheB, evaluator)

	now := time.Now()
	s := session.Session{
		ID:          "session-shared-1",
		Subject:     "alice-id",
		Issuer:      "http://localhost:18081/realms/rebff",
		DisplayName: "Alice Admin",
		Email:       "alice@example.com",
		Roles:       []string{"admin"},
		CreatedAt:   now,
		LastSeen:    now,
		ExpiresAt:   now.Add(10 * time.Minute),
	}
	if err := sessionsA.Put(ctx, s); err != nil {
		t.Fatal(err)
	}

	assertMe(t, replicaA, s.ID, http.StatusOK, "alice-id")
	assertMe(t, replicaB, s.ID, http.StatusOK, "alice-id")

	if err := sessionsA.Delete(ctx, s.ID); err != nil {
		t.Fatal(err)
	}

	// Even if replica B previously populated the shared user-context cache,
	// session deletion remains authoritative and must force 401.
	assertMe(t, replicaB, s.ID, http.StatusUnauthorized, "")
}

func newReplica(t *testing.T, sessions session.Store, c cache.Cache, evaluator policy.Evaluator) http.Handler {
	t.Helper()
	resolver := usercontext.CachedResolver{
		Next: usercontext.SessionResolver{
			Sessions: sessions,
			Policy:   evaluator,
		},
		Cache: c,
		TTL:   time.Minute,
	}
	handler := auth.Handler{
		Service: &auth.Service{
			Sessions: sessions,
		},
		UserContext: resolver,
		Cache:       c,
		Metrics:     observability.NewMetrics(),
	}
	return httpx.NewRouter("", httpx.RouterDeps{
		Auth:      handler,
		PublicURL: "http://localhost",
		Metrics:   observability.NewMetrics(),
	})
}

func assertMe(t *testing.T, h http.Handler, sessionID string, wantStatus int, wantSub string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != wantStatus {
		t.Fatalf("/api/me status = %d, want %d; body=%s", rec.Code, wantStatus, rec.Body.String())
	}
	if wantStatus != http.StatusOK {
		return
	}

	var got usercontext.Context
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Subject != wantSub {
		t.Fatalf("subject = %q, want %q", got.Subject, wantSub)
	}
	if len(got.Permissions) != 2 {
		t.Fatalf("permissions = %#v", got.Permissions)
	}
}
