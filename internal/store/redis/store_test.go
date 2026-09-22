package redisstore

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"gateway-runtime/internal/auth"
	"gateway-runtime/internal/session"
	"gateway-runtime/internal/token"
)

func testClient(t *testing.T) (*Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	return &Client{RDB: rdb, Prefix: "test"}, mr
}

func TestFlowStoreTakeIsOneTime(t *testing.T) {
	client, mr := testClient(t)
	defer mr.Close()
	defer client.Close()

	store := FlowStore{Client: client}
	flow := auth.Flow{
		State: "state-1", Nonce: "nonce", CodeVerifier: "verifier",
		ReturnTo: "/", ExpiresAt: time.Now().Add(time.Minute),
	}
	if err := store.Put(context.Background(), flow); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Take(context.Background(), flow.State); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Take(context.Background(), flow.State); err != auth.ErrFlowNotFound {
		t.Fatalf("second Take error = %v, want %v", err, auth.ErrFlowNotFound)
	}
}

func TestSessionAndTokenExpireWithSessionTTL(t *testing.T) {
	client, mr := testClient(t)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()
	expires := time.Now().Add(2 * time.Minute)
	ss := SessionStore{Client: client}
	ts := TokenStore{Client: client}

	sess := session.Session{
		ID: "sid", Subject: "user-1", Issuer: "issuer",
		CreatedAt: time.Now(), LastSeen: time.Now(), ExpiresAt: expires,
	}
	if err := ss.Put(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if err := ts.Put(ctx, sess.ID, token.Set{AccessToken: "a", RefreshToken: "r"}, expires); err != nil {
		t.Fatal(err)
	}
	if _, err := ss.Get(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.Get(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}

	mr.FastForward(3 * time.Minute)

	if _, err := ss.Get(ctx, sess.ID); err != session.ErrNotFound {
		t.Fatalf("session error = %v, want %v", err, session.ErrNotFound)
	}
	if _, err := ts.Get(ctx, sess.ID); err != token.ErrNotFound {
		t.Fatalf("token error = %v, want %v", err, token.ErrNotFound)
	}
}
