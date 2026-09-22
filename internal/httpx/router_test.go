package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gateway-runtime/internal/auth"
	"gateway-runtime/internal/cache"
	"gateway-runtime/internal/session"
	"gateway-runtime/internal/token"
	"gateway-runtime/internal/usercontext"
)

func testDeps() RouterDeps {
	sessions := session.NewMemoryStore()
	c := cache.NewMemory()
	resolver := usercontext.CachedResolver{
		Next:  usercontext.SessionResolver{Sessions: sessions},
		Cache: c,
	}
	return RouterDeps{
		Auth: auth.Handler{
			Service: &auth.Service{
				Provider:     auth.UnconfiguredProvider{},
				Flows:        auth.NewMemoryFlowStore(),
				Sessions:     sessions,
				Tokens:       token.NewMemoryStore(),
				RedirectURI:  "https://example.com/auth/callback",
				LogoutReturn: "https://example.com/",
			},
			UserContext: resolver,
			Cache:       c,
			Secure:      true,
		},
		PublicURL:        "https://example.com",
		SecureCookies:    true,
		AccessLogEnabled: true,
		TraceEnabled:     true,
	}
}

func TestNewRouterRootMode(t *testing.T) {
	h := NewRouter("", testDeps())
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing request id")
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing security headers")
	}
}

func TestNewRouterPrefixMode(t *testing.T) {
	h := NewRouter("/portal", testDeps())

	for _, tc := range []struct {
		path string
		want int
	}{
		{path: "/portal/health/live", want: http.StatusOK},
		{path: "/health/live", want: http.StatusNotFound},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("%s status = %d, want %d", tc.path, rec.Code, tc.want)
		}
	}
}

func TestRouterCanDisableMetricsAndTrace(t *testing.T) {
	deps := testDeps()
	deps.Metrics = nil
	deps.TraceEnabled = false
	deps.AccessLogEnabled = false
	h := NewRouter("", deps)

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	h.ServeHTTP(metricsRec, metricsReq)
	if metricsRec.Code != http.StatusNotFound {
		t.Fatalf("/metrics status = %d, want 404", metricsRec.Code)
	}

	liveReq := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	liveRec := httptest.NewRecorder()
	h.ServeHTTP(liveRec, liveReq)
	if liveRec.Code != http.StatusOK {
		t.Fatalf("live status = %d", liveRec.Code)
	}
	if liveRec.Header().Get("X-Trace-ID") != "" {
		t.Fatal("trace header present while trace disabled")
	}
	if liveRec.Header().Get("X-Request-ID") == "" {
		t.Fatal("request id should remain enabled")
	}
}
