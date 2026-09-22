package cache

import (
	"context"
	"time"
)

type Cache interface {
	Get(ctx context.Context, key string, dst any) (found bool, err error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}
