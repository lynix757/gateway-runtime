package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"

	"gateway-runtime/internal/session"
	"gateway-runtime/internal/token"
)

type fakeProvider struct {
	lastState     string
	lastChallenge string
	lastVerifier  string
	lastNonce     string
}

func (f *fakeProvider) AuthorizationURL(_ context.Context, state, nonce, challenge, redirectURI string) (string, error) {
	f.lastState = state
	f.lastChallenge = challenge
	f.lastNonce = nonce
	return "https://idp.example/authorize?state=" + url.QueryEscape(state), nil
}

func (f *fakeProvider) Exchange(_ context.Context, code, verifier, redirectURI, expectedNonce string) (ExchangeResult, error) {
	f.lastVerifier = verifier
	if expectedNonce != f.lastNonce {
		return ExchangeResult{}, ErrFlowNotFound
	}
	return ExchangeResult{
		Identity: Identity{Subject: "user-1", Issuer: "https://idp.example"},
		Tokens: TokenSet{
			AccessToken:  "access",
			RefreshToken: "refresh",
			IDToken:      "id-token",
			Expiry:       time.Now().Add(time.Hour),
		},
	}, nil
}

func (f *fakeProvider) EndSessionURL(_ context.Context, idTokenHint, postLogoutRedirectURI string) (string, error) {
	return "https://idp.example/logout", nil
}

func newTestService(p IdentityProvider) (*Service, *session.MemoryStore, *token.MemoryStore) {
	sessions := session.NewMemoryStore()
	tokens := token.NewMemoryStore()
	return &Service{
		Provider:     p,
		Flows:        NewMemoryFlowStore(),
		Sessions:     sessions,
		Tokens:       tokens,
		RedirectURI:  "https://app.example/auth/callback",
		LogoutReturn: "https://app.example/",
	}, sessions, tokens
}

func TestLoginCallbackCreatesServerSideSessionAndTokens(t *testing.T) {
	p := &fakeProvider{}
	svc, sessions, tokens := newTestService(p)

	start, err := svc.StartLogin(context.Background(), "/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	if start.URL == "" || p.lastState == "" || p.lastChallenge == "" || p.lastNonce == "" {
		t.Fatal("missing authorization flow values")
	}

	result, err := svc.CompleteCallback(context.Background(), p.lastState, "code-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.ReturnTo != "/dashboard" {
		t.Fatalf("return_to = %q", result.ReturnTo)
	}
	if _, err := sessions.Get(context.Background(), result.SessionID); err != nil {
		t.Fatalf("session missing: %v", err)
	}
	gotTokens, err := tokens.Get(context.Background(), result.SessionID)
	if err != nil {
		t.Fatalf("tokens missing: %v", err)
	}
	if gotTokens.AccessToken != "access" || p.lastVerifier == "" {
		t.Fatal("token or PKCE verifier missing")
	}
	if pkceChallengeForTest(p.lastVerifier) != p.lastChallenge {
		t.Fatal("PKCE challenge does not match verifier")
	}
}

func TestLoginRejectsExternalReturnTo(t *testing.T) {
	svc, _, _ := newTestService(&fakeProvider{})
	for _, v := range []string{"https://evil.example/", "//evil.example/path"} {
		if _, err := svc.StartLogin(context.Background(), v); err == nil {
			t.Fatalf("expected %q to be rejected", v)
		}
	}
}

func TestStateIsOneTimeUse(t *testing.T) {
	p := &fakeProvider{}
	svc, _, _ := newTestService(p)
	if _, err := svc.StartLogin(context.Background(), "/"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteCallback(context.Background(), p.lastState, "code"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteCallback(context.Background(), p.lastState, "code"); err == nil {
		t.Fatal("expected reused state to fail")
	}
}

func pkceChallengeForTest(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return strings.TrimRight(base64.URLEncoding.EncodeToString(h[:]), "=")
}

func TestCallbackSessionLifetimeIsIndependentFromAccessTokenExpiry(t *testing.T) {
	p := &fakeProvider{}
	svc, sessions, _ := newTestService(p)
	now := time.Now().Truncate(time.Second)
	svc.Now = func() time.Time { return now }
	svc.SessionTTL = 8 * time.Hour

	if _, err := svc.StartLogin(context.Background(), "/"); err != nil {
		t.Fatal(err)
	}
	result, err := svc.CompleteCallback(context.Background(), p.lastState, "code")
	if err != nil {
		t.Fatal(err)
	}
	got, err := sessions.Get(context.Background(), result.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(8 * time.Hour); !got.ExpiresAt.Equal(want) {
		t.Fatalf("session expiry = %v, want %v", got.ExpiresAt, want)
	}
}
