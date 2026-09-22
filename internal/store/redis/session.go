package redisstore

import (
	"context"
	"encoding/json"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"gateway-runtime/internal/session"
)

type SessionStore struct{ Client *Client }

func (s SessionStore) Get(ctx context.Context, sessionID string) (session.Session, error) {
	raw, err := s.Client.RDB.Get(ctx, s.Client.key("session", sessionID)).Bytes()
	if err == goredis.Nil {
		return session.Session{}, session.ErrNotFound
	}
	if err != nil {
		return session.Session{}, err
	}
	var v session.Session
	if err := json.Unmarshal(raw, &v); err != nil {
		return session.Session{}, err
	}
	if !v.ExpiresAt.IsZero() && !v.ExpiresAt.After(time.Now()) {
		_ = s.Delete(ctx, sessionID)
		return session.Session{}, session.ErrNotFound
	}
	return v, nil
}

func (s SessionStore) Put(ctx context.Context, v session.Session) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	ttl := time.Until(v.ExpiresAt)
	if ttl <= 0 {
		return session.ErrNotFound
	}
	return s.Client.RDB.Set(ctx, s.Client.key("session", v.ID), b, ttl).Err()
}

func (s SessionStore) Delete(ctx context.Context, sessionID string) error {
	return s.Client.RDB.Del(ctx, s.Client.key("session", sessionID)).Err()
}
