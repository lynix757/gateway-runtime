package httpx

import (
	"net/http"
	"strings"

	"gateway-runtime/internal/auth"
	mw "gateway-runtime/internal/httpx/middleware"
	"gateway-runtime/internal/observability"
)

type RouteRegistrar interface {
	Register(*http.ServeMux)
}

type RouterDeps struct {
	Auth                auth.Handler
	AppRoutes           RouteRegistrar
	Readiness           ReadinessCheck
	PublicURL           string
	SecureCookies       bool
	TrustedProxy        mw.TrustedProxyConfig
	Metrics             *observability.Metrics
	MaxRequestBodyBytes int64
	MaxConcurrency      int
	RatePerSecond       float64
	RateBurst           int
	RateMaxEntries      int
	AuthRatePerSecond   float64
	AuthRateBurst       int
	AccessLogEnabled    bool
	TraceEnabled        bool
	SessionCookieName   string
	CSRFCookieName      string
}

func NewRouter(basePath string, deps RouterDeps) http.Handler {
	app := http.NewServeMux()
	var metricsHandler http.Handler
	if deps.Metrics != nil {
		metricsHandler = deps.Metrics.Handler()
	}
	RegisterSystemRoutes(app, deps.Readiness, metricsHandler)
	deps.Auth.Register(app)
	if deps.AppRoutes != nil {
		deps.AppRoutes.Register(app)
	}

	var appHandler http.Handler = app
	appHandler = mw.HTTPMetrics(deps.Metrics, appHandler)

	var mounted http.Handler = appHandler
	if basePath != "" && basePath != "/" {
		basePath = strings.TrimRight(basePath, "/")
		root := http.NewServeMux()
		root.Handle(basePath+"/", http.StripPrefix(basePath, appHandler))
		mounted = root
	}

	mounted = mw.CSRF(mw.CSRFConfig{
		PublicURL:  deps.PublicURL,
		Secure:     deps.SecureCookies,
		CookieName: deps.CSRFCookieName,
	}, mounted)
	mounted = mw.MaxRequestBodyBytes(deps.MaxRequestBodyBytes, mounted)
	mounted = mw.ConcurrencyLimit(deps.MaxConcurrency, deps.Metrics, mounted)
	mounted = mw.RateLimit(mw.RateLimitConfig{
		RatePerSecond: deps.AuthRatePerSecond,
		Burst:         deps.AuthRateBurst,
		MaxEntries:    deps.RateMaxEntries,
		Proxy:         deps.TrustedProxy,
		Metrics:       deps.Metrics,
		PathSuffixes:  []string{"/auth/login"},
		RejectReason:  "auth_rate_limit",
	}, mounted)
	mounted = mw.RateLimit(mw.RateLimitConfig{
		RatePerSecond: deps.RatePerSecond,
		Burst:         deps.RateBurst,
		MaxEntries:    deps.RateMaxEntries,
		Proxy:         deps.TrustedProxy,
		Metrics:       deps.Metrics,
	}, mounted)
	if deps.AccessLogEnabled {
		mounted = mw.AccessLog(deps.TrustedProxy, mounted)
	}
	mounted = mw.SecurityHeaders(mounted)
	if deps.TraceEnabled {
		mounted = mw.TraceContext(mounted)
	}
	mounted = mw.RequestID(mounted)
	return mounted
}
