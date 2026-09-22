package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func AccessLog(proxy TrustedProxyConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}

		if successfulHealthCheck(r.URL.Path, status) {
			return
		}

		slog.Info("http request",
			"request_id", RequestIDFromContext(r.Context()),
			"trace_id", TraceIDFromContext(r.Context()),
			"method", r.Method,
			"url", RedactedURL(r.URL),
			"client_ip", ClientIP(r, proxy),
			"status", status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func successfulHealthCheck(path string, status int) bool {
	if status < http.StatusOK || status >= http.StatusBadRequest {
		return false
	}
	return strings.HasSuffix(path, "/health/live") ||
		strings.HasSuffix(path, "/health/ready")
}
