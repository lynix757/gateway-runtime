package token

import (
	"context"
	"time"
)

type Set struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiryUnix   int64
}

type Store interface {
	Get(ctx context.Context, sessionID string) (Set, error)
	Put(ctx context.Context, sessionID string, tokens Set, expiresAt time.Time) error
	Delete(ctx context.Context, sessionID string) error
}

type Manager interface {
	AccessToken(ctx context.Context, sessionID string) (string, error)
}
