package redisstore

import (
	"context"
	"encoding/json"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"gateway-runtime/internal/token"
)

type TokenStore struct{ Client *Client }

func (s TokenStore) Get(ctx context.Context, sessionID string) (token.Set, error) {
	raw, err := s.Client.RDB.Get(ctx, s.Client.key("token", sessionID)).Bytes()
	if err == goredis.Nil {
		return token.Set{}, token.ErrNotFound
	}
	if err != nil {
		return token.Set{}, err
	}
	var v token.Set
	if err := json.Unmarshal(raw, &v); err != nil {
		return token.Set{}, err
	}
	return v, nil
}

func (s TokenStore) Put(ctx context.Context, sessionID string, v token.Set, expiresAt time.Time) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return token.ErrNotFound
	}
	return s.Client.RDB.Set(ctx, s.Client.key("token", sessionID), b, ttl).Err()
}

func (s TokenStore) Delete(ctx context.Context, sessionID string) error {
	return s.Client.RDB.Del(ctx, s.Client.key("token", sessionID)).Err()
}
