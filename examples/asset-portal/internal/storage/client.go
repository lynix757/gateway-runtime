package storage

import (
	"context"
	"net/http"
)

type JSONDoer interface {
	DoJSON(ctx context.Context, sessionID, method, path string, in, out any) error
}

type UploadRequest struct {
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type"`
	TTLSeconds  int    `json:"ttl_seconds"`
}

type UploadResponse struct {
	Method    string `json:"method"`
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

type Client struct {
	HTTP JSONDoer
}

func (c Client) PresignUpload(ctx context.Context, sessionID string, req UploadRequest) (UploadResponse, error) {
	var out UploadResponse
	err := c.HTTP.DoJSON(ctx, sessionID, http.MethodPost, "/api/presign/upload", req, &out)
	return out, err
}
