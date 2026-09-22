package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsHandlerLabeledSeriesAndHistograms(t *testing.T) {
	m := NewMetrics()
	m.RecordHTTPRequest(http.MethodGet, "GET /api/me", 200, 25*time.Millisecond)
	m.RecordHTTPRequest(http.MethodGet, "GET /api/me", 503, 100*time.Millisecond)
	m.RecordAuth("success")
	m.RecordAuth("failure")
	m.RecordAuthorization("allowed", "asset.read")
	m.RecordAuthorization("denied", "asset.write")
	m.RecordOutbound("domain-api", http.MethodGet, "success", 50*time.Millisecond)
	m.RecordRejected("rate_limit")
	m.RecordOutboundRejected("domain-api", "circuit")
	m.AddInflight(2)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	m.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		"rebff_http_requests_total{method=\"GET\",route=\"GET /api/me\",status_class=\"2xx\"} 1",
		"rebff_http_requests_total{method=\"GET\",route=\"GET /api/me\",status_class=\"5xx\"} 1",
		"rebff_http_request_duration_seconds_count{method=\"GET\",route=\"GET /api/me\"} 2",
		"rebff_auth_attempts_total{outcome=\"success\"} 1",
		"rebff_auth_attempts_total{outcome=\"failure\"} 1",
		"rebff_authorization_decisions_total{outcome=\"allowed\",permission=\"asset.read\"} 1",
		"rebff_authorization_decisions_total{outcome=\"denied\",permission=\"asset.write\"} 1",
		"rebff_outbound_requests_total{service=\"domain-api\",method=\"GET\",outcome=\"success\"} 1",
		"rebff_outbound_request_duration_seconds_count{service=\"domain-api\",method=\"GET\"} 1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q:\n%s", want, body)
		}
	}
}
