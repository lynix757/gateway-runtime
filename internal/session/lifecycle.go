package session

import (
	"context"
	"time"
)

type LifecycleStore struct {
	Next          Store
	MaxLifetime   time.Duration
	IdleTimeout   time.Duration
	TouchInterval time.Duration
	Now           func() time.Time
}

func (s LifecycleStore) Get(ctx context.Context, sessionID string) (Session, error) {
	v, err := s.Next.Get(ctx, sessionID)
	if err != nil {
		return Session{}, err
	}

	now := s.now()
	if s.MaxLifetime > 0 && !v.CreatedAt.IsZero() && !v.CreatedAt.Add(s.MaxLifetime).After(now) {
		_ = s.Next.Delete(ctx, sessionID)
		return Session{}, ErrNotFound
	}
	if s.IdleTimeout > 0 && !v.LastSeen.IsZero() && !v.LastSeen.Add(s.IdleTimeout).After(now) {
		_ = s.Next.Delete(ctx, sessionID)
		return Session{}, ErrNotFound
	}

	touch := s.TouchInterval
	if touch <= 0 {
		touch = time.Minute
	}
	if s.IdleTimeout > 0 && (v.LastSeen.IsZero() || now.Sub(v.LastSeen) >= touch) {
		v.LastSeen = now
		if err := s.Next.Put(ctx, v); err != nil {
			return Session{}, err
		}
	}
	return v, nil
}

func (s LifecycleStore) Put(ctx context.Context, v Session) error {
	now := s.now()
	if v.CreatedAt.IsZero() {
		v.CreatedAt = now
	}
	if v.LastSeen.IsZero() {
		v.LastSeen = now
	}
	if s.MaxLifetime > 0 {
		absolute := v.CreatedAt.Add(s.MaxLifetime)
		if v.ExpiresAt.IsZero() || v.ExpiresAt.After(absolute) {
			v.ExpiresAt = absolute
		}
	}
	return s.Next.Put(ctx, v)
}

func (s LifecycleStore) Delete(ctx context.Context, sessionID string) error {
	return s.Next.Delete(ctx, sessionID)
}

func (s LifecycleStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
