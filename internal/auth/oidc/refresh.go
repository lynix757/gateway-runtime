package oidc

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"

	"gateway-runtime/internal/token"
)

func (p *Provider) RefreshTokens(ctx context.Context, refreshToken string) (token.Set, error) {
	if refreshToken == "" {
		return token.Set{}, fmt.Errorf("refresh token is required")
	}

	refreshed, err := p.oauth.TokenSource(ctx, &oauth2.Token{
		RefreshToken: refreshToken,
	}).Token()
	if err != nil {
		return token.Set{}, fmt.Errorf("oidc token refresh: %w", err)
	}
	if refreshed.AccessToken == "" {
		return token.Set{}, fmt.Errorf("oidc token refresh missing access token")
	}

	nextRefresh := refreshed.RefreshToken
	if nextRefresh == "" {
		nextRefresh = refreshToken
	}
	rawIDToken, _ := refreshed.Extra("id_token").(string)
	if refreshed.Expiry.IsZero() {
		return token.Set{}, fmt.Errorf("oidc token refresh missing expiry")
	}

	return token.Set{
		AccessToken:  refreshed.AccessToken,
		RefreshToken: nextRefresh,
		IDToken:      rawIDToken,
		ExpiryUnix:   refreshed.Expiry.Unix(),
	}, nil
}

var _ token.Refresher = (*Provider)(nil)
