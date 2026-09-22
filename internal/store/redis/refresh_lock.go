package redisstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type RefreshLocker struct{ Client *Client }

func (l RefreshLocker) Lock(ctx context.Context, key string, ttl time.Duration) (func() error, error) {
	if ttl <= 0 {
		ttl = 10 * time.Second
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	value := hex.EncodeToString(b[:])
	redisKey := l.Client.key("lock", key)

	for {
		ok, err := l.Client.RDB.SetNX(ctx, redisKey, value, ttl).Result()
		if err != nil {
			return nil, err
		}
		if ok {
			break
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	return func() error {
		script := "if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) end return 0"
		if err := l.Client.RDB.Eval(context.Background(), script, []string{redisKey}, value).Err(); err != nil {
			return fmt.Errorf("release refresh lock: %w", err)
		}
		return nil
	}, nil
}
