package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gateway-runtime/internal/observability"
)

func TestRateLimitRejectsAndRefills(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cfg := RateLimitConfig{
		RatePerSecond: 1,
		Burst:         2,
		Now:           func() time.Time { return now },
	}
	h := RateLimit(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/x", nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("request %d status = %d", i, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/x", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}

	now = now.Add(time.Second)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/x", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("refilled status = %d", rec.Code)
	}
}

func TestOperationalEndpointsBypassLimiters(t *testing.T) {
	metrics := observability.NewMetrics()
	var calls int
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})
	h := RateLimit(RateLimitConfig{RatePerSecond: 1, Burst: 1, Metrics: metrics},
		ConcurrencyLimit(1, metrics, next))

	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/live", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("health status = %d", rec.Code)
		}
	}
	if calls != 5 {
		t.Fatalf("calls = %d, want 5", calls)
	}
}

func TestConcurrencyLimitRejectsWhenFull(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	h := ConcurrencyLimit(1, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/x", nil))
		if rec.Code != http.StatusNoContent {
			t.Errorf("first status = %d", rec.Code)
		}
	}()

	<-started
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/x", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("second status = %d, want 503", rec.Code)
	}
	close(release)
	wg.Wait()
}

func TestMaxRequestBodyBytes(t *testing.T) {
	h := MaxRequestBodyBytes(4, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/x", strings.NewReader("12345"))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestPathSpecificRateLimitOnlyMatchesLogin(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	h := RateLimit(RateLimitConfig{
		RatePerSecond: 1,
		Burst:         1,
		PathSuffixes:  []string{"/auth/login"},
		Now:           func() time.Time { return now },
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/me", nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("/api/me status = %d", rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("first login status = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second login status = %d, want 429", rec.Code)
	}
}
