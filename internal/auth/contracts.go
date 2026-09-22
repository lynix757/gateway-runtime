package auth

import (
	"context"
	"time"
)

type Identity struct {
	Subject     string
	Issuer      string
	DisplayName string
	Email       string
	Roles       []string
}

type TokenSet struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	Expiry       time.Time
}

type ExchangeResult struct {
	Identity Identity
	Tokens   TokenSet
}

type IdentityProvider interface {
	AuthorizationURL(ctx context.Context, state, nonce, codeChallenge, redirectURI string) (string, error)
	Exchange(ctx context.Context, code, codeVerifier, redirectURI, expectedNonce string) (ExchangeResult, error)
	EndSessionURL(ctx context.Context, idTokenHint, postLogoutRedirectURI string) (string, error)
}
