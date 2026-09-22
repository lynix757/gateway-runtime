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
