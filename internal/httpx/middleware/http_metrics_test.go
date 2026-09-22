package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gateway-runtime/internal/observability"
)

func TestHTTPMetricsUsesServeMuxPatternNotRawID(t *testing.T) {
	metrics := observability.NewMetrics()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := HTTPMetrics(metrics, mux)
	req := httptest.NewRequest(http.MethodGet, "/api/items/asset-123456", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	metricsRec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(metricsRec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := metricsRec.Body.String()

	if !strings.Contains(body, "route=\"GET /api/items/{id}\"") {
		t.Fatalf("route pattern missing: %s", body)
	}
	if strings.Contains(body, "asset-123456") {
		t.Fatalf("raw resource id leaked into metric label: %s", body)
	}
}
