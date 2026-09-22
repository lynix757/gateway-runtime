package storagecontract

import (
	"testing"
	"time"

	"gateway-runtime/internal/capability"
)

func TestUploadIntentValidate(t *testing.T) {
	ok := UploadIntent{
		Target:      "asset",
		ObjectKey:   "tenant-01/project-22/assets/A-100/file.pdf",
		ContentType: "application/pdf",
		ExpiresIn:   15 * time.Minute,
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid intent rejected: %v", err)
	}

	bad := UploadIntent{Target: "asset", ObjectKey: "../secret"}
	if err := bad.Validate(); err == nil {
		t.Fatal("unsafe object key accepted")
	}
}

func TestStorageReferenceValidate(t *testing.T) {
	ref := StorageReference{
		Target:      "asset",
		ObjectKey:   "tenant-01/project-22/assets/A-100/file.pdf",
		VersionID:   "v1",
		ETag:        "etag-1",
		Size:        1234,
		ContentType: "application/pdf",
	}
	if err := ref.Validate(); err != nil {
		t.Fatalf("valid reference rejected: %v", err)
	}
}

func TestGrantConversion(t *testing.T) {
	exp := time.Now().Add(time.Minute).UTC().Truncate(time.Second)
	op := capability.PresignedOperation{
		URL:       "https://storage.example/object",
		Method:    "PUT",
		ExpiresAt: exp,
		Headers:   map[string]string{"Content-Type": "application/pdf"},
	}
	grant := UploadGrantFromOperation(op)
	if grant.URL != op.URL || grant.Method != "PUT" || !grant.ExpiresAt.Equal(exp) {
		t.Fatalf("grant=%+v", grant)
	}
}

func TestReferenceFromMetadata(t *testing.T) {
	meta := capability.StorageMetadata{
		ObjectKey:   "tenant-01/a.pdf",
		Size:        99,
		ContentType: "application/pdf",
		ETag:        "etag",
		VersionID:   "v1",
		Checksums:   map[string]string{"sha256": "abc"},
	}
	ref := ReferenceFromMetadata("asset", meta)
	if ref.Target != "asset" || ref.ObjectKey != meta.ObjectKey || ref.ETag != "etag" || ref.VersionID != "v1" {
		t.Fatalf("ref=%+v", ref)
	}
}

func TestVerifyUploadSuccess(t *testing.T) {
	size := int64(1234)
	intent := UploadVerificationIntent{
		Target:              "asset",
		ObjectKey:           "tenant-01/a.pdf",
		ExpectedSize:        &size,
		ExpectedContentType: "application/pdf",
		ExpectedChecksums:   map[string]string{"sha256": "abc"},
	}
	meta := capability.StorageMetadata{
		Exists:      true,
		ObjectKey:   "tenant-01/a.pdf",
		Size:        1234,
		ContentType: "application/pdf",
		ETag:        "etag-1",
		Checksums:   map[string]string{"sha256": "abc"},
	}
	result, err := VerifyUpload(intent, meta)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified || len(result.Mismatches) != 0 {
		t.Fatalf("result=%+v", result)
	}
}

func TestVerifyUploadMismatch(t *testing.T) {
	size := int64(100)
	intent := UploadVerificationIntent{
		Target:              "asset",
		ObjectKey:           "tenant-01/a.pdf",
		ExpectedSize:        &size,
		ExpectedContentType: "image/png",
		ExpectedChecksums:   map[string]string{"sha256": "expected"},
	}
	meta := capability.StorageMetadata{
		Exists:      true,
		ObjectKey:   "tenant-01/a.pdf",
		Size:        1234,
		ContentType: "application/pdf",
		Checksums:   map[string]string{"sha256": "actual"},
	}
	result, err := VerifyUpload(intent, meta)
	if err != nil {
		t.Fatal(err)
	}
	if result.Verified {
		t.Fatal("mismatched upload verified")
	}
	if len(result.Mismatches) != 3 {
		t.Fatalf("mismatches=%+v", result.Mismatches)
	}
}

func TestVerifyUploadNotFound(t *testing.T) {
	intent := UploadVerificationIntent{Target: "asset", ObjectKey: "tenant-01/missing.pdf"}
	result, err := VerifyUpload(intent, capability.StorageMetadata{
		Exists: false, ObjectKey: "tenant-01/missing.pdf",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Verified || len(result.Mismatches) != 1 || result.Mismatches[0].Field != "object" {
		t.Fatalf("result=%+v", result)
	}
}
