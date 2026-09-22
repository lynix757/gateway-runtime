package oidc

import (
	"reflect"
	"testing"
)

func TestNormalizeScopes(t *testing.T) {
	got := NormalizeScopes([]string{"profile", "openid", " email ", "profile"})
	want := []string{"openid", "profile", "email"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeScopes = %#v, want %#v", got, want)
	}
}

func TestConfigValidation(t *testing.T) {
	if err := (Config{}).Validate(); err == nil {
		t.Fatal("expected empty config to fail")
	}
	if err := (Config{Issuer: "https://idp.example", ClientID: "bff"}).Validate(); err != nil {
		t.Fatalf("valid config failed: %v", err)
	}
}
