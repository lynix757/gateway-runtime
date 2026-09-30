package middleware

import (
	"context"
	"errors"
	"net/http"

	"gateway-runtime/internal/audit"
	"gateway-runtime/internal/observability"
	"gateway-runtime/internal/policy"
	"gateway-runtime/internal/session"
)

type authenticatedSessionKey struct{}

func AuthenticatedSession(ctx context.Context) (session.Session, bool) {
	s, ok := ctx.Value(authenticatedSessionKey{}).(session.Session)
	return s, ok
}

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
			emitDecision(r, sink, metrics, "", "", permission, "deny", "unauthenticated", http.StatusUnauthorized)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		s, err := sessions.Get(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, session.ErrNotFound) {
				emitDecision(r, sink, metrics, "", "", permission, "deny", "invalid_session", http.StatusUnauthorized)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
			} else {
				emitDecision(r, sink, metrics, "", "", permission, "error", "session_store_error", http.StatusServiceUnavailable)
				http.Error(w, "session store unavailable", http.StatusServiceUnavailable)
			}
			return
		}

		if evaluator == nil {
			emitDecision(r, sink, metrics, s.Subject, s.Username, permission, "deny", "policy_unconfigured", http.StatusForbidden)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		allowed, err := evaluator.Allowed(r.Context(), policy.Subject{
			ID:    s.Subject,
			Roles: append([]string(nil), s.Roles...),
		}, permission)
		if err != nil {
			emitDecision(r, sink, metrics, s.Subject, s.Username, permission, "error", "policy_error", http.StatusServiceUnavailable)
			http.Error(w, "authorization unavailable", http.StatusServiceUnavailable)
			return
		}

		if !allowed {
			emitDecision(r, sink, metrics, s.Subject, s.Username, permission, "deny", "permission_denied", http.StatusForbidden)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		if metrics != nil {
			metrics.RecordAuthorization("allowed", permission)
		}
		emitDecision(r, sink, nil, s.Subject, s.Username, permission, "allow", "authorized", 0)
		ctx := context.WithValue(r.Context(), authenticatedSessionKey{}, s)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func emitDecision(r *http.Request, sink audit.Sink, metrics *observability.Metrics, actor, username, permission, outcome, reason string, status int) {
	if metrics != nil {
		metricOutcome := outcome
		if outcome == "deny" {
			metricOutcome = "denied"
		}
		metrics.RecordAuthorization(metricOutcome, permission)
	}
	if sink == nil {
		return
	}

	e := audit.NewEvent("authorization."+outcome, permission, outcome)
	e.Actor = actor
	e.ActorUsername = username
	e.HTTPStatus = status
	EnrichAuditEvent(r, &e)
	e.Attributes["reason"] = reason
	e.Attributes["request_method"] = r.Method
	e.Attributes["request_path"] = r.URL.Path
	_ = sink.Append(r.Context(), e)
}
