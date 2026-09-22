package middleware

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPTrustsForwardedOnlyFromTrustedProxy(t *testing.T) {
	cfg, err := ParseTrustedProxyCIDRs([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}

	trusted := httptest.NewRequest("GET", "http://example.com", nil)
	trusted.RemoteAddr = "10.1.2.3:1234"
	trusted.Header.Set("X-Forwarded-For", "203.0.113.10, 10.1.2.3")
	if got := ClientIP(trusted, cfg); got != "203.0.113.10" {
		t.Fatalf("trusted proxy client ip = %q", got)
	}

	untrusted := httptest.NewRequest("GET", "http://example.com", nil)
	untrusted.RemoteAddr = "192.0.2.55:1234"
	untrusted.Header.Set("X-Forwarded-For", "203.0.113.10")
	if got := ClientIP(untrusted, cfg); got != "192.0.2.55" {
		t.Fatalf("untrusted proxy client ip = %q", got)
	}
}
