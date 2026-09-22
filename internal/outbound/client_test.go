package outbound

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mw "gateway-runtime/internal/httpx/middleware"
)

type staticTokenSource struct{ token string }

func (s staticTokenSource) AccessToken(context.Context, string) (string, error) {
	return s.token, nil
}

func TestClientForwardsBearerAndCorrelationHeaders(t *testing.T) {
	var gotAuth, gotReqID, gotTraceID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotReqID = r.Header.Get("X-Request-ID")
		gotTraceID = r.Header.Get("X-Trace-ID")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "yes"})
	}))
	defer srv.Close()

	c, err := New(srv.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.Tokens = staticTokenSource{token: "secret"}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var out map[string]string
		if err := c.DoJSON(r.Context(), "sid", http.MethodGet, "/x", nil, &out); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	handler := mw.RequestID(mw.TraceContext(inner))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.Header.Set("X-Request-ID", "req-1")
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if gotReqID != "req-1" {
		t.Fatalf("request id = %q", gotReqID)
	}
	if gotTraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id = %q", gotTraceID)
	}
}

func TestClientRetriesGETButNotPOST(t *testing.T) {
	getCalls := 0
	postCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			getCalls++
			if getCalls == 1 {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"ok": "yes"})
			return
		}
		postCalls++
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c, _ := New(srv.URL, time.Second)
	c.MaxRetries = 1
	c.RetryBackoff = 0

	var out map[string]string
	if err := c.DoJSON(context.Background(), "", http.MethodGet, "/x", nil, &out); err != nil {
		t.Fatal(err)
	}
	if getCalls != 2 {
		t.Fatalf("GET calls = %d, want 2", getCalls)
	}
	if err := c.DoJSON(context.Background(), "", http.MethodPost, "/x", map[string]string{"a": "b"}, nil); err == nil {
		t.Fatal("expected POST error")
	}
	if postCalls != 1 {
		t.Fatalf("POST calls = %d, want 1", postCalls)
	}
}

func TestClientNormalizesUpstreamErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusForbidden)
	}))
	defer srv.Close()

	c, _ := New(srv.URL, time.Second)
	err := c.DoJSON(context.Background(), "", http.MethodGet, "/x", nil, nil)
	if !IsKind(err, ErrForbidden) {
		t.Fatalf("error = %v", err)
	}
}
