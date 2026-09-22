package token

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gateway-runtime/internal/session"
)

var (
	ErrRefreshUnavailable = errors.New("token refresh unavailable")
	ErrRefreshFailed      = errors.New("token refresh failed")
)

type Refresher interface {
	RefreshTokens(ctx context.Context, refreshToken string) (Set, error)
}

type RefreshLocker interface {
	Lock(ctx context.Context, key string, ttl time.Duration) (unlock func() error, err error)
}

type StoreManager struct {
	Store         Store
	Sessions      session.Store
	Refresher     Refresher
	Locker        RefreshLocker
	Now           func() time.Time
	RefreshBefore time.Duration
	LockTTL       time.Duration
}

func (m StoreManager) AccessToken(ctx context.Context, sessionID string) (string, error) {
	if m.Store == nil {
		return "", fmt.Errorf("token store is not configured")
	}

	current, err := m.Store.Get(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if current.AccessToken == "" {
		return "", fmt.Errorf("access token missing")
	}
	if !m.needsRefresh(current) {
		return current.AccessToken, nil
	}
	if current.RefreshToken == "" || m.Refresher == nil {
		return "", ErrRefreshUnavailable
	}

	unlock := func() error { return nil }
	if m.Locker != nil {
		unlock, err = m.Locker.Lock(ctx, "token-refresh:"+sessionID, m.lockTTL())
		if err != nil {
			return "", fmt.Errorf("acquire refresh lock: %w", err)
		}
		defer func() { _ = unlock() }()
	}

	// Another replica may have refreshed while this request waited for the lock.
	current, err = m.Store.Get(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if !m.needsRefresh(current) {
		return current.AccessToken, nil
	}
	if current.RefreshToken == "" {
		return "", ErrRefreshUnavailable
	}

	refreshed, err := m.Refresher.RefreshTokens(ctx, current.RefreshToken)
	if err != nil {
		m.invalidate(ctx, sessionID)
		return "", fmt.Errorf("%w: %v", ErrRefreshFailed, err)
	}
	if refreshed.AccessToken == "" || refreshed.ExpiryUnix <= m.now().Unix() {
		m.invalidate(ctx, sessionID)
		return "", fmt.Errorf("%w: invalid refreshed token set", ErrRefreshFailed)
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = current.RefreshToken
	}
	if refreshed.IDToken == "" {
		refreshed.IDToken = current.IDToken
	}

	expiresAt, err := m.sessionExpiry(ctx, sessionID)
	if err != nil {
		m.invalidate(ctx, sessionID)
		return "", err
	}
	if err := m.Store.Put(ctx, sessionID, refreshed, expiresAt); err != nil {
		return "", fmt.Errorf("persist refreshed token: %w", err)
	}
	return refreshed.AccessToken, nil
}

func (m StoreManager) needsRefresh(set Set) bool {
	if set.ExpiryUnix <= 0 {
		return false
	}
	before := m.RefreshBefore
	if before <= 0 {
		before = 30 * time.Second
	}
	return !time.Unix(set.ExpiryUnix, 0).After(m.now().Add(before))
}

func (m StoreManager) sessionExpiry(ctx context.Context, sessionID string) (time.Time, error) {
	if m.Sessions == nil {
		return time.Time{}, fmt.Errorf("session store is required for token refresh")
	}
	s, err := m.Sessions.Get(ctx, sessionID)
	if err != nil {
		return time.Time{}, err
	}
	return s.ExpiresAt, nil
}

func (m StoreManager) invalidate(ctx context.Context, sessionID string) {
	_ = m.Store.Delete(ctx, sessionID)
	if m.Sessions != nil {
		_ = m.Sessions.Delete(ctx, sessionID)
	}
}

func (m StoreManager) lockTTL() time.Duration {
	if m.LockTTL > 0 {
		return m.LockTTL
	}
	return 10 * time.Second
}

func (m StoreManager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}
