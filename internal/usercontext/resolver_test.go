package usercontext

import (
	"context"
	"testing"
	"time"

	"gateway-runtime/internal/cache"
	"gateway-runtime/internal/policy"
	"gateway-runtime/internal/session"
)

func TestSessionResolverDerivesContext(t *testing.T) {
	sessions := session.NewMemoryStore()
	err := sessions.Put(context.Background(), session.Session{
		ID:          "sid",
		Subject:     "user-1",
		Issuer:      "issuer",
		DisplayName: "Alice",
		Email:       "alice@example.com",
		Roles:       []string{"admin", "viewer"},
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	resolver := SessionResolver{
		Sessions: sessions,
		Policy: policy.Static{
			RolePermissions: map[string][]string{
				"admin":  {"asset.write", "asset.read"},
				"viewer": {"asset.read"},
			},
			RoleCapabilities: map[string][]string{
				"admin": {"asset-admin"},
			},
		},
	}

	got, err := resolver.Resolve(context.Background(), "sid")
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "user-1" || got.DisplayName != "Alice" || got.Email != "alice@example.com" {
		t.Fatalf("unexpected identity context: %+v", got)
	}
	if len(got.Permissions) != 2 {
		t.Fatalf("permissions = %#v", got.Permissions)
	}
	if len(got.Capabilities) != 1 || got.Capabilities[0] != "asset-admin" {
		t.Fatalf("capabilities = %#v", got.Capabilities)
	}
}

type countingResolver struct {
	calls int
	out   Context
}

func (r *countingResolver) Resolve(context.Context, string) (Context, error) {
	r.calls++
	return r.out, nil
}

func TestCachedResolverCachesAndInvalidates(t *testing.T) {
	c := cache.NewMemory()
	next := &countingResolver{out: Context{Subject: "user-1"}}
	resolver := CachedResolver{Next: next, Cache: c, TTL: time.Minute}

	if _, err := resolver.Resolve(context.Background(), "sid"); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), "sid"); err != nil {
		t.Fatal(err)
	}
	if next.calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", next.calls)
	}

	if err := c.Delete(context.Background(), CacheKey("sid")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), "sid"); err != nil {
		t.Fatal(err)
	}
	if next.calls != 2 {
		t.Fatalf("resolver calls after invalidation = %d, want 2", next.calls)
	}
}
