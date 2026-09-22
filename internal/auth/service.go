package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gateway-runtime/internal/session"
	"gateway-runtime/internal/token"
)

const (
	SessionCookieName         = "__Host-bff_session"
	InsecureSessionCookieName = "bff_session"
	defaultFlowTTL            = 5 * time.Minute
	defaultSessionTTL         = 8 * time.Hour
)

var ErrInvalidReturnTo = errors.New("invalid return_to")

func SessionCookieNameFor(secure bool) string {
	if secure {
		return SessionCookieName
	}
	return InsecureSessionCookieName
}

type Service struct {
	Provider     IdentityProvider
	Flows        FlowStore
	Sessions     session.Store
	Tokens       token.Store
	RedirectURI  string
	LogoutReturn string
	FlowTTL      time.Duration
	SessionTTL   time.Duration
	Now          func() time.Time
}

type LoginStart struct{ URL string }

type CallbackResult struct {
	SessionID string
	ReturnTo  string
	ExpiresAt time.Time
}

func (s *Service) StartLogin(ctx context.Context, returnTo string) (LoginStart, error) {
	if s.Provider == nil || s.Flows == nil {
		return LoginStart{}, fmt.Errorf("auth service is not configured")
	}
	if returnTo == "" {
		returnTo = "/"
	}
	if !safeLocalReturnTo(returnTo) {
		return LoginStart{}, ErrInvalidReturnTo
	}

	state, err := randomURLSafe(32)
	if err != nil {
		return LoginStart{}, err
	}
	nonce, err := randomURLSafe(32)
	if err != nil {
		return LoginStart{}, err
	}
	verifier, err := randomURLSafe(64)
	if err != nil {
		return LoginStart{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	now := s.now()
	ttl := s.FlowTTL
	if ttl <= 0 {
		ttl = defaultFlowTTL
	}
	if err := s.Flows.Put(ctx, Flow{
		State: state, Nonce: nonce, CodeVerifier: verifier,
		ReturnTo: returnTo, ExpiresAt: now.Add(ttl),
	}); err != nil {
		return LoginStart{}, err
	}
	authURL, err := s.Provider.AuthorizationURL(ctx, state, nonce, challenge, s.RedirectURI)
	if err != nil {
		return LoginStart{}, err
	}
	return LoginStart{URL: authURL}, nil
}

func (s *Service) CompleteCallback(ctx context.Context, state, code string) (CallbackResult, error) {
	if state == "" || code == "" {
		return CallbackResult{}, fmt.Errorf("missing state or code")
	}
	flow, err := s.Flows.Take(ctx, state)
	if err != nil {
		return CallbackResult{}, err
	}
	result, err := s.Provider.Exchange(ctx, code, flow.CodeVerifier, s.RedirectURI, flow.Nonce)
	if err != nil {
		return CallbackResult{}, err
	}
	if result.Identity.Subject == "" || result.Identity.Issuer == "" {
		return CallbackResult{}, fmt.Errorf("provider returned incomplete identity")
	}

	sessionID, err := randomURLSafe(32)
	if err != nil {
		return CallbackResult{}, err
	}
	now := s.now()
	ttl := s.SessionTTL
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	expires := now.Add(ttl)

	if err := s.Sessions.Put(ctx, session.Session{
		ID:          sessionID,
		Subject:     result.Identity.Subject,
		Issuer:      result.Identity.Issuer,
		DisplayName: result.Identity.DisplayName,
		Email:       result.Identity.Email,
		Roles:       append([]string(nil), result.Identity.Roles...),
		CreatedAt:   now,
		ExpiresAt:   expires,
		LastSeen:    now,
	}); err != nil {
		return CallbackResult{}, err
	}
	if err := s.Tokens.Put(ctx, sessionID, token.Set{
		AccessToken:  result.Tokens.AccessToken,
		RefreshToken: result.Tokens.RefreshToken,
		IDToken:      result.Tokens.IDToken,
		ExpiryUnix:   result.Tokens.Expiry.Unix(),
	}, expires); err != nil {
		_ = s.Sessions.Delete(ctx, sessionID)
		return CallbackResult{}, err
	}
	return CallbackResult{SessionID: sessionID, ReturnTo: flow.ReturnTo, ExpiresAt: expires}, nil
}

func (s *Service) Logout(ctx context.Context, sessionID string, global bool) (string, error) {
	if sessionID == "" {
		return s.LogoutReturn, nil
	}

	var idToken string
	if s.Tokens != nil {
		ts, err := s.Tokens.Get(ctx, sessionID)
		switch {
		case err == nil:
			idToken = ts.IDToken
		case errors.Is(err, token.ErrNotFound):
		default:
			return "", fmt.Errorf("load logout token: %w", err)
		}
		if err := s.Tokens.Delete(ctx, sessionID); err != nil {
			return "", fmt.Errorf("delete logout token: %w", err)
		}
	}

	if s.Sessions == nil {
		return "", fmt.Errorf("session store is not configured")
	}
	if err := s.Sessions.Delete(ctx, sessionID); err != nil {
		return "", fmt.Errorf("delete session: %w", err)
	}

	if global && idToken != "" {
		return s.Provider.EndSessionURL(ctx, idToken, s.LogoutReturn)
	}
	return s.LogoutReturn, nil
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func randomURLSafe(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func safeLocalReturnTo(v string) bool {
	u, err := url.Parse(v)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/") {
		return false
	}
	return !strings.HasPrefix(v, "//")
}
