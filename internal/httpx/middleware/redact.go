package middleware

import (
	"net/http"
	"net/url"
	"strings"
)

var sensitiveHeaders = map[string]struct{}{
	"authorization":       {},
	"cookie":              {},
	"set-cookie":          {},
	"proxy-authorization": {},
}

var sensitiveQueryKeys = map[string]struct{}{
	"access_token":    {},
	"refresh_token":   {},
	"id_token":        {},
	"code":            {},
	"client_secret":   {},
	"token":           {},
	"signature":       {},
	"x-amz-signature": {},
}

func RedactedHeaders(h http.Header) http.Header {
	out := h.Clone()
	for key := range out {
		if _, ok := sensitiveHeaders[strings.ToLower(key)]; ok {
			out.Set(key, "[REDACTED]")
		}
	}
	return out
}

func RedactedURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	copyURL := *u
	q := copyURL.Query()
	for key := range q {
		if _, ok := sensitiveQueryKeys[strings.ToLower(key)]; ok {
			q.Set(key, "[REDACTED]")
		}
	}
	copyURL.RawQuery = q.Encode()
	return copyURL.String()
}
