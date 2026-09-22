package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessLogSuppressesSuccessfulHealthChecks(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := AccessLog(TrustedProxyConfig{}, next)

	for _, path := range []string{"/health/live", "/health/ready", "/base/health/live", "/base/health/ready"} {
		buf.Reset()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := buf.String(); got != "" {
			t.Fatalf("path=%s expected no access log, got %q", path, got)
		}
	}
}

func TestAccessLogKeepsFailedHealthChecks(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	})
	h := AccessLog(TrustedProxyConfig{}, next)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	got := buf.String()
	if !strings.Contains(got, "status=503") || !strings.Contains(got, "url=/health/ready") {
		t.Fatalf("expected failed health access log, got %q", got)
	}
}

func TestAccessLogKeepsSuccessfulApplicationRequests(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := AccessLog(TrustedProxyConfig{}, next)

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	got := buf.String()
	if !strings.Contains(got, "status=200") || !strings.Contains(got, "url=/api/me") {
		t.Fatalf("expected application access log, got %q", got)
	}
}
