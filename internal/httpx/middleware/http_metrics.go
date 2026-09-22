package middleware

import (
	"net/http"
	"time"

	"gateway-runtime/internal/observability"
)

func HTTPMetrics(metrics *observability.Metrics, next http.Handler) http.Handler {
	if metrics == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		metrics.RecordHTTPRequest(r.Method, route, status, time.Since(start))
	})
}
