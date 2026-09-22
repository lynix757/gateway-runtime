package outbound

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type doerFunc func(context.Context, string, string, string, any, any) error

func (f doerFunc) DoJSON(ctx context.Context, sid, method, path string, in, out any) error {
	return f(ctx, sid, method, path, in, out)
}

func TestProtectedClientCircuitBreakerOpensAndRecovers(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	var calls atomic.Int32
	fail := true
	next := doerFunc(func(context.Context, string, string, string, any, any) error {
		calls.Add(1)
		if fail {
			return &Error{Kind: ErrUnavailable, StatusCode: 503}
		}
		return nil
	})
	c := &ProtectedClient{
		Next: next, FailureThreshold: 2, OpenFor: time.Minute,
		Now: func() time.Time { return now },
	}

	for i := 0; i < 2; i++ {
		if err := c.DoJSON(context.Background(), "", http.MethodGet, "/", nil, nil); err == nil {
			t.Fatal("expected failure")
		}
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}

	if err := c.DoJSON(context.Background(), "", http.MethodGet, "/", nil, nil); err == nil {
		t.Fatal("expected open circuit")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("open circuit called upstream; calls=%d", got)
	}

	now = now.Add(time.Minute)
	fail = false
	if err := c.DoJSON(context.Background(), "", http.MethodGet, "/", nil, nil); err != nil {
		t.Fatalf("half-open probe failed: %v", err)
	}
	if err := c.DoJSON(context.Background(), "", http.MethodGet, "/", nil, nil); err != nil {
		t.Fatalf("closed circuit failed: %v", err)
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("calls = %d, want 4", got)
	}
}

func TestProtectedClientBulkheadRejectsWithoutOpeningCircuit(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	next := doerFunc(func(context.Context, string, string, string, any, any) error {
		once.Do(func() { close(started) })
		<-release
		return nil
	})
	c := &ProtectedClient{
		Next: next, MaxConcurrent: 1, FailureThreshold: 1, OpenFor: time.Hour,
	}

	done := make(chan error, 1)
	go func() {
		done <- c.DoJSON(context.Background(), "", http.MethodGet, "/", nil, nil)
	}()
	<-started

	err := c.DoJSON(context.Background(), "", http.MethodGet, "/", nil, nil)
	if !IsKind(err, ErrUnavailable) {
		t.Fatalf("bulkhead err = %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if err := c.DoJSON(context.Background(), "", http.MethodGet, "/", nil, nil); err != nil {
		t.Fatalf("bulkhead saturation opened circuit: %v", err)
	}
}

func TestProtectedClientCancelledRequestDoesNotTripCircuit(t *testing.T) {
	next := doerFunc(func(context.Context, string, string, string, any, any) error {
		return context.Canceled
	})
	c := &ProtectedClient{Next: next, FailureThreshold: 1}

	if err := c.DoJSON(context.Background(), "", http.MethodGet, "/", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if err := c.DoJSON(context.Background(), "", http.MethodGet, "/", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("circuit unexpectedly opened: %v", err)
	}
}
