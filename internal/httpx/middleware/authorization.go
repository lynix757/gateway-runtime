package middleware

import (
	"errors"
	"net/http"

	"gateway-runtime/internal/audit"
	"gateway-runtime/internal/observability"
	"gateway-runtime/internal/policy"
	"gateway-runtime/internal/session"
)

func RequirePermission(permission string, sessions session.Store, evaluator policy.Evaluator, next http.Handler) http.Handler {
	return RequirePermissionWithAuditCookie(permission, "__Host-bff_session", sessions, evaluator, nil, nil, next)
}

func RequirePermissionWithAudit(permission string, sessions session.Store, evaluator policy.Evaluator, sink audit.Sink, metrics *observability.Metrics, next http.Handler) http.Handler {
	return RequirePermissionWithAuditCookie(permission, "__Host-bff_session", sessions, evaluator, sink, metrics, next)
}

func RequirePermissionWithAuditCookie(permission, cookieName string, sessions session.Store, evaluator policy.Evaluator, sink audit.Sink, metrics *observability.Metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil || cookie.Value == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		s, err := sessions.Get(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, session.ErrNotFound) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
			} else {
				http.Error(w, "session store unavailable", http.StatusServiceUnavailable)
			}
			return
		}

		if evaluator == nil {
			emitDecision(r, sink, metrics, s.Subject, permission, "denied", "policy_unconfigured")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		allowed, err := evaluator.Allowed(r.Context(), policy.Subject{
			ID:    s.Subject,
			Roles: append([]string(nil), s.Roles...),
		}, permission)
		if err != nil {
			emitDecision(r, sink, metrics, s.Subject, permission, "error", "policy_error")
			http.Error(w, "authorization unavailable", http.StatusServiceUnavailable)
			return
		}

		if !allowed {
			emitDecision(r, sink, metrics, s.Subject, permission, "denied", "permission_denied")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		if metrics != nil {
			metrics.RecordAuthorization("allowed", permission)
		}
		next.ServeHTTP(w, r)
	})
}

func emitDecision(r *http.Request, sink audit.Sink, metrics *observability.Metrics, actor, permission, outcome, reason string) {
	if metrics != nil {
		metrics.RecordAuthorization(outcome, permission)
	}
	if sink == nil {
		return
	}

	e := audit.NewEvent("authorization."+outcome, permission, outcome)
	e.Actor = actor
	e.CorrelationID = RequestIDFromContext(r.Context())
	e.TraceID = TraceIDFromContext(r.Context())
	e.Attributes["reason"] = reason
	_ = sink.Append(r.Context(), e)
}
