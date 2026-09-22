package middleware

import (
	"net/http"

	"gateway-runtime/internal/observability"
)

func ConcurrencyLimit(max int, metrics *observability.Metrics, next http.Handler) http.Handler {
	if max <= 0 {
		return next
	}
	sem := make(chan struct{}, max)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if operationalPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		select {
		case sem <- struct{}{}:
			if metrics != nil {
				metrics.AddInflight(1)
			}
			defer func() {
				<-sem
				if metrics != nil {
					metrics.AddInflight(-1)
				}
			}()
			next.ServeHTTP(w, r)
		default:
			if metrics != nil {
				metrics.RecordRejected("concurrency")
			}
			w.Header().Set("Retry-After", "1")
			http.Error(w, "server busy", http.StatusServiceUnavailable)
		}
	})
}
