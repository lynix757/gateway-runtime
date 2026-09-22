package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssetHandlerLogsAllowDecision(t *testing.T) {
	var buf bytes.Buffer
	h := newAssetHandler(log.New(&buf, "", 0))

	req := httptest.NewRequest(http.MethodGet, "/api/assets/A001", nil)
	req.Header.Set("Authorization", "Bearer alice-token")
	req.Header.Set("X-Request-ID", "req-allow")
	req.Header.Set("X-Trace-ID", "trace-allow")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	got := buf.String()
	for _, want := range []string{
		"component=asset-api",
		"event=request",
		"request_id=req-allow",
		"trace_id=trace-allow",
		"username=alice",
		"asset_id=A001",
		"asset_province=50",
		"decision=allow",
		"event=response",
		"status=200",
		"duration_ms=",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log missing %q: %s", want, got)
		}
	}
}

func TestAssetHandlerLogsDenyDecision(t *testing.T) {
	var buf bytes.Buffer
	h := newAssetHandler(log.New(&buf, "", 0))

	req := httptest.NewRequest(http.MethodGet, "/api/assets/A002", nil)
	req.Header.Set("Authorization", "Bearer alice-token")
	req.Header.Set("X-Request-ID", "req-deny")
	req.Header.Set("X-Trace-ID", "trace-deny")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	got := buf.String()
	for _, want := range []string{
		"component=asset-api",
		"request_id=req-deny",
		"trace_id=trace-deny",
		"username=alice",
		"asset_id=A002",
		"asset_province=10",
		"decision=deny",
		"event=response",
		"status=403",
		"duration_ms=",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log missing %q: %s", want, got)
		}
	}
}

func TestSelectedMockService(t *testing.T) {
	cases := map[string]mockService{
		"asset":   mockServiceAsset,
		"storage": mockServiceStorage,
		"":        mockServiceAll,
	}
	for input, want := range cases {
		if got, err := parseMockService(input); err != nil || got != want {
			t.Fatalf("parseMockService(%q)=(%q,%v) want=(%q,nil)", input, got, err, want)
		}
	}
	if _, err := parseMockService("invalid"); err == nil {
		t.Fatal("parseMockService(invalid) expected error")
	}
}
