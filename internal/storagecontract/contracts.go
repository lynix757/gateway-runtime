package storagecontract

import (
	"fmt"
	"strings"
	"time"

	"gateway-runtime/internal/capability"
)

type UploadIntent struct {
	Target      string
	ObjectKey   string
	ContentType string
	ExpiresIn   time.Duration
}

type DownloadIntent struct {
	Target    string
	ObjectKey string
	ExpiresIn time.Duration
}

type StorageReference struct {
	Target      string            `json:"target"`
	ObjectKey   string            `json:"object_key"`
	VersionID   string            `json:"version_id,omitempty"`
	ETag        string            `json:"etag,omitempty"`
	Size        int64             `json:"size,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	Checksums   map[string]string `json:"checksums,omitempty"`
}

type UploadVerificationIntent struct {
	Target              string
	ObjectKey           string
	ExpectedSize        *int64
	ExpectedContentType string
	ExpectedChecksums   map[string]string
}

type VerificationMismatch struct {
	Field    string `json:"field"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
}

type UploadVerificationResult struct {
	Verified   bool                   `json:"verified"`
	Reference  StorageReference       `json:"reference"`
	Mismatches []VerificationMismatch `json:"mismatches,omitempty"`
}

type UploadGrant struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	ExpiresAt time.Time         `json:"expires_at"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type DownloadGrant struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	ExpiresAt time.Time         `json:"expires_at"`
	Headers   map[string]string `json:"headers,omitempty"`
}

func (i UploadIntent) Validate() error {
	if strings.TrimSpace(i.Target) == "" {
		return fmt.Errorf("target is required")
	}
	if err := validateObjectKey(i.ObjectKey); err != nil {
		return err
	}
	if i.ExpiresIn < 0 {
		return fmt.Errorf("expires_in must not be negative")
	}
	return nil
}

func (i DownloadIntent) Validate() error {
	if strings.TrimSpace(i.Target) == "" {
		return fmt.Errorf("target is required")
	}
	if err := validateObjectKey(i.ObjectKey); err != nil {
		return err
	}
	if i.ExpiresIn < 0 {
		return fmt.Errorf("expires_in must not be negative")
	}
	return nil
}

func (r StorageReference) Validate() error {
	if strings.TrimSpace(r.Target) == "" {
		return fmt.Errorf("target is required")
	}
	if err := validateObjectKey(r.ObjectKey); err != nil {
		return err
	}
	if r.Size < 0 {
		return fmt.Errorf("size must not be negative")
	}
	return nil
}

func (i UploadVerificationIntent) Validate() error {
	if strings.TrimSpace(i.Target) == "" {
		return fmt.Errorf("target is required")
	}
	if err := validateObjectKey(i.ObjectKey); err != nil {
		return err
	}
	if i.ExpectedSize != nil && *i.ExpectedSize < 0 {
		return fmt.Errorf("expected_size must not be negative")
	}
	for algorithm, value := range i.ExpectedChecksums {
		if strings.TrimSpace(algorithm) == "" || strings.TrimSpace(value) == "" {
			return fmt.Errorf("expected checksum algorithm and value are required")
		}
	}
	return nil
}

func VerifyUpload(i UploadVerificationIntent, metadata capability.StorageMetadata) (UploadVerificationResult, error) {
	if err := i.Validate(); err != nil {
		return UploadVerificationResult{}, err
	}

	ref := ReferenceFromMetadata(i.Target, metadata)
	result := UploadVerificationResult{Reference: ref}

	if !metadata.Exists {
		result.Mismatches = append(result.Mismatches, VerificationMismatch{Field: "object", Expected: "exists", Actual: "not_found"})
		return result, nil
	}

	if i.ExpectedSize != nil && metadata.Size != *i.ExpectedSize {
		result.Mismatches = append(result.Mismatches, VerificationMismatch{
			Field: "size", Expected: fmt.Sprint(*i.ExpectedSize), Actual: fmt.Sprint(metadata.Size),
		})
	}

	if expected := strings.TrimSpace(i.ExpectedContentType); expected != "" {
		actual := strings.TrimSpace(metadata.ContentType)
		if !strings.EqualFold(expected, actual) {
			result.Mismatches = append(result.Mismatches, VerificationMismatch{
				Field: "content_type", Expected: expected, Actual: actual,
			})
		}
	}

	for algorithm, expected := range i.ExpectedChecksums {
		key := strings.ToLower(strings.TrimSpace(algorithm))
		actual := ""
		for actualAlgorithm, value := range metadata.Checksums {
			if strings.ToLower(strings.TrimSpace(actualAlgorithm)) == key {
				actual = strings.TrimSpace(value)
				break
			}
		}
		if strings.TrimSpace(expected) != actual {
			result.Mismatches = append(result.Mismatches, VerificationMismatch{
				Field: "checksum." + key, Expected: strings.TrimSpace(expected), Actual: actual,
			})
		}
	}

	result.Verified = len(result.Mismatches) == 0
	return result, nil
}

func UploadGrantFromOperation(op capability.PresignedOperation) UploadGrant {
	return UploadGrant{
		URL:       op.URL,
		Method:    op.Method,
		ExpiresAt: op.ExpiresAt,
		Headers:   op.Headers,
	}
}

func DownloadGrantFromOperation(op capability.PresignedOperation) DownloadGrant {
	return DownloadGrant{
		URL:       op.URL,
		Method:    op.Method,
		ExpiresAt: op.ExpiresAt,
		Headers:   op.Headers,
	}
}

func ReferenceFromMetadata(target string, m capability.StorageMetadata) StorageReference {
	return StorageReference{
		Target:      strings.TrimSpace(target),
		ObjectKey:   m.ObjectKey,
		VersionID:   m.VersionID,
		ETag:        m.ETag,
		Size:        m.Size,
		ContentType: m.ContentType,
		Checksums:   m.Checksums,
	}
}

func validateObjectKey(v string) error {
	key := strings.TrimSpace(v)
	if key == "" {
		return fmt.Errorf("object_key is required")
	}
	if strings.HasPrefix(key, "/") || strings.Contains(key, "..") || strings.Contains(key, "\\\\") {
		return fmt.Errorf("object_key is invalid")
	}
	return nil
}
