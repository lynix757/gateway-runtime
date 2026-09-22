package middleware

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestRedactedHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer secret")
	h.Set("Cookie", "session=secret")
	h.Set("X-Test", "ok")

	got := RedactedHeaders(h)
	if got.Get("Authorization") != "[REDACTED]" || got.Get("Cookie") != "[REDACTED]" {
		t.Fatal("sensitive headers not redacted")
	}
	if got.Get("X-Test") != "ok" {
		t.Fatal("non-sensitive header changed")
	}
}

func TestRedactedURL(t *testing.T) {
	u, _ := url.Parse("https://example.com/callback?code=secret&state=ok&X-Amz-Signature=sig")
	got := RedactedURL(u)
	if strings.Contains(got, "secret") || strings.Contains(got, "sig") {
		t.Fatalf("sensitive query leaked: %s", got)
	}
	if !strings.Contains(got, "state=ok") {
		t.Fatalf("non-sensitive query missing: %s", got)
	}
}
