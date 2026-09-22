package oidc

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"gateway-runtime/internal/auth"
)

type Provider struct {
	provider           *coreoidc.Provider
	verifier           *coreoidc.IDTokenVerifier
	oauth              oauth2.Config
	endSessionEndpoint string
}

type discoveryClaims struct {
	EndSessionEndpoint string `json:"end_session_endpoint"`
}

type idClaims struct {
	Subject           string   `json:"sub"`
	Issuer            string   `json:"iss"`
	Name              string   `json:"name"`
	PreferredUsername string   `json:"preferred_username"`
	Email             string   `json:"email"`
	Roles             []string `json:"roles"`
}

func New(ctx context.Context, cfg Config) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	discovered, err := coreoidc.NewProvider(ctx, strings.TrimRight(cfg.Issuer, "/"))
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}

	var meta discoveryClaims
	if err := discovered.Claims(&meta); err != nil {
		return nil, fmt.Errorf("oidc discovery claims: %w", err)
	}

	return &Provider{
		provider: discovered,
		verifier: discovered.Verifier(&coreoidc.Config{
			ClientID: cfg.ClientID,
		}),
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     discovered.Endpoint(),
			Scopes:       NormalizeScopes(cfg.Scopes),
		},
		endSessionEndpoint: meta.EndSessionEndpoint,
	}, nil
}

func (p *Provider) AuthorizationURL(_ context.Context, state, nonce, codeChallenge, redirectURI string) (string, error) {
	cfg := p.oauth
	cfg.RedirectURL = redirectURI
	return cfg.AuthCodeURL(
		state,
		coreoidc.Nonce(nonce),
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	), nil
}

func (p *Provider) Exchange(ctx context.Context, code, codeVerifier, redirectURI, expectedNonce string) (auth.ExchangeResult, error) {
	cfg := p.oauth
	cfg.RedirectURL = redirectURI

	oauthToken, err := cfg.Exchange(
		ctx,
		code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier),
	)
	if err != nil {
		return auth.ExchangeResult{}, fmt.Errorf("oidc token exchange: %w", err)
	}

	rawIDToken, ok := oauthToken.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return auth.ExchangeResult{}, fmt.Errorf("oidc token response missing id_token")
	}

	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return auth.ExchangeResult{}, fmt.Errorf("oidc verify id_token: %w", err)
	}
	if expectedNonce == "" || idToken.Nonce != expectedNonce {
		return auth.ExchangeResult{}, fmt.Errorf("oidc nonce mismatch")
	}

	var claims idClaims
	if err := idToken.Claims(&claims); err != nil {
		return auth.ExchangeResult{}, fmt.Errorf("oidc decode claims: %w", err)
	}

	displayName := claims.Name
	if displayName == "" {
		displayName = claims.PreferredUsername
	}

	refreshToken, _ := oauthToken.Extra("refresh_token").(string)
	if refreshToken == "" {
		refreshToken = oauthToken.RefreshToken
	}

	expiry := oauthToken.Expiry
	if expiry.IsZero() && idToken.Expiry.After(time.Now()) {
		expiry = idToken.Expiry
	}

	return auth.ExchangeResult{
		Identity: auth.Identity{
			Subject:     claims.Subject,
			Issuer:      claims.Issuer,
			DisplayName: displayName,
			Email:       claims.Email,
			Roles:       claims.Roles,
		},
		Tokens: auth.TokenSet{
			AccessToken:  oauthToken.AccessToken,
			RefreshToken: refreshToken,
			IDToken:      rawIDToken,
			Expiry:       expiry,
		},
	}, nil
}

func (p *Provider) EndSessionURL(_ context.Context, idTokenHint, postLogoutRedirectURI string) (string, error) {
	if p.endSessionEndpoint == "" {
		return postLogoutRedirectURI, nil
	}

	u, err := url.Parse(p.endSessionEndpoint)
	if err != nil {
		return "", fmt.Errorf("oidc end_session_endpoint: %w", err)
	}
	q := u.Query()
	if idTokenHint != "" {
		q.Set("id_token_hint", idTokenHint)
	}
	if postLogoutRedirectURI != "" {
		q.Set("post_logout_redirect_uri", postLogoutRedirectURI)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
