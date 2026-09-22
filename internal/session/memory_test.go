package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreExpiredSessionReturnsNotFound(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now()
	store.now = func() time.Time { return now }

	if err := store.Put(context.Background(), Session{
		ID:        "expired",
		Subject:   "user-1",
		ExpiresAt: now.Add(-time.Second),
	}); err != nil {
		t.Fatal(err)
	}

	_, err := store.Get(context.Background(), "expired")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}
