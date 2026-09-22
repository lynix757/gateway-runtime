package oidc

import (
	"fmt"
	"strings"
)

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Issuer) == "" {
		return fmt.Errorf("oidc issuer is required")
	}
	if strings.TrimSpace(c.ClientID) == "" {
		return fmt.Errorf("oidc client id is required")
	}
	return nil
}

func NormalizeScopes(scopes []string) []string {
	seen := map[string]bool{"openid": true}
	out := []string{"openid"}
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" || seen[scope] {
			continue
		}
		seen[scope] = true
		out = append(out, scope)
	}
	return out
}
