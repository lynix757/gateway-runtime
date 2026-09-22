package integration

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"gateway-runtime/internal/auth"
	"gateway-runtime/internal/httpx"
	"gateway-runtime/internal/observability"
	redisstore "gateway-runtime/internal/store/redis"
	"gateway-runtime/internal/usercontext"
)

func TestRedisOutageReadinessAndAuthenticatedAPI(t *testing.T) {
	if os.Getenv("REBFF_INTEGRATION") != "1" {
		t.Skip("set REBFF_INTEGRATION=1 to run integration tests")
	}

	client, err := redisstore.New("redis://localhost:16379/0", "rebff-it-outage")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Ping(t.Context()); err != nil {
		t.Fatalf("redis unavailable: %v", err)
	}

	sessions := redisstore.SessionStore{Client: client}
	handler := auth.Handler{
		Service: &auth.Service{Sessions: sessions},
		UserContext: usercontext.SessionResolver{
			Sessions: sessions,
		},
		Metrics: observability.NewMetrics(),
	}

	router := httpx.NewRouter("", httpx.RouterDeps{
		Auth:      handler,
		Readiness: client.Ping,
		PublicURL: "http://localhost",
		Metrics:   observability.NewMetrics(),
	})

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	readyReq := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	readyRec := httptest.NewRecorder()
	router.ServeHTTP(readyRec, readyReq)
	if readyRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d, want 503", readyRec.Code)
	}

	liveReq := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	liveRec := httptest.NewRecorder()
	router.ServeHTTP(liveRec, liveReq)
	if liveRec.Code != http.StatusOK {
		t.Fatalf("liveness status = %d, want 200", liveRec.Code)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "sid"})
	meRec := httptest.NewRecorder()
	router.ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/api/me status = %d, want 503", meRec.Code)
	}
}
