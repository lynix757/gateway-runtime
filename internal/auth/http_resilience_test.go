package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"gateway-runtime/internal/session"
	"gateway-runtime/internal/usercontext"
)

type failingFlowStore struct{}

func (failingFlowStore) Put(context.Context, Flow) error { return errors.New("flow store down") }
func (failingFlowStore) Take(context.Context, string) (Flow, error) {
	return Flow{}, errors.New("flow store down")
}

type failingSessionStore struct{}

func (failingSessionStore) Get(context.Context, string) (session.Session, error) {
	return session.Session{}, errors.New("session store down")
}
func (failingSessionStore) Put(context.Context, session.Session) error {
	return errors.New("session store down")
}
func (failingSessionStore) Delete(context.Context, string) error {
	return errors.New("session store down")
}

func TestLoginDependencyFailureReturns503(t *testing.T) {
	h := Handler{
		Service: &Service{
			Provider: fakeProviderForHTTP{},
			Flows:    failingFlowStore{},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rec := httptest.NewRecorder()

	h.login(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestLoginInvalidReturnToReturns400(t *testing.T) {
	h := Handler{
		Service: &Service{
			Provider: fakeProviderForHTTP{},
			Flows:    NewMemoryFlowStore(),
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/login?return_to=https://evil.example", nil)
	rec := httptest.NewRecorder()

	h.login(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMeSessionStoreFailureReturns503(t *testing.T) {
	h := Handler{
		Service:     &Service{Sessions: failingSessionStore{}},
		UserContext: nil,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "sid"})
	rec := httptest.NewRecorder()

	// Provide a non-nil resolver only after the initial guard.
	h.UserContext = resolverStub{}
	h.me(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

type resolverStub struct{}

func (resolverStub) Resolve(context.Context, string) (usercontext.Context, error) {
	return usercontext.Context{}, nil
}

type fakeProviderForHTTP struct{}

func (fakeProviderForHTTP) AuthorizationURL(context.Context, string, string, string, string) (string, error) {
	return "https://idp.example/login", nil
}
func (fakeProviderForHTTP) Exchange(context.Context, string, string, string, string) (ExchangeResult, error) {
	return ExchangeResult{}, nil
}
func (fakeProviderForHTTP) EndSessionURL(context.Context, string, string) (string, error) {
	return "", nil
}

func TestLogoutSessionStoreFailureReturns503(t *testing.T) {
	h := Handler{
		Service: &Service{
			Sessions:     failingSessionStore{},
			LogoutReturn: "https://example.com/",
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "sid"})
	rec := httptest.NewRecorder()

	h.logout(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
