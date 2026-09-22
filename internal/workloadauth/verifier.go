package workloadauth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
)

type Principal struct {
	Service string
	Subject string
	Issuer  string
}

type Verifier interface {
	Verify(ctx context.Context, rawToken string) (Principal, error)
}

type OIDCConfig struct {
	Issuer   string
	Audience string
}

type OIDCVerifier struct {
	verifier *coreoidc.IDTokenVerifier
	issuer   string
}

type claims struct {
	Subject  string `json:"sub"`
	Issuer   string `json:"iss"`
	ClientID string `json:"client_id"`
	AZP      string `json:"azp"`
}

func NewOIDC(ctx context.Context, cfg OIDCConfig) (*OIDCVerifier, error) {
	cfg.Issuer = strings.TrimSpace(cfg.Issuer)
	cfg.Audience = strings.TrimSpace(cfg.Audience)
	if cfg.Issuer == "" || cfg.Audience == "" {
		return nil, fmt.Errorf("workload oidc issuer and audience are required")
	}

	provider, err := coreoidc.NewProvider(ctx, strings.TrimRight(cfg.Issuer, "/"))
	if err != nil {
		return nil, fmt.Errorf("workload oidc discovery: %w", err)
	}

	return &OIDCVerifier{
		verifier: provider.Verifier(&coreoidc.Config{ClientID: cfg.Audience}),
		issuer:   strings.TrimRight(cfg.Issuer, "/"),
	}, nil
}

func (v *OIDCVerifier) Verify(ctx context.Context, rawToken string) (Principal, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return Principal{}, fmt.Errorf("workload token is required")
	}

	token, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return Principal{}, fmt.Errorf("verify workload token: %w", err)
	}

	var c claims
	if err := token.Claims(&c); err != nil {
		return Principal{}, fmt.Errorf("decode workload claims: %w", err)
	}

	service := strings.TrimSpace(c.ClientID)
	if service == "" {
		service = strings.TrimSpace(c.AZP)
	}
	if service == "" {
		service = strings.TrimSpace(c.Subject)
	}
	if service == "" {
		return Principal{}, fmt.Errorf("workload token missing service identity")
	}

	return Principal{
		Service: service,
		Subject: strings.TrimSpace(c.Subject),
		Issuer:  strings.TrimSpace(c.Issuer),
	}, nil
}

func BearerToken(r *http.Request) (string, error) {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if value == "" {
		return "", fmt.Errorf("authorization header required")
	}
	scheme, token, ok := strings.Cut(value, " ")
	if !ok || !strings.EqualFold(strings.TrimSpace(scheme), "Bearer") || strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("bearer token required")
	}
	return strings.TrimSpace(token), nil
}
