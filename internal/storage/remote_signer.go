package storage

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"gateway-runtime/internal/capability"
	"gateway-runtime/internal/outbound"
)

type RemoteSigner struct {
	HTTP      *outbound.Client
	SessionID func(context.Context) string
}

type presignPayload struct {
	Bucket      string `json:"bucket"`
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type,omitempty"`
	ExpiresIn   int64  `json:"expires_in_seconds"`
}

type presignResponse struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	ExpiresAt time.Time         `json:"expires_at"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type multipartInitiatePayload struct {
	Bucket      string `json:"bucket"`
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type,omitempty"`
	ExpiresIn   int64  `json:"expires_in_seconds"`
}

type multipartInitiateResponse struct {
	UploadID  string    `json:"upload_id"`
	Bucket    string    `json:"bucket"`
	ObjectKey string    `json:"object_key"`
	ExpiresAt time.Time `json:"expires_at"`
}

type multipartPartPayload struct {
	Bucket     string `json:"bucket"`
	ObjectKey  string `json:"object_key"`
	UploadID   string `json:"upload_id"`
	PartNumber int    `json:"part_number"`
	ExpiresIn  int64  `json:"expires_in_seconds"`
}

type multipartCompletePayload struct {
	Bucket    string                     `json:"bucket"`
	ObjectKey string                     `json:"object_key"`
	UploadID  string                     `json:"upload_id"`
	Parts     []capability.CompletedPart `json:"parts"`
}

type multipartCompleteResponse struct {
	ETag      string `json:"etag"`
	VersionID string `json:"version_id,omitempty"`
	Location  string `json:"location,omitempty"`
}

type multipartAbortPayload struct {
	Bucket    string `json:"bucket"`
	ObjectKey string `json:"object_key"`
	UploadID  string `json:"upload_id"`
}

func (s RemoteSigner) PresignPut(ctx context.Context, req capability.PresignedRequest) (capability.PresignedOperation, error) {
	return s.presign(ctx, "/api/storage/presign/put", req)
}

func (s RemoteSigner) PresignGet(ctx context.Context, req capability.PresignedRequest) (capability.PresignedOperation, error) {
	return s.presign(ctx, "/api/storage/presign/get", req)
}

func (s RemoteSigner) presign(ctx context.Context, route string, req capability.PresignedRequest) (capability.PresignedOperation, error) {
	if s.HTTP == nil {
		return capability.PresignedOperation{}, fmt.Errorf("remote signer client is not configured")
	}
	payload := presignPayload{
		Bucket: req.Bucket, ObjectKey: req.ObjectKey, ContentType: req.ContentType,
		ExpiresIn: int64(req.ExpiresIn / time.Second),
	}
	var out presignResponse
	if err := s.HTTP.DoJSON(ctx, s.sessionID(ctx), http.MethodPost, route, payload, &out); err != nil {
		return capability.PresignedOperation{}, err
	}
	return capability.PresignedOperation{
		URL: out.URL, Method: out.Method, ExpiresAt: out.ExpiresAt, Headers: out.Headers,
	}, nil
}

func (s RemoteSigner) InitiateMultipart(ctx context.Context, req capability.MultipartInitiateRequest) (capability.MultipartUpload, error) {
	if s.HTTP == nil {
		return capability.MultipartUpload{}, fmt.Errorf("remote signer client is not configured")
	}
	payload := multipartInitiatePayload{
		Bucket: req.Bucket, ObjectKey: req.ObjectKey, ContentType: req.ContentType,
		ExpiresIn: int64(req.ExpiresIn / time.Second),
	}
	var out multipartInitiateResponse
	if err := s.HTTP.DoJSON(ctx, s.sessionID(ctx), http.MethodPost, "/api/storage/multipart/initiate", payload, &out); err != nil {
		return capability.MultipartUpload{}, err
	}
	return capability.MultipartUpload{
		UploadID: out.UploadID, Bucket: out.Bucket, ObjectKey: out.ObjectKey, ExpiresAt: out.ExpiresAt,
	}, nil
}

func (s RemoteSigner) PresignMultipartPart(ctx context.Context, req capability.MultipartPartRequest) (capability.PresignedOperation, error) {
	if s.HTTP == nil {
		return capability.PresignedOperation{}, fmt.Errorf("remote signer client is not configured")
	}
	payload := multipartPartPayload{
		Bucket: req.Bucket, ObjectKey: req.ObjectKey, UploadID: req.UploadID,
		PartNumber: req.PartNumber, ExpiresIn: int64(req.ExpiresIn / time.Second),
	}
	var out presignResponse
	if err := s.HTTP.DoJSON(ctx, s.sessionID(ctx), http.MethodPost, "/api/storage/multipart/part", payload, &out); err != nil {
		return capability.PresignedOperation{}, err
	}
	return capability.PresignedOperation{
		URL: out.URL, Method: out.Method, ExpiresAt: out.ExpiresAt, Headers: out.Headers,
	}, nil
}

func (s RemoteSigner) CompleteMultipart(ctx context.Context, req capability.MultipartCompleteRequest) (capability.MultipartCompleteResult, error) {
	if s.HTTP == nil {
		return capability.MultipartCompleteResult{}, fmt.Errorf("remote signer client is not configured")
	}
	payload := multipartCompletePayload{
		Bucket: req.Bucket, ObjectKey: req.ObjectKey, UploadID: req.UploadID, Parts: req.Parts,
	}
	var out multipartCompleteResponse
	if err := s.HTTP.DoJSON(ctx, s.sessionID(ctx), http.MethodPost, "/api/storage/multipart/complete", payload, &out); err != nil {
		return capability.MultipartCompleteResult{}, err
	}
	return capability.MultipartCompleteResult{
		ETag: out.ETag, VersionID: out.VersionID, Location: out.Location,
	}, nil
}

func (s RemoteSigner) AbortMultipart(ctx context.Context, req capability.MultipartAbortRequest) error {
	if s.HTTP == nil {
		return fmt.Errorf("remote signer client is not configured")
	}
	payload := multipartAbortPayload{Bucket: req.Bucket, ObjectKey: req.ObjectKey, UploadID: req.UploadID}
	return s.HTTP.DoJSON(ctx, s.sessionID(ctx), http.MethodPost, "/api/storage/multipart/abort", payload, nil)
}

func (s RemoteSigner) sessionID(ctx context.Context) string {
	if s.SessionID == nil {
		return ""
	}
	return s.SessionID(ctx)
}

type metadataPayload struct {
	Bucket    string `json:"bucket"`
	ObjectKey string `json:"object_key"`
}

func (s RemoteSigner) GetMetadata(ctx context.Context, req capability.StorageMetadataRequest) (capability.StorageMetadata, error) {
	if s.HTTP == nil {
		return capability.StorageMetadata{}, fmt.Errorf("remote signer client is not configured")
	}
	payload := metadataPayload{Bucket: req.Bucket, ObjectKey: req.ObjectKey}
	var out capability.StorageMetadata
	if err := s.HTTP.DoJSON(ctx, s.sessionID(ctx), http.MethodPost, "/api/storage/metadata", payload, &out); err != nil {
		return capability.StorageMetadata{}, err
	}
	return out, nil
}
