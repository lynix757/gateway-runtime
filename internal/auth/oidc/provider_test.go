package oidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestAuthorizationURLUsesDiscoveryAndPKCE(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                                srv.URL,
				"authorization_endpoint":                srv.URL + "/authorize",
				"token_endpoint":                        srv.URL + "/token",
				"jwks_uri":                              srv.URL + "/jwks",
				"end_session_endpoint":                  srv.URL + "/logout",
				"response_types_supported":              []string{"code"},
				"subject_types_supported":               []string{"public"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := New(context.Background(), Config{
		Issuer:   srv.URL,
		ClientID: "portal-bff",
		Scopes:   []string{"profile", "email"},
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := p.AuthorizationURL(
		context.Background(),
		"state-1", "nonce-1", "challenge-1",
		"https://app.example/portal/auth/callback",
	)
	if err != nil {
		t.Fatal(err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	checks := map[string]string{
		"client_id":             "portal-bff",
		"state":                 "state-1",
		"nonce":                 "nonce-1",
		"code_challenge":        "challenge-1",
		"code_challenge_method": "S256",
		"redirect_uri":          "https://app.example/portal/auth/callback",
	}
	for key, want := range checks {
		if got := q.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if q.Get("scope") != "openid profile email" {
		t.Fatalf("scope = %q", q.Get("scope"))
	}

	logout, err := p.EndSessionURL(context.Background(), "id-token", "https://app.example/")
	if err != nil {
		t.Fatal(err)
	}
	logoutURL, _ := url.Parse(logout)
	if logoutURL.Path != "/logout" || logoutURL.Query().Get("id_token_hint") != "id-token" {
		t.Fatalf("unexpected logout URL %q", logout)
	}
}
