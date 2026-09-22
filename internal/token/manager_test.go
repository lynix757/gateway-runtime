package token

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gateway-runtime/internal/session"
)

type fakeRefresher struct {
	calls atomic.Int32
	out   Set
	err   error
	delay time.Duration
}

func (f *fakeRefresher) RefreshTokens(ctx context.Context, refreshToken string) (Set, error) {
	f.calls.Add(1)
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return Set{}, ctx.Err()
		case <-time.After(f.delay):
		}
	}
	return f.out, f.err
}

func TestStoreManagerAccessToken(t *testing.T) {
	store := NewMemoryStore()
	expires := time.Now().Add(time.Hour)
	if err := store.Put(context.Background(), "sid", Set{
		AccessToken: "token-1",
		ExpiryUnix:  expires.Unix(),
	}, expires); err != nil {
		t.Fatal(err)
	}
	m := StoreManager{Store: store}
	got, err := m.AccessToken(context.Background(), "sid")
	if err != nil {
		t.Fatal(err)
	}
	if got != "token-1" {
		t.Fatalf("token = %q", got)
	}
}

func TestStoreManagerRefreshesAndRotatesToken(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	tokens := NewMemoryStore()
	sessions := session.NewMemoryStore()
	sessionExpiry := now.Add(time.Hour)
	if err := sessions.Put(context.Background(), session.Session{
		ID: "sid", Subject: "u1", ExpiresAt: sessionExpiry,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tokens.Put(context.Background(), "sid", Set{
		AccessToken: "old-access", RefreshToken: "old-refresh",
		ExpiryUnix: now.Add(5 * time.Second).Unix(),
	}, sessionExpiry); err != nil {
		t.Fatal(err)
	}
	refresher := &fakeRefresher{out: Set{
		AccessToken: "new-access", RefreshToken: "new-refresh",
		ExpiryUnix: now.Add(10 * time.Minute).Unix(),
	}}
	m := StoreManager{
		Store: tokens, Sessions: sessions, Refresher: refresher,
		Locker: NewMemoryLocker(), Now: func() time.Time { return now },
		RefreshBefore: 30 * time.Second,
	}
	got, err := m.AccessToken(context.Background(), "sid")
	if err != nil {
		t.Fatal(err)
	}
	if got != "new-access" {
		t.Fatalf("token = %q", got)
	}
	stored, err := tokens.Get(context.Background(), "sid")
	if err != nil {
		t.Fatal(err)
	}
	if stored.RefreshToken != "new-refresh" {
		t.Fatalf("refresh token = %q", stored.RefreshToken)
	}
}

func TestStoreManagerRefreshFailureInvalidatesSession(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	tokens := NewMemoryStore()
	sessions := session.NewMemoryStore()
	exp := now.Add(time.Hour)
	if err := sessions.Put(context.Background(), session.Session{ID: "sid", Subject: "u1", ExpiresAt: exp}); err != nil {
		t.Fatal(err)
	}
	if err := tokens.Put(context.Background(), "sid", Set{
		AccessToken: "old", RefreshToken: "revoked", ExpiryUnix: now.Unix(),
	}, exp); err != nil {
		t.Fatal(err)
	}
	m := StoreManager{
		Store: tokens, Sessions: sessions,
		Refresher: &fakeRefresher{err: errors.New("invalid_grant")},
		Locker:    NewMemoryLocker(), Now: func() time.Time { return now },
	}
	if _, err := m.AccessToken(context.Background(), "sid"); !errors.Is(err, ErrRefreshFailed) {
		t.Fatalf("err = %v, want ErrRefreshFailed", err)
	}
	if _, err := sessions.Get(context.Background(), "sid"); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("session err = %v, want ErrNotFound", err)
	}
}

func TestStoreManagerConcurrentRefreshOnlyOnce(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	tokens := NewMemoryStore()
	sessions := session.NewMemoryStore()
	exp := now.Add(time.Hour)
	if err := sessions.Put(context.Background(), session.Session{ID: "sid", Subject: "u1", ExpiresAt: exp}); err != nil {
		t.Fatal(err)
	}
	if err := tokens.Put(context.Background(), "sid", Set{
		AccessToken: "old", RefreshToken: "refresh", ExpiryUnix: now.Unix(),
	}, exp); err != nil {
		t.Fatal(err)
	}
	refresher := &fakeRefresher{
		delay: 20 * time.Millisecond,
		out:   Set{AccessToken: "new", RefreshToken: "rotated", ExpiryUnix: now.Add(time.Hour).Unix()},
	}
	m := StoreManager{
		Store: tokens, Sessions: sessions, Refresher: refresher,
		Locker: NewMemoryLocker(), Now: func() time.Time { return now },
	}

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := m.AccessToken(context.Background(), "sid")
			if err == nil && got != "new" {
				err = errors.New("unexpected token")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := refresher.calls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
}
