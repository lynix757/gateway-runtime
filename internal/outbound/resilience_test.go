package outbound

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientTimeoutIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := New(srv.URL, 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	c.MaxRetries = 0

	start := time.Now()
	err = c.DoJSON(context.Background(), "", http.MethodGet, "/slow", nil, nil)
	if err == nil {
		t.Fatal("expected timeout")
	}
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Fatalf("timeout not bounded, elapsed=%s", elapsed)
	}
}

func TestClientStopsBackoffWhenContextCancelled(t *testing.T) {
	c, err := New("http://127.0.0.1:1", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.MaxRetries = 3
	c.RetryBackoff = time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = c.DoJSON(ctx, "", http.MethodGet, "/x", nil, nil)
	if err == nil {
		t.Fatal("expected request failure")
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("cancel did not stop retry backoff, elapsed=%s", elapsed)
	}
}
