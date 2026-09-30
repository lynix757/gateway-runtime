package middleware

import (
	"context"
	"net/http"
	"strings"

	"gateway-runtime/internal/audit"
)

type requestMetadataKey struct{}

type requestMetadata struct {
	clientIP       string
	clientIPSource string
	cfRay          string
}

func RequestMetadata(proxy TrustedProxyConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP, source := ClientIdentity(r, proxy)
		metadata := requestMetadata{clientIP: clientIP, clientIPSource: source}
		if source != "remote-address" {
			metadata.cfRay = strings.TrimSpace(r.Header.Get("CF-Ray"))
		}
		ctx := context.WithValue(r.Context(), requestMetadataKey{}, metadata)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func EnrichAuditEvent(r *http.Request, event *audit.Event) {
	metadata, _ := r.Context().Value(requestMetadataKey{}).(requestMetadata)
	event.ClientIP = metadata.clientIP
	event.ClientIPSource = metadata.clientIPSource
	event.CFRay = metadata.cfRay
	event.CorrelationID = RequestIDFromContext(r.Context())
	event.TraceID = TraceIDFromContext(r.Context())
}
