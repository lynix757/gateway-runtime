package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"gateway-runtime/internal/observability"
)

type rateBucket struct {
	tokens float64
	last   time.Time
}

type RateLimitConfig struct {
	RatePerSecond float64
	Burst         int
	IdleTTL       time.Duration
	MaxEntries    int
	Proxy         TrustedProxyConfig
	Metrics       *observability.Metrics
	PathSuffixes  []string
	RejectReason  string
	Now           func() time.Time
}

func RateLimit(cfg RateLimitConfig, next http.Handler) http.Handler {
	if cfg.RatePerSecond <= 0 || cfg.Burst <= 0 {
		return next
	}
	if cfg.IdleTTL <= 0 {
		cfg.IdleTTL = 10 * time.Minute
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 10000
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.RejectReason == "" {
		cfg.RejectReason = "rate_limit"
	}

	var mu sync.Mutex
	buckets := make(map[string]rateBucket)
	var lastSweep time.Time

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if operationalPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if len(cfg.PathSuffixes) > 0 && !matchesPathSuffix(r.URL.Path, cfg.PathSuffixes) {
			next.ServeHTTP(w, r)
			return
		}
		now := cfg.Now()
		key := ClientIP(r, cfg.Proxy)

		mu.Lock()
		if _, exists := buckets[key]; !exists && len(buckets) >= cfg.MaxEntries {
			key = "__overflow__"
		}
		if lastSweep.IsZero() || now.Sub(lastSweep) >= cfg.IdleTTL {
			for k, b := range buckets {
				if now.Sub(b.last) >= cfg.IdleTTL {
					delete(buckets, k)
				}
			}
			lastSweep = now
		}
		b := buckets[key]
		if b.last.IsZero() {
			b.tokens = float64(cfg.Burst)
			b.last = now
		} else {
			b.tokens += now.Sub(b.last).Seconds() * cfg.RatePerSecond
			if b.tokens > float64(cfg.Burst) {
				b.tokens = float64(cfg.Burst)
			}
			b.last = now
		}
		allowed := b.tokens >= 1
		if allowed {
			b.tokens--
		}
		buckets[key] = b
		mu.Unlock()

		if !allowed {
			if cfg.Metrics != nil {
				cfg.Metrics.RecordRejected(cfg.RejectReason)
			}
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func matchesPathSuffix(path string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if suffix != "" && strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}
