package middleware

import (
	"net/http"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
)

func BenchmarkRateLimiterConcurrentSameClient(b *testing.B) {
	h := RateLimit(RateLimitConfig{
		RatePerSecond: 1e12,
		Burst:         1000000000,
		MaxEntries:    10000,
	}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		w := &benchmarkResponseWriter{header: make(http.Header)}
		req := &http.Request{
			Method:     http.MethodGet,
			URL:        &url.URL{Path: "/api/benchmark"},
			Header:     make(http.Header),
			RemoteAddr: "127.0.0.1:12345",
		}
		for pb.Next() {
			w.reset()
			h.ServeHTTP(w, req)
		}
	})
}

func BenchmarkRateLimiterConcurrentManyClients(b *testing.B) {
	h := RateLimit(RateLimitConfig{
		RatePerSecond: 1e12,
		Burst:         1000000000,
		MaxEntries:    10000,
	}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	var seq atomic.Uint64
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		w := &benchmarkResponseWriter{header: make(http.Header)}
		req := &http.Request{
			Method: http.MethodGet,
			URL:    &url.URL{Path: "/api/benchmark"},
			Header: make(http.Header),
		}
		for pb.Next() {
			n := seq.Add(1) % 10000
			req.RemoteAddr = "10.0." + strconv.FormatUint((n/250)%250, 10) + "." + strconv.FormatUint(n%250+1, 10) + ":12345"
			w.reset()
			h.ServeHTTP(w, req)
		}
	})
}
