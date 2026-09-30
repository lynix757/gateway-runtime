package appapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gateway-runtime/internal/audit"
	"gateway-runtime/internal/capability"
	"gateway-runtime/internal/httpx/middleware"
	"gateway-runtime/internal/observability"
	"gateway-runtime/internal/outbound"
	"gateway-runtime/internal/policy"
	"gateway-runtime/internal/session"
)

type fakeDomain struct {
	item ManagedItem
	err  error
}

func (f fakeDomain) GetManagedItem(context.Context, string, string) (ManagedItem, error) {
	return f.item, f.err
}

type fakeSigner struct {
	last capability.PresignedRequest
	err  error
}

func (f *fakeSigner) PresignPut(_ context.Context, req capability.PresignedRequest) (capability.PresignedOperation, error) {
	f.last = req
	if f.err != nil {
		return capability.PresignedOperation{}, f.err
	}
	return capability.PresignedOperation{
		URL:       "https://storage.example/upload?signature=secret",
		Method:    http.MethodPut,
		ExpiresAt: time.Now().Add(req.ExpiresIn),
	}, nil
}

func (f *fakeSigner) PresignGet(context.Context, capability.PresignedRequest) (capability.PresignedOperation, error) {
	return capability.PresignedOperation{}, errors.New("not implemented")
}

func testHandler(t *testing.T, rolePerms map[string][]string) (Handler, *session.MemoryStore) {
	t.Helper()
	sessions := session.NewMemoryStore()
	if err := sessions.Put(context.Background(), session.Session{
		ID: "sid", Subject: "user-1", Roles: []string{"user"},
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	return Handler{
		Sessions:       sessions,
		Policy:         policy.Static{RolePermissions: rolePerms},
		Audit:          &audit.MemorySink{},
		Metrics:        &observability.Metrics{},
		UploadBucket:   "uploads",
		UploadPrefixes: []string{"users/user-1"},
		MaxPresignTTL:  10 * time.Minute,
	}, sessions
}

func TestManagedItemRouteRequiresPermission(t *testing.T) {
	h, _ := testHandler(t, map[string][]string{"user": {"managed-item.read"}})
	h.Domain = fakeDomain{item: ManagedItem{ID: "a-1", Name: "Asset"}}

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/managed-items/a-1", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-bff_session", Value: "sid"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got ManagedItem
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "a-1" {
		t.Fatalf("item = %+v", got)
	}
	events := h.Audit.(*audit.MemorySink).Snapshot()
	if len(events) != 2 {
		t.Fatalf("audit events = %d, want 2: %+v", len(events), events)
	}
	e := events[1]
	if e.Actor != "user-1" || e.Action != "managed-item.read" || e.ResourceID != "a-1" || e.Outcome != "success" || e.HTTPStatus != http.StatusOK {
		t.Fatalf("unexpected audit result: %+v", e)
	}
}

func TestManagedItemRouteDenied(t *testing.T) {
	h, _ := testHandler(t, map[string][]string{"user": {"managed-item.list"}})
	h.Domain = fakeDomain{item: ManagedItem{ID: "a-1"}}

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/managed-items/a-1", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-bff_session", Value: "sid"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestManagedItemMapsUpstreamNotFound(t *testing.T) {
	h, _ := testHandler(t, map[string][]string{"user": {"managed-item.read"}})
	h.Domain = fakeDomain{err: &outbound.Error{Kind: outbound.ErrNotFound, StatusCode: 404}}

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/managed-items/missing", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-bff_session", Value: "sid"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestPresignUploadValidatesScopeAndTTL(t *testing.T) {
	h, _ := testHandler(t, map[string][]string{"user": {"storage.upload"}})
	signer := &fakeSigner{}
	h.Storage = signer

	mux := http.NewServeMux()
	h.Register(mux)

	body := `{"object_key":"users/user-1/report.pdf","content_type":"application/pdf","expires_in_seconds":300}`
	req := httptest.NewRequest(http.MethodPost, "/api/storage/upload-url", strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "__Host-bff_session", Value: "sid"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if signer.last.Bucket != "uploads" || signer.last.ObjectKey != "users/user-1/report.pdf" {
		t.Fatalf("presign request = %+v", signer.last)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("expected no-store")
	}
}

func TestPresignUploadAuditIdentifiesActorClientActionAndOutcome(t *testing.T) {
	h, sessions := testHandler(t, map[string][]string{"user": {"storage.upload"}})
	if err := sessions.Put(context.Background(), session.Session{
		ID: "sid", Subject: "user-1", Username: "alice", Roles: []string{"user"},
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	h.Storage = &fakeSigner{}

	mux := http.NewServeMux()
	h.Register(mux)
	proxy, err := middleware.ParseTrustedProxyCIDRs([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	handler := middleware.RequestMetadata(proxy, mux)

	body := `{"object_key":"users/user-1/report.pdf","content_type":"application/pdf"}`
	req := httptest.NewRequest(http.MethodPost, "/api/storage/upload-url", strings.NewReader(body))
	req.RemoteAddr = "10.1.2.3:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	req.Header.Set("CF-Ray", "8f1234567890abcd-BKK")
	req.AddCookie(&http.Cookie{Name: "__Host-bff_session", Value: "sid"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	events := h.Audit.(*audit.MemorySink).Snapshot()
	if len(events) != 2 {
		t.Fatalf("audit events = %d, want 2: %+v", len(events), events)
	}
	e := events[1]
	if e.Actor != "user-1" || e.ActorUsername != "alice" {
		t.Fatalf("actor = %q username = %q", e.Actor, e.ActorUsername)
	}
	if e.ClientIP != "203.0.113.10" || e.ClientIPSource != "x-forwarded-for" {
		t.Fatalf("client ip = %q source = %q", e.ClientIP, e.ClientIPSource)
	}
	if e.Action != "storage.presign_put" || e.Target != "object" || e.ResourceID != "users/user-1/report.pdf" {
		t.Fatalf("unexpected action target resource: %+v", e)
	}
	if e.Outcome != "success" || e.HTTPStatus != http.StatusOK || e.OccurredAt.IsZero() {
		t.Fatalf("unexpected outcome status timestamp: %+v", e)
	}
	if e.CFRay != "8f1234567890abcd-BKK" {
		t.Fatalf("cf ray = %q", e.CFRay)
	}
}

func TestPresignUploadRejectsEscapedPrefix(t *testing.T) {
	h, _ := testHandler(t, map[string][]string{"user": {"storage.upload"}})
	h.Storage = &fakeSigner{}

	mux := http.NewServeMux()
	h.Register(mux)

	body := `{"object_key":"users/user-1/../other/secret.txt"}`
	req := httptest.NewRequest(http.MethodPost, "/api/storage/upload-url", strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "__Host-bff_session", Value: "sid"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	events := h.Audit.(*audit.MemorySink).Snapshot()
	if len(events) != 2 || events[1].Action != "storage.presign_put" || events[1].Outcome != "deny" || events[1].HTTPStatus != http.StatusBadRequest {
		t.Fatalf("unexpected audit events: %+v", events)
	}
}

func TestPresignUploadFailureIsAudited(t *testing.T) {
	h, _ := testHandler(t, map[string][]string{"user": {"storage.upload"}})
	h.Storage = &fakeSigner{err: &outbound.Error{Kind: outbound.ErrUnavailable, StatusCode: http.StatusServiceUnavailable}}

	mux := http.NewServeMux()
	h.Register(mux)
	body := `{"object_key":"users/user-1/report.pdf"}`
	req := httptest.NewRequest(http.MethodPost, "/api/storage/upload-url", strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "__Host-bff_session", Value: "sid"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	events := h.Audit.(*audit.MemorySink).Snapshot()
	if len(events) != 2 {
		t.Fatalf("audit events = %d, want 2: %+v", len(events), events)
	}
	e := events[1]
	if e.Action != "storage.presign_put" || e.ResourceID != "users/user-1/report.pdf" || e.Outcome != "failure" || e.HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("unexpected audit result: %+v", e)
	}
}

func TestDisabledCapabilityDoesNotRegisterRoute(t *testing.T) {
	h, _ := testHandler(t, map[string][]string{"user": {"managed-item.read", "storage.upload"}})

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/managed-items/a-1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
