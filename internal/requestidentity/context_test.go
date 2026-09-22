package requestidentity

import (
	"net/http/httptest"
	"testing"
)

func TestExtractPrefersActorHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Actor-Subject", "user-001")
	req.Header.Set("X-Auth-Subject", "legacy-user")
	req.Header.Set("X-Service-Identity", "asset-api")
	req.Header.Set("X-Tenant-ID", "tenant-01")
	req.Header.Set("X-Project-ID", "project-22")
	req.Header.Set("X-Request-ID", "req-123")
	req.Header.Set("X-Trace-ID", "trace-456")

	got := Extract(req, Headers{})
	if got.Actor != "user-001" || got.Service != "asset-api" {
		t.Fatalf("identity=%+v", got)
	}
	ctx := got.PolicyContext()
	if ctx["service_id"] != "asset-api" || ctx["tenant_id"] != "tenant-01" || ctx["project_id"] != "project-22" {
		t.Fatalf("context=%+v", ctx)
	}
}

func TestExtractFallsBackToLegacySubject(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Auth-Subject", "legacy-user")

	got := Extract(req, Headers{})
	if got.Actor != "legacy-user" {
		t.Fatalf("identity=%+v", got)
	}
}

func TestValidateActor(t *testing.T) {
	if err := (Identity{}).ValidateActor(); err == nil {
		t.Fatal("expected actor validation error")
	}
}
