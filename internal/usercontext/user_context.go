package usercontext

import (
	"context"
	"time"

	"gateway-runtime/internal/cache"
)

const DefaultTTL = 60 * time.Second

type Context struct {
	Subject      string   `json:"sub"`
	DisplayName  string   `json:"display_name,omitempty"`
	Email        string   `json:"email,omitempty"`
	Roles        []string `json:"roles,omitempty"`
	Permissions  []string `json:"permissions,omitempty"`
	TenantID     string   `json:"tenant_id,omitempty"`
	ProjectID    string   `json:"project_id,omitempty"`
	AuthLevel    string   `json:"auth_level,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type Resolver interface {
	Resolve(ctx context.Context, sessionID string) (Context, error)
}

func CacheKey(sessionID string) string {
	return "userctx:" + sessionID
}

type CachedResolver struct {
	Next  Resolver
	Cache cache.Cache
	TTL   time.Duration
}

func (r CachedResolver) Resolve(ctx context.Context, sessionID string) (Context, error) {
	if r.Cache == nil {
		return r.Next.Resolve(ctx, sessionID)
	}
	key := CacheKey(sessionID)

	var out Context
	if ok, err := r.Cache.Get(ctx, key, &out); err == nil && ok {
		return out, nil
	}

	out, err := r.Next.Resolve(ctx, sessionID)
	if err != nil {
		return Context{}, err
	}

	ttl := r.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	_ = r.Cache.Set(ctx, key, out, ttl)
	return out, nil
}
