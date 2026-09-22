package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"testing"

	"gateway-runtime/internal/observability"
)

type benchmarkResponseWriter struct {
	header http.Header
	status int
}

func (w *benchmarkResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *benchmarkResponseWriter) WriteHeader(status int) { w.status = status }
func (w *benchmarkResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return len(p), nil
}
func (w *benchmarkResponseWriter) reset() {
	clear(w.header)
	w.status = 0
}

func BenchmarkMiddlewareOverhead(b *testing.B) {
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(oldLogger) })

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	cases := []struct {
		name string
		make func() http.Handler
	}{
		{name: "baseline", make: func() http.Handler { return final }},
		{name: "request_id", make: func() http.Handler { return RequestID(final) }},
		{name: "trace", make: func() http.Handler { return TraceContext(final) }},
		{name: "metrics", make: func() http.Handler {
			return HTTPMetrics(observability.NewMetrics(), final)
		}},
		{name: "rate_limit", make: func() http.Handler {
			return RateLimit(RateLimitConfig{
				RatePerSecond: 1e9,
				Burst:         1000000,
				MaxEntries:    10000,
			}, final)
		}},
		{name: "access_log", make: func() http.Handler {
			return AccessLog(TrustedProxyConfig{}, final)
		}},
		{name: "full_hot_path", make: func() http.Handler {
			var h http.Handler = final
			h = HTTPMetrics(observability.NewMetrics(), h)
			h = ConcurrencyLimit(256, nil, h)
			h = RateLimit(RateLimitConfig{
				RatePerSecond: 1e9,
				Burst:         1000000,
				MaxEntries:    10000,
			}, h)
			h = AccessLog(TrustedProxyConfig{}, h)
			h = SecurityHeaders(h)
			h = TraceContext(h)
			h = RequestID(h)
			return h
		}},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			h := tc.make()
			req := &http.Request{
				Method:     http.MethodGet,
				URL:        &url.URL{Path: "/api/benchmark"},
				Header:     make(http.Header),
				RemoteAddr: "127.0.0.1:12345",
			}
			w := &benchmarkResponseWriter{header: make(http.Header)}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w.reset()
				h.ServeHTTP(w, req)
			}
		})
	}
}
