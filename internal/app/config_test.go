package app

import "testing"

func TestNormalizeBasePath(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"/", "", false},
		{"/portal", "/portal", false},
		{"/portal/", "/portal", false},
		{"portal", "", true},
	}

	for _, tc := range tests {
		got, err := normalizeBasePath(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("normalizeBasePath(%q) expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizeBasePath(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("normalizeBasePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPublicEndpointIncludesBasePath(t *testing.T) {
	cfg := HTTPConfig{BasePath: "/portal", PublicURL: "https://example.com"}
	got := cfg.PublicEndpoint("/auth/callback")
	want := "https://example.com/portal/auth/callback"
	if got != want {
		t.Fatalf("PublicEndpoint = %q, want %q", got, want)
	}
}

func TestRedisStoreRequiresURL(t *testing.T) {
	cfg := Config{
		HTTP:  HTTPConfig{Addr: ":8080", PublicURL: "https://example.com"},
		Store: StoreConfig{Backend: "redis"},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected redis backend without URL to fail")
	}

	cfg.Store.RedisURL = "redis://localhost:6379/0"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid redis config failed: %v", err)
	}
}

func TestUnknownStoreBackendRejected(t *testing.T) {
	cfg := Config{
		HTTP:  HTTPConfig{Addr: ":8080", PublicURL: "https://example.com"},
		Store: StoreConfig{Backend: "unknown"},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected unsupported backend to fail")
	}
}

func TestInvalidLimitEnvRejected(t *testing.T) {
	t.Setenv("BFF_LIMIT_MAX_CONCURRENCY", "not-a-number")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected invalid concurrency env to fail")
	}
}

func TestTelemetryFeatureFlags(t *testing.T) {
	t.Setenv("BFF_METRICS_ENABLED", "false")
	t.Setenv("BFF_AUDIT_ENABLED", "false")
	t.Setenv("BFF_ACCESS_LOG_ENABLED", "false")
	t.Setenv("BFF_TRACE_ENABLED", "false")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Telemetry.MetricsEnabled || cfg.Telemetry.AuditEnabled || cfg.Telemetry.AccessLogEnabled || cfg.Telemetry.TraceEnabled {
		t.Fatalf("telemetry flags not disabled: %+v", cfg.Telemetry)
	}
}

func TestInvalidTelemetryFlagRejected(t *testing.T) {
	t.Setenv("BFF_TRACE_ENABLED", "sometimes")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected invalid boolean feature flag to fail")
	}
}
