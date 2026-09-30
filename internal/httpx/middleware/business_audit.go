package middleware

import (
	"net/http"
	"strings"

	"gateway-runtime/internal/audit"
)

func BusinessAudit(action, targetType string, sink audit.Sink, next http.Handler) http.Handler {
	if sink == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}

		outcome := "success"
		switch {
		case status >= http.StatusInternalServerError:
			outcome = "failure"
		case status >= http.StatusBadRequest:
			outcome = "deny"
		}

		e := audit.NewEvent(action, targetType, outcome)
		if s, ok := AuthenticatedSession(r.Context()); ok {
			e.Actor = s.Subject
			e.ActorUsername = s.Username
		}
		e.ResourceID = strings.TrimSpace(r.PathValue("id"))
		if e.ResourceID == "" {
			e.ResourceID = r.URL.Path
		}
		e.HTTPStatus = status
		e.Attributes["reason"] = http.StatusText(status)
		e.Attributes["request_method"] = r.Method
		e.Attributes["request_path"] = r.URL.Path
		EnrichAuditEvent(r, &e)
		_ = sink.Append(r.Context(), e)
	})
}
