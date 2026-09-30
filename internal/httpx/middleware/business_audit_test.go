package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gateway-runtime/internal/audit"
)

func TestBusinessAuditMapsResponseStatusToOutcome(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		outcome string
	}{
		{name: "denied", status: http.StatusForbidden, outcome: "deny"},
		{name: "failed", status: http.StatusServiceUnavailable, outcome: "failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := &audit.MemorySink{}
			h := BusinessAudit("asset.read", "asset", sink, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			req := httptest.NewRequest(http.MethodGet, "/api/assets/a-1", nil)
			req.SetPathValue("id", "a-1")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			events := sink.Snapshot()
			if len(events) != 1 || events[0].Outcome != tc.outcome || events[0].HTTPStatus != tc.status || events[0].ResourceID != "a-1" {
				t.Fatalf("unexpected audit events: %+v", events)
			}
		})
	}
}
