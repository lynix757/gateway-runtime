package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"gateway-runtime/internal/app"
	"gateway-runtime/internal/auth"
	"gateway-runtime/internal/httpx"
	mw "gateway-runtime/internal/httpx/middleware"
	"gateway-runtime/internal/outbound"
)

type Application interface {
	Register(*Router) error
}

type ApplicationFunc func(*Router) error

func (f ApplicationFunc) Register(r *Router) error { return f(r) }

func Run(application Application) error {
	return app.RunWith(app.Options{
		BuildAppRoutes: func(deps app.RouteDeps) (httpx.RouteRegistrar, error) {
			r := &Router{deps: deps}
			if application != nil {
				if err := application.Register(r); err != nil {
					return nil, err
				}
			}
			return r, nil
		},
	})
}

type route struct {
	pattern string
	handler http.Handler
}

type Router struct {
	deps   app.RouteDeps
	routes []route
}

func (r *Router) Handle(pattern, permission string, handler http.Handler) {
	if permission != "" {
		handler = mw.RequirePermissionWithAuditCookie(
			permission,
			r.deps.SessionCookieName,
			r.deps.Sessions,
			r.deps.Policy,
			r.deps.Audit,
			r.deps.Metrics,
			handler,
		)
	}
	r.routes = append(r.routes, route{pattern: pattern, handler: handler})
}

func (r *Router) HandleFunc(pattern, permission string, handler http.HandlerFunc) {
	r.Handle(pattern, permission, handler)
}

func (r *Router) Register(mux *http.ServeMux) {
	for _, rt := range r.routes {
		mux.Handle(rt.pattern, rt.handler)
	}
}

type ClientOptions struct {
	Timeout          time.Duration
	MaxConcurrent    int
	FailureThreshold int
	OpenFor          time.Duration
}

type JSONClient struct {
	next outbound.JSONDoer
}

func (r *Router) NewJSONClient(serviceName, baseURL string, opts ClientOptions) (*JSONClient, error) {
	client, err := outbound.New(baseURL, opts.Timeout)
	if err != nil {
		return nil, err
	}
	client.Tokens = r.deps.Tokens

	var next outbound.JSONDoer = client
	next = outbound.ObservedClient{
		Next:        next,
		Metrics:     r.deps.Metrics,
		ServiceName: serviceName,
	}
	next = &outbound.ProtectedClient{
		Next:             next,
		MaxConcurrent:    opts.MaxConcurrent,
		FailureThreshold: opts.FailureThreshold,
		OpenFor:          opts.OpenFor,
		ServiceName:      serviceName,
		Metrics:          r.deps.Metrics,
	}
	return &JSONClient{next: next}, nil
}

func (c *JSONClient) DoJSON(ctx context.Context, sessionID, method, path string, in, out any) error {
	return c.next.DoJSON(ctx, sessionID, method, path, in, out)
}

func SessionID(r *http.Request) (string, bool) {
	for _, name := range []string{auth.SessionCookieName, auth.InsecureSessionCookieName} {
		cookie, err := r.Cookie(name)
		if err == nil && cookie.Value != "" {
			return cookie.Value, true
		}
	}
	return "", false
}

func HTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	var oe *outbound.Error
	if errors.As(err, &oe) {
		switch {
		case outbound.IsKind(err, outbound.ErrUnauthorized):
			return http.StatusUnauthorized
		case outbound.IsKind(err, outbound.ErrForbidden):
			return http.StatusForbidden
		case outbound.IsKind(err, outbound.ErrRateLimited):
			return http.StatusTooManyRequests
		case outbound.IsKind(err, outbound.ErrUnavailable):
			return http.StatusServiceUnavailable
		default:
			if oe.StatusCode >= 400 && oe.StatusCode <= 599 {
				return oe.StatusCode
			}
		}
	}
	return http.StatusBadGateway
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func WriteError(w http.ResponseWriter, err error) {
	status := HTTPStatus(err)
	http.Error(w, http.StatusText(status), status)
}
