package session

import (
	"context"
	"time"
)

type Session struct {
	ID          string
	Subject     string
	Issuer      string
	DisplayName string
	Email       string
	Roles       []string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	LastSeen    time.Time
}

type Store interface {
	Get(ctx context.Context, sessionID string) (Session, error)
	Put(ctx context.Context, s Session) error
	Delete(ctx context.Context, sessionID string) error
}
