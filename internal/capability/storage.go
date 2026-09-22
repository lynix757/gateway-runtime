package capability

import (
	"context"
	"time"
)

type PresignedRequest struct {
	Bucket      string
	ObjectKey   string
	ContentType string
	ExpiresIn   time.Duration
}

type PresignedOperation struct {
	URL       string
	Method    string
	ExpiresAt time.Time
	Headers   map[string]string
}

type StorageSigner interface {
	PresignPut(ctx context.Context, req PresignedRequest) (PresignedOperation, error)
	PresignGet(ctx context.Context, req PresignedRequest) (PresignedOperation, error)
}

type MultipartInitiateRequest struct {
	Bucket      string
	ObjectKey   string
	ContentType string
	ExpiresIn   time.Duration
}

type MultipartUpload struct {
	UploadID  string    `json:"upload_id"`
	Bucket    string    `json:"bucket"`
	ObjectKey string    `json:"object_key"`
	ExpiresAt time.Time `json:"expires_at"`
}

type MultipartPartRequest struct {
	Bucket     string
	ObjectKey  string
	UploadID   string
	PartNumber int
	ExpiresIn  time.Duration
}

type CompletedPart struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
}

type MultipartCompleteRequest struct {
	Bucket    string
	ObjectKey string
	UploadID  string
	Parts     []CompletedPart
}

type MultipartCompleteResult struct {
	ETag      string `json:"etag"`
	VersionID string `json:"version_id,omitempty"`
	Location  string `json:"location,omitempty"`
}

type MultipartAbortRequest struct {
	Bucket    string
	ObjectKey string
	UploadID  string
}

type MultipartStorageSigner interface {
	StorageSigner
	InitiateMultipart(ctx context.Context, req MultipartInitiateRequest) (MultipartUpload, error)
	PresignMultipartPart(ctx context.Context, req MultipartPartRequest) (PresignedOperation, error)
	CompleteMultipart(ctx context.Context, req MultipartCompleteRequest) (MultipartCompleteResult, error)
	AbortMultipart(ctx context.Context, req MultipartAbortRequest) error
}

type StorageMetadataRequest struct {
	Bucket    string
	ObjectKey string
}

type StorageMetadata struct {
	Exists       bool              `json:"exists"`
	Bucket       string            `json:"bucket"`
	ObjectKey    string            `json:"object_key"`
	Size         int64             `json:"size,omitempty"`
	ContentType  string            `json:"content_type,omitempty"`
	ETag         string            `json:"etag,omitempty"`
	VersionID    string            `json:"version_id,omitempty"`
	LastModified time.Time         `json:"last_modified,omitempty"`
	Checksums    map[string]string `json:"checksums,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type StorageMetadataProvider interface {
	GetMetadata(ctx context.Context, req StorageMetadataRequest) (StorageMetadata, error)
}
