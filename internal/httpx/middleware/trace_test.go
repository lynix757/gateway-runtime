package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTraceContextAcceptsValidTraceparent(t *testing.T) {
	h := TraceContext(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := TraceIDFromContext(r.Context()); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Fatalf("trace id = %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Header().Get("X-Trace-ID") != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("missing propagated trace id")
	}
}

func TestTraceContextGeneratesTraceID(t *testing.T) {
	h := TraceContext(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if len(rec.Header().Get("X-Trace-ID")) != 32 {
		t.Fatalf("generated trace id = %q", rec.Header().Get("X-Trace-ID"))
	}
}
