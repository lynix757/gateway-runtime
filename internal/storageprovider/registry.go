package storageprovider

import (
	"fmt"
	"strings"

	"gateway-runtime/internal/capability"
)

type Provider struct {
	Signer capability.StorageSigner
}

type Registry map[string]Provider

func (r Registry) Resolve(name string) (Provider, bool) {
	provider, ok := r[strings.TrimSpace(name)]
	return provider, ok
}

func (r Registry) ValidateRequired(names ...string) error {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			name = "default"
		}
		provider, ok := r.Resolve(name)
		if !ok {
			return fmt.Errorf("storage provider %q is not configured", name)
		}
		if provider.Signer == nil {
			return fmt.Errorf("storage provider %q signer is not configured", name)
		}
	}
	return nil
}
