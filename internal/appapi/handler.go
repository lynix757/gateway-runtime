package appapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	"gateway-runtime/internal/audit"
	"gateway-runtime/internal/capability"
	mw "gateway-runtime/internal/httpx/middleware"
	"gateway-runtime/internal/observability"
	"gateway-runtime/internal/outbound"
	"gateway-runtime/internal/policy"
	"gateway-runtime/internal/session"
)

type ManagedItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type DomainAPI interface {
	GetManagedItem(ctx context.Context, sessionID, id string) (ManagedItem, error)
}

type Handler struct {
	Sessions session.Store
	Policy   policy.Evaluator
	Domain   DomainAPI
	Storage  capability.StorageSigner
	Audit    audit.Sink
	Metrics  *observability.Metrics

	UploadBucket   string
	UploadPrefixes []string
	MaxPresignTTL  time.Duration
}

func (h Handler) Register(mux *http.ServeMux) {
	if h.Domain != nil {
		mux.Handle("GET /api/managed-items/{id}", mw.RequirePermissionWithAudit(
			"managed-item.read", h.Sessions, h.Policy, h.Audit, h.Metrics,
			http.HandlerFunc(h.getManagedItem),
		))
	}
	if h.Storage != nil {
		mux.Handle("POST /api/storage/upload-url", mw.RequirePermissionWithAudit(
			"storage.upload", h.Sessions, h.Policy, h.Audit, h.Metrics,
			http.HandlerFunc(h.presignUpload),
		))
	}
}

func (h Handler) getManagedItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	sessionID, ok := sessionID(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	item, err := h.Domain.GetManagedItem(r.Context(), sessionID, id)
	if err != nil {
		writeOutboundError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type uploadRequest struct {
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type"`
	ExpiresIn   int64  `json:"expires_in_seconds,omitempty"`
}

func (h Handler) presignUpload(w http.ResponseWriter, r *http.Request) {
	var in uploadRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	key, ok := validateObjectKey(in.ObjectKey, h.UploadPrefixes)
	if !ok || strings.TrimSpace(h.UploadBucket) == "" {
		writeError(w, http.StatusBadRequest, "invalid_object_key")
		return
	}

	ttl := time.Duration(in.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	maxTTL := h.MaxPresignTTL
	if maxTTL <= 0 {
		maxTTL = 15 * time.Minute
	}
	if ttl > maxTTL {
		writeError(w, http.StatusBadRequest, "expiry_too_long")
		return
	}

	op, err := h.Storage.PresignPut(r.Context(), capability.PresignedRequest{
		Bucket: h.UploadBucket, ObjectKey: key,
		ContentType: strings.TrimSpace(in.ContentType), ExpiresIn: ttl,
	})
	if err != nil {
		writeOutboundError(w, err)
		return
	}

	h.emit(r, "storage.presign_put", "object", "success", map[string]string{
		"bucket":     h.UploadBucket,
		"object_key": key,
	})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, op)
}

func validateObjectKey(raw string, prefixes []string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "/") || strings.Contains(raw, "\\") {
		return "", false
	}
	cleaned := path.Clean(raw)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != raw {
		return "", false
	}
	if len(prefixes) == 0 {
		return "", false
	}
	for _, prefix := range prefixes {
		prefix = strings.Trim(strings.TrimSpace(prefix), "/")
		if prefix != "" && (cleaned == prefix || strings.HasPrefix(cleaned, prefix+"/")) {
			return cleaned, true
		}
	}
	return "", false
}

func sessionID(r *http.Request) (string, bool) {
	c, err := r.Cookie("__Host-bff_session")
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

func writeOutboundError(w http.ResponseWriter, err error) {
	var oe *outbound.Error
	if !errors.As(err, &oe) {
		writeError(w, http.StatusBadGateway, "upstream_error")
		return
	}
	switch oe.Kind {
	case outbound.ErrBadRequest:
		writeError(w, http.StatusBadRequest, string(oe.Kind))
	case outbound.ErrUnauthorized:
		writeError(w, http.StatusUnauthorized, string(oe.Kind))
	case outbound.ErrForbidden:
		writeError(w, http.StatusForbidden, string(oe.Kind))
	case outbound.ErrNotFound:
		writeError(w, http.StatusNotFound, string(oe.Kind))
	case outbound.ErrConflict:
		writeError(w, http.StatusConflict, string(oe.Kind))
	case outbound.ErrRateLimited:
		writeError(w, http.StatusTooManyRequests, string(oe.Kind))
	case outbound.ErrUnavailable:
		writeError(w, http.StatusServiceUnavailable, string(oe.Kind))
	default:
		writeError(w, http.StatusBadGateway, string(oe.Kind))
	}
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (h Handler) emit(r *http.Request, action, target, outcome string, attrs map[string]string) {
	if h.Audit == nil {
		return
	}
	e := audit.NewEvent(action, target, outcome)
	e.CorrelationID = mw.RequestIDFromContext(r.Context())
	e.TraceID = mw.TraceIDFromContext(r.Context())
	e.Attributes = attrs
	_ = h.Audit.Append(r.Context(), e)
}
