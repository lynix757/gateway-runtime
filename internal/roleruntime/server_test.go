package roleruntime

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gateway-runtime/internal/accesspolicy"
	"gateway-runtime/internal/capability"
	"gateway-runtime/internal/runtimecfg"
	"gateway-runtime/internal/storageprovider"
	"gateway-runtime/internal/storagetarget"
	"gateway-runtime/internal/workloadauth"
)

type fakeSigner struct {
	put int
	get int
}

func (f *fakeSigner) PresignPut(context.Context, capability.PresignedRequest) (capability.PresignedOperation, error) {
	f.put++
	return capability.PresignedOperation{URL: "https://object.example/upload", Method: "PUT", ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (f *fakeSigner) PresignGet(context.Context, capability.PresignedRequest) (capability.PresignedOperation, error) {
	f.get++
	return capability.PresignedOperation{URL: "https://object.example/download", Method: "GET", ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func storageConfig(caps ...string) Config {
	return Config{
		Selection: runtimecfg.Selection{
			Roles:        []runtimecfg.Role{runtimecfg.RoleStorage},
			Capabilities: map[runtimecfg.Role][]string{runtimecfg.RoleStorage: caps},
		},
		StorageDefaultTTL: 15 * time.Minute,
		StorageMaxTTL:     time.Hour,
	}
}

func TestStorageCapabilityGating(t *testing.T) {
	cfg := storageConfig("upload")
	mux := http.NewServeMux()
	registerSystem(mux, cfg)
	registerRole(mux, cfg, runtimecfg.RoleStorage, cfg.Selection.Capabilities[runtimecfg.RoleStorage])

	req := httptest.NewRequest(http.MethodPost, "/storage/upload", bytes.NewBufferString(`{"bucket":"a","object_key":"x"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("upload status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/storage/download", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled download status=%d", rec.Code)
	}
}

func TestStorageUploadUsesSigner(t *testing.T) {
	signer := &fakeSigner{}
	cfg := storageConfig("upload")
	cfg.StorageSigner = signer
	mux := http.NewServeMux()
	registerSystem(mux, cfg)
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"upload"})
	req := httptest.NewRequest(http.MethodPost, "/storage/upload", bytes.NewBufferString(`{"bucket":"assets","object_key":"P1/a.pdf","content_type":"application/pdf","expires_in_seconds":60}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if signer.put != 1 {
		t.Fatalf("put calls=%d", signer.put)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache=%q", got)
	}
}

func TestStorageRejectsUnsafeObjectKey(t *testing.T) {
	cfg := storageConfig("upload")
	cfg.StorageSigner = &fakeSigner{}
	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"upload"})
	req := httptest.NewRequest(http.MethodPost, "/storage/upload", bytes.NewBufferString(`{"bucket":"assets","object_key":"../secret"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestStorageReadinessRequiresSigner(t *testing.T) {
	cfg := storageConfig("upload")
	mux := http.NewServeMux()
	registerSystem(mux, cfg)
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
}

type fakeMultipartSigner struct {
	fakeSigner
	initiate int
	part     int
	complete int
	abort    int
}

func (f *fakeMultipartSigner) InitiateMultipart(context.Context, capability.MultipartInitiateRequest) (capability.MultipartUpload, error) {
	f.initiate++
	return capability.MultipartUpload{UploadID: "u1", Bucket: "assets", ObjectKey: "big.bin", ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (f *fakeMultipartSigner) PresignMultipartPart(context.Context, capability.MultipartPartRequest) (capability.PresignedOperation, error) {
	f.part++
	return capability.PresignedOperation{URL: "https://object.example/part", Method: "PUT", ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (f *fakeMultipartSigner) CompleteMultipart(context.Context, capability.MultipartCompleteRequest) (capability.MultipartCompleteResult, error) {
	f.complete++
	return capability.MultipartCompleteResult{ETag: "final-etag"}, nil
}
func (f *fakeMultipartSigner) AbortMultipart(context.Context, capability.MultipartAbortRequest) error {
	f.abort++
	return nil
}

func TestMultipartLifecycle(t *testing.T) {
	signer := &fakeMultipartSigner{}
	cfg := storageConfig("multipart")
	cfg.StorageSigner = signer
	mux := http.NewServeMux()
	registerSystem(mux, cfg)
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"multipart"})

	cases := []struct {
		path string
		body string
		want int
	}{
		{"/storage/multipart/initiate", `{"bucket":"assets","object_key":"big.bin","content_type":"application/octet-stream","expires_in_seconds":300}`, http.StatusOK},
		{"/storage/multipart/part", `{"bucket":"assets","object_key":"big.bin","upload_id":"u1","part_number":1,"expires_in_seconds":300}`, http.StatusOK},
		{"/storage/multipart/complete", `{"bucket":"assets","object_key":"big.bin","upload_id":"u1","parts":[{"part_number":1,"etag":"etag-1"}]}`, http.StatusOK},
		{"/storage/multipart/abort", `{"bucket":"assets","object_key":"big.bin","upload_id":"u1"}`, http.StatusNoContent},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("%s status=%d body=%s", tc.path, rec.Code, rec.Body.String())
		}
	}
	if signer.initiate != 1 || signer.part != 1 || signer.complete != 1 || signer.abort != 1 {
		t.Fatalf("calls initiate=%d part=%d complete=%d abort=%d", signer.initiate, signer.part, signer.complete, signer.abort)
	}
}

func TestMultipartRejectsInvalidPartNumber(t *testing.T) {
	signer := &fakeMultipartSigner{}
	cfg := storageConfig("multipart")
	cfg.StorageSigner = signer
	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"multipart"})
	req := httptest.NewRequest(http.MethodPost, "/storage/multipart/part", bytes.NewBufferString(`{"bucket":"assets","object_key":"big.bin","upload_id":"u1","part_number":10001}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestMultipartReadinessRequiresMultipartProvider(t *testing.T) {
	cfg := storageConfig("multipart")
	cfg.StorageSigner = &fakeSigner{}
	mux := http.NewServeMux()
	registerSystem(mux, cfg)
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

type fakeMetadataSigner struct{ fakeSigner }

func (f *fakeMetadataSigner) GetMetadata(context.Context, capability.StorageMetadataRequest) (capability.StorageMetadata, error) {
	return capability.StorageMetadata{
		Exists:      true,
		Bucket:      "assets",
		ObjectKey:   "P1/a.pdf",
		Size:        1234,
		ContentType: "application/pdf",
		ETag:        "etag-1",
		Checksums:   map[string]string{"sha256": "abc"},
	}, nil
}

func TestStorageMetadata(t *testing.T) {
	cfg := storageConfig("metadata")
	cfg.StorageSigner = &fakeMetadataSigner{}
	mux := http.NewServeMux()
	registerSystem(mux, cfg)
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"metadata"})
	req := httptest.NewRequest(http.MethodGet, "/storage/metadata?bucket=assets&object_key=P1/a.pdf", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache=%q", got)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"exists":true`)) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestStorageMetadataReadinessRequiresProvider(t *testing.T) {
	cfg := storageConfig("metadata")
	cfg.StorageSigner = &fakeSigner{}
	mux := http.NewServeMux()
	registerSystem(mux, cfg)
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

type fakeAccessPolicy struct {
	allow bool
	err   error
	calls []accesspolicy.Request
}

func (f *fakeAccessPolicy) Authorize(_ context.Context, req accesspolicy.Request) (accesspolicy.Decision, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return accesspolicy.Decision{}, f.err
	}
	if f.allow {
		return accesspolicy.Decision{Allow: true}, nil
	}
	return accesspolicy.Decision{Allow: false, Reason: "target_not_allowed"}, nil
}

func targetStorageConfig(caps ...string) Config {
	cfg := storageConfig(caps...)
	cfg.StorageTargets = storagetarget.Registry{
		"asset":   {Provider: "minio-main", Bucket: "assets"},
		"archive": {Provider: "minio-main", Bucket: "archive", ReadOnly: true},
	}
	cfg.StorageSubjectHeader = "X-Auth-Subject"
	return cfg
}

func TestStorageTargetAllowResolvesBucket(t *testing.T) {
	signer := &fakeSigner{}
	policy := &fakeAccessPolicy{allow: true}
	cfg := targetStorageConfig("upload")
	cfg.StorageSigner = signer
	cfg.StorageAccessPolicy = policy

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"upload"})
	req := httptest.NewRequest(http.MethodPost, "/storage/upload", bytes.NewBufferString(`{"target":"asset","object_key":"P1/a.pdf"}`))
	req.Header.Set("X-Auth-Subject", "user-001")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if signer.put != 1 {
		t.Fatalf("put calls=%d", signer.put)
	}
	if len(policy.calls) != 1 || policy.calls[0].Subject != "user-001" || policy.calls[0].Action != "storage.upload" || policy.calls[0].Target != "asset" {
		t.Fatalf("policy calls=%+v", policy.calls)
	}
}

func TestStorageTargetDenyStopsSigner(t *testing.T) {
	signer := &fakeSigner{}
	policy := &fakeAccessPolicy{allow: false}
	cfg := targetStorageConfig("download")
	cfg.StorageSigner = signer
	cfg.StorageAccessPolicy = policy

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"download"})
	req := httptest.NewRequest(http.MethodGet, "/storage/download?target=asset&object_key=P1/a.pdf", nil)
	req.Header.Set("X-Auth-Subject", "user-002")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if signer.get != 0 {
		t.Fatalf("signer called on deny")
	}
}

func TestStorageReadOnlyTargetBlocksWriteBeforePolicy(t *testing.T) {
	signer := &fakeSigner{}
	policy := &fakeAccessPolicy{allow: true}
	cfg := targetStorageConfig("upload")
	cfg.StorageSigner = signer
	cfg.StorageAccessPolicy = policy

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"upload"})
	req := httptest.NewRequest(http.MethodPost, "/storage/upload", bytes.NewBufferString(`{"target":"archive","object_key":"2026/a.pdf"}`))
	req.Header.Set("X-Auth-Subject", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if signer.put != 0 || len(policy.calls) != 0 {
		t.Fatalf("write should stop before provider/policy: put=%d policy=%d", signer.put, len(policy.calls))
	}
}

func TestStorageTargetRequiresSubject(t *testing.T) {
	cfg := targetStorageConfig("metadata")
	cfg.StorageSigner = &fakeMetadataSigner{}
	cfg.StorageAccessPolicy = &fakeAccessPolicy{allow: true}

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"metadata"})
	req := httptest.NewRequest(http.MethodGet, "/storage/metadata?target=asset&object_key=P1/a.pdf", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStorageTargetUnknownRejected(t *testing.T) {
	cfg := targetStorageConfig("download")
	cfg.StorageSigner = &fakeSigner{}
	cfg.StorageAccessPolicy = &fakeAccessPolicy{allow: true}

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"download"})
	req := httptest.NewRequest(http.MethodGet, "/storage/download?target=missing&object_key=a.pdf", nil)
	req.Header.Set("X-Auth-Subject", "user-001")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMultipartTargetAuthorizationAppliedOncePerOperation(t *testing.T) {
	signer := &fakeMultipartSigner{}
	policy := &fakeAccessPolicy{allow: true}
	cfg := targetStorageConfig("multipart")
	cfg.StorageSigner = signer
	cfg.StorageAccessPolicy = policy

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"multipart"})

	cases := []struct {
		path string
		body string
		want int
	}{
		{"/storage/multipart/initiate", `{"target":"asset","object_key":"big.bin"}`, http.StatusOK},
		{"/storage/multipart/part", `{"target":"asset","object_key":"big.bin","upload_id":"u1","part_number":1}`, http.StatusOK},
		{"/storage/multipart/complete", `{"target":"asset","object_key":"big.bin","upload_id":"u1","parts":[{"part_number":1,"etag":"etag-1"}]}`, http.StatusOK},
		{"/storage/multipart/abort", `{"target":"asset","object_key":"big.bin","upload_id":"u1"}`, http.StatusNoContent},
	}

	for i, tc := range cases {
		before := len(policy.calls)
		req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set("X-Auth-Subject", "user-001")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("%s status=%d body=%s", tc.path, rec.Code, rec.Body.String())
		}
		if got := len(policy.calls) - before; got != 1 {
			t.Fatalf("%s policy calls=%d want=1 (case=%d)", tc.path, got, i)
		}
		last := policy.calls[len(policy.calls)-1]
		if last.Action != "storage.multipart" || last.Target != "asset" || last.Subject != "user-001" {
			t.Fatalf("%s policy call=%+v", tc.path, last)
		}
	}
}

func TestMultipartTargetDenyStopsCompleteAndAbort(t *testing.T) {
	for _, path := range []string{"/storage/multipart/complete", "/storage/multipart/abort"} {
		t.Run(path, func(t *testing.T) {
			signer := &fakeMultipartSigner{}
			policy := &fakeAccessPolicy{allow: false}
			cfg := targetStorageConfig("multipart")
			cfg.StorageSigner = signer
			cfg.StorageAccessPolicy = policy

			mux := http.NewServeMux()
			registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"multipart"})

			body := `{"target":"asset","object_key":"big.bin","upload_id":"u1"}`
			if path == "/storage/multipart/complete" {
				body = `{"target":"asset","object_key":"big.bin","upload_id":"u1","parts":[{"part_number":1,"etag":"etag-1"}]}`
			}
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
			req.Header.Set("X-Auth-Subject", "user-001")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if signer.complete != 0 || signer.abort != 0 {
				t.Fatalf("provider called on deny: complete=%d abort=%d", signer.complete, signer.abort)
			}
		})
	}
}

func TestStorageTargetRoutesToConfiguredProvider(t *testing.T) {
	mainSigner := &fakeSigner{}
	archiveSigner := &fakeSigner{}
	policy := &fakeAccessPolicy{allow: true}
	cfg := targetStorageConfig("download")
	cfg.StorageSigner = nil
	cfg.StorageAccessPolicy = policy
	cfg.StorageTargets = storagetarget.Registry{
		"asset":   {Provider: "minio-main", Bucket: "assets"},
		"archive": {Provider: "minio-archive", Bucket: "archive"},
	}
	cfg.StorageProviders = storageprovider.Registry{
		"minio-main":    {Signer: mainSigner},
		"minio-archive": {Signer: archiveSigner},
	}

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"download"})

	for _, tc := range []struct {
		target string
	}{
		{target: "asset"},
		{target: "archive"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/storage/download?target="+tc.target+"&object_key=a.pdf", nil)
		req.Header.Set("X-Auth-Subject", "user-001")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("target=%s status=%d body=%s", tc.target, rec.Code, rec.Body.String())
		}
	}

	if mainSigner.get != 1 {
		t.Fatalf("main signer get=%d want=1", mainSigner.get)
	}
	if archiveSigner.get != 1 {
		t.Fatalf("archive signer get=%d want=1", archiveSigner.get)
	}
}

func TestStorageTargetMissingProviderFailsClosed(t *testing.T) {
	cfg := targetStorageConfig("download")
	cfg.StorageSigner = nil
	cfg.StorageAccessPolicy = &fakeAccessPolicy{allow: true}
	cfg.StorageTargets = storagetarget.Registry{
		"asset": {Provider: "missing", Bucket: "assets"},
	}
	cfg.StorageProviders = storageprovider.Registry{
		"minio-main": {Signer: &fakeSigner{}},
	}

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"download"})
	req := httptest.NewRequest(http.MethodGet, "/storage/download?target=asset&object_key=a.pdf", nil)
	req.Header.Set("X-Auth-Subject", "user-001")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBackendStorageIntentUpload(t *testing.T) {
	signer := &fakeSigner{}
	policy := &fakeAccessPolicy{allow: true}
	cfg := targetStorageConfig("upload")
	cfg.StorageSigner = signer
	cfg.StorageAccessPolicy = policy

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"upload"})

	req := httptest.NewRequest(http.MethodPost, "/storage/intents/upload", bytes.NewBufferString("{\"target\":\"asset\",\"object_key\":\"tenant-01/project-22/assets/A-100/file.pdf\",\"content_type\":\"application/pdf\",\"expires_in_seconds\":300}"))
	req.Header.Set("X-Auth-Subject", "user-001")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if signer.put != 1 {
		t.Fatalf("put calls=%d", signer.put)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("\"method\":\"PUT\"")) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestBackendStorageIntentDownload(t *testing.T) {
	signer := &fakeSigner{}
	policy := &fakeAccessPolicy{allow: true}
	cfg := targetStorageConfig("download")
	cfg.StorageSigner = signer
	cfg.StorageAccessPolicy = policy

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"download"})

	req := httptest.NewRequest(http.MethodPost, "/storage/intents/download", bytes.NewBufferString("{\"target\":\"asset\",\"object_key\":\"tenant-01/project-22/assets/A-100/file.pdf\",\"expires_in_seconds\":300}"))
	req.Header.Set("X-Auth-Subject", "user-001")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if signer.get != 1 {
		t.Fatalf("get calls=%d", signer.get)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("\"method\":\"GET\"")) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestBackendStorageReferenceMetadata(t *testing.T) {
	policy := &fakeAccessPolicy{allow: true}
	cfg := targetStorageConfig("metadata")
	cfg.StorageSigner = &fakeMetadataSigner{}
	cfg.StorageAccessPolicy = policy

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"metadata"})

	req := httptest.NewRequest(http.MethodGet, "/storage/references/metadata?target=asset&object_key=P1/a.pdf", nil)
	req.Header.Set("X-Auth-Subject", "user-001")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("\"target\":\"asset\"")) {
		t.Fatalf("body=%s", rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("\"object_key\":\"P1/a.pdf\"")) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestBackendUploadVerificationSuccess(t *testing.T) {
	policy := &fakeAccessPolicy{allow: true}
	cfg := targetStorageConfig("metadata")
	cfg.StorageSigner = &fakeMetadataSigner{}
	cfg.StorageAccessPolicy = policy

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"metadata"})

	body := `{"target":"asset","object_key":"P1/a.pdf","expected_size":1234,"expected_content_type":"application/pdf","expected_checksums":{"sha256":"abc"}}`
	req := httptest.NewRequest(http.MethodPost, "/storage/intents/verify", bytes.NewBufferString(body))
	req.Header.Set("X-Auth-Subject", "user-001")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"verified":true`)) {
		t.Fatalf("body=%s", rec.Body.String())
	}
	if len(policy.calls) != 1 || policy.calls[0].Action != "storage.metadata" {
		t.Fatalf("policy calls=%+v", policy.calls)
	}
}

func TestBackendUploadVerificationMismatchIsBusinessResult(t *testing.T) {
	policy := &fakeAccessPolicy{allow: true}
	cfg := targetStorageConfig("metadata")
	cfg.StorageSigner = &fakeMetadataSigner{}
	cfg.StorageAccessPolicy = policy

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"metadata"})

	body := `{"target":"asset","object_key":"P1/a.pdf","expected_size":999,"expected_content_type":"image/png","expected_checksums":{"sha256":"wrong"}}`
	req := httptest.NewRequest(http.MethodPost, "/storage/intents/verify", bytes.NewBufferString(body))
	req.Header.Set("X-Auth-Subject", "user-001")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"verified":false`)) {
		t.Fatalf("body=%s", rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"field":"size"`)) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestStoragePolicyReceivesActorAndServiceContext(t *testing.T) {
	signer := &fakeSigner{}
	policy := &fakeAccessPolicy{allow: true}
	cfg := targetStorageConfig("upload")
	cfg.StorageSigner = signer
	cfg.StorageAccessPolicy = policy

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"upload"})

	req := httptest.NewRequest(http.MethodPost, "/storage/intents/upload", bytes.NewBufferString(
		`{"target":"asset","object_key":"P1/a.pdf"}`,
	))
	req.Header.Set("X-Actor-Subject", "user-001")
	req.Header.Set("X-Service-Identity", "asset-api")
	req.Header.Set("X-Tenant-ID", "tenant-01")
	req.Header.Set("X-Project-ID", "project-22")
	req.Header.Set("X-Request-ID", "req-123")
	req.Header.Set("X-Trace-ID", "trace-456")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(policy.calls) != 1 {
		t.Fatalf("policy calls=%+v", policy.calls)
	}
	call := policy.calls[0]
	if call.Subject != "user-001" {
		t.Fatalf("subject=%q", call.Subject)
	}
	if call.Context["service_id"] != "asset-api" ||
		call.Context["tenant_id"] != "tenant-01" ||
		call.Context["project_id"] != "project-22" ||
		call.Context["request_id"] != "req-123" ||
		call.Context["trace_id"] != "trace-456" {
		t.Fatalf("context=%+v", call.Context)
	}
}

type fakeWorkloadVerifier struct {
	principal workloadauth.Principal
	err       error
	token     string
}

func (f *fakeWorkloadVerifier) Verify(_ context.Context, rawToken string) (workloadauth.Principal, error) {
	f.token = rawToken
	if f.err != nil {
		return workloadauth.Principal{}, f.err
	}
	return f.principal, nil
}

func TestWorkloadTokenOverridesServiceHeader(t *testing.T) {
	signer := &fakeSigner{}
	policy := &fakeAccessPolicy{allow: true}
	verifier := &fakeWorkloadVerifier{principal: workloadauth.Principal{Service: "asset-api", Subject: "svc-123"}}
	cfg := targetStorageConfig("upload")
	cfg.StorageSigner = signer
	cfg.StorageAccessPolicy = policy
	cfg.WorkloadVerifier = verifier
	cfg.WorkloadAuthRequired = true

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"upload"})

	req := httptest.NewRequest(http.MethodPost, "/storage/intents/upload", bytes.NewBufferString(
		`{"target":"asset","object_key":"P1/a.pdf"}`,
	))
	req.Header.Set("Authorization", "Bearer signed-token")
	req.Header.Set("X-Actor-Subject", "user-001")
	req.Header.Set("X-Service-Identity", "spoofed-service")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if verifier.token != "signed-token" {
		t.Fatalf("token=%q", verifier.token)
	}
	if len(policy.calls) != 1 || policy.calls[0].Context["service_id"] != "asset-api" {
		t.Fatalf("policy calls=%+v", policy.calls)
	}
}

func TestWorkloadTokenRequired(t *testing.T) {
	cfg := targetStorageConfig("upload")
	cfg.StorageSigner = &fakeSigner{}
	cfg.StorageAccessPolicy = &fakeAccessPolicy{allow: true}
	cfg.WorkloadVerifier = &fakeWorkloadVerifier{principal: workloadauth.Principal{Service: "asset-api"}}
	cfg.WorkloadAuthRequired = true

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"upload"})

	req := httptest.NewRequest(http.MethodPost, "/storage/intents/upload", bytes.NewBufferString(
		`{"target":"asset","object_key":"P1/a.pdf"}`,
	))
	req.Header.Set("X-Actor-Subject", "user-001")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestInvalidWorkloadTokenRejected(t *testing.T) {
	cfg := targetStorageConfig("upload")
	cfg.StorageSigner = &fakeSigner{}
	cfg.StorageAccessPolicy = &fakeAccessPolicy{allow: true}
	cfg.WorkloadVerifier = &fakeWorkloadVerifier{err: errors.New("invalid signature")}
	cfg.WorkloadAuthRequired = true

	mux := http.NewServeMux()
	registerRole(mux, cfg, runtimecfg.RoleStorage, []string{"upload"})

	req := httptest.NewRequest(http.MethodPost, "/storage/intents/upload", bytes.NewBufferString(
		`{"target":"asset","object_key":"P1/a.pdf"}`,
	))
	req.Header.Set("Authorization", "Bearer bad-token")
	req.Header.Set("X-Actor-Subject", "user-001")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
