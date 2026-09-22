package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLifecycleStoreIdleTimeoutAndTouch(t *testing.T) {
	base := NewMemoryStore()
	now := time.Unix(1_700_000_000, 0)
	base.now = func() time.Time { return now }

	store := LifecycleStore{
		Next:          base,
		MaxLifetime:   8 * time.Hour,
		IdleTimeout:   30 * time.Minute,
		TouchInterval: time.Minute,
		Now:           func() time.Time { return now },
	}

	if err := store.Put(context.Background(), Session{ID: "sid", Subject: "u1"}); err != nil {
		t.Fatal(err)
	}

	now = now.Add(2 * time.Minute)
	got, err := store.Get(context.Background(), "sid")
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastSeen.Equal(now) {
		t.Fatalf("LastSeen = %v, want %v", got.LastSeen, now)
	}

	now = now.Add(31 * time.Minute)
	_, err = store.Get(context.Background(), "sid")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestLifecycleStoreAbsoluteLifetime(t *testing.T) {
	base := NewMemoryStore()
	now := time.Unix(1_700_000_000, 0)
	base.now = func() time.Time { return now }

	store := LifecycleStore{
		Next:        base,
		MaxLifetime: time.Hour,
		IdleTimeout: 30 * time.Minute,
		Now:         func() time.Time { return now },
	}

	if err := store.Put(context.Background(), Session{ID: "sid", Subject: "u1"}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	_, err := store.Get(context.Background(), "sid")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
