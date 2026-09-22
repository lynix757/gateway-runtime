package auth

import (
	"context"
	"errors"
)

var ErrProviderUnconfigured = errors.New("identity provider is not configured")

type UnconfiguredProvider struct{}

func (UnconfiguredProvider) AuthorizationURL(context.Context, string, string, string, string) (string, error) {
	return "", ErrProviderUnconfigured
}
func (UnconfiguredProvider) Exchange(context.Context, string, string, string, string) (ExchangeResult, error) {
	return ExchangeResult{}, ErrProviderUnconfigured
}
func (UnconfiguredProvider) EndSessionURL(context.Context, string, string) (string, error) {
	return "", ErrProviderUnconfigured
}
