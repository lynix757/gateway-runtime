package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"gateway-runtime/internal/audit"
	"gateway-runtime/internal/cache"
	mw "gateway-runtime/internal/httpx/middleware"
	"gateway-runtime/internal/observability"
	"gateway-runtime/internal/session"
	"gateway-runtime/internal/usercontext"
)

type Handler struct {
	Service     *Service
	UserContext usercontext.Resolver
	Cache       cache.Cache
	Audit       audit.Sink
	Metrics     *observability.Metrics
	Secure      bool
	CookieName  string
}

func (h Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", h.login)
	mux.HandleFunc("GET /auth/callback", h.callback)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.HandleFunc("GET /api/me", h.me)
}

func (h Handler) login(w http.ResponseWriter, r *http.Request) {
	start, err := h.Service.StartLogin(r.Context(), r.URL.Query().Get("return_to"))
	if err != nil {
		if errors.Is(err, ErrInvalidReturnTo) {
			http.Error(w, "invalid login request", http.StatusBadRequest)
		} else {
			http.Error(w, "authentication service unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	http.Redirect(w, r, start.URL, http.StatusFound)
}

func (h Handler) callback(w http.ResponseWriter, r *http.Request) {
	result, err := h.Service.CompleteCallback(r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("code"))
	if err != nil {
		if h.Metrics != nil {
			h.Metrics.RecordAuth("failure")
		}
		h.emit(r, "", "auth.login", "session", "failure", nil)
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}
	if h.Metrics != nil {
		h.Metrics.RecordAuth("success")
	}
	h.emit(r, "", "auth.login", "session", "success", nil)

	http.SetCookie(w, &http.Cookie{
		Name: h.cookieName(), Value: result.SessionID,
		Path: "/", Secure: h.Secure, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Expires: result.ExpiresAt,
	})
	http.Redirect(w, r, result.ReturnTo, http.StatusFound)
}

func (h Handler) logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(h.cookieName())
	sessionID := ""
	actor := ""
	if cookie != nil {
		sessionID = cookie.Value
		if h.UserContext != nil {
			if uc, err := h.UserContext.Resolve(r.Context(), sessionID); err == nil {
				actor = uc.Subject
			}
		}
	}
	global := strings.EqualFold(r.URL.Query().Get("global"), "true")
	redirectTo, err := h.Service.Logout(r.Context(), sessionID, global)
	if err != nil {
		h.emit(r, actor, "auth.logout", "session", "failure", map[string]string{"global": boolString(global)})
		http.Error(w, "logout unavailable", http.StatusServiceUnavailable)
		return
	}
	h.emit(r, actor, "auth.logout", "session", "success", map[string]string{"global": boolString(global)})
	if h.Cache != nil && sessionID != "" {
		_ = h.Cache.Delete(r.Context(), usercontext.CacheKey(sessionID))
	}
	http.SetCookie(w, &http.Cookie{
		Name: h.cookieName(), Value: "", Path: "/",
		Secure: h.Secure, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		MaxAge: -1,
	})
	http.Redirect(w, r, redirectTo, http.StatusSeeOther)
}

func (h Handler) me(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(h.cookieName())
	if err != nil || h.UserContext == nil || h.Service == nil || h.Service.Sessions == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if _, err := h.Service.Sessions.Get(r.Context(), cookie.Value); err != nil {
		if errors.Is(err, session.ErrNotFound) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		} else {
			http.Error(w, "session store unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	ctx, err := h.UserContext.Resolve(r.Context(), cookie.Value)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(ctx)
}

func (h Handler) emit(r *http.Request, actor, action, target, outcome string, attrs map[string]string) {
	if h.Audit == nil {
		return
	}
	e := audit.NewEvent(action, target, outcome)
	e.Actor = actor
	e.CorrelationID = mw.RequestIDFromContext(r.Context())
	e.TraceID = mw.TraceIDFromContext(r.Context())
	if attrs != nil {
		e.Attributes = attrs
	}
	_ = h.Audit.Append(r.Context(), e)
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func (h Handler) cookieName() string {
	if h.CookieName != "" {
		return h.CookieName
	}
	return SessionCookieName
}
