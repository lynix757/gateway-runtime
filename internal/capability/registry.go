package capability

import (
	"fmt"
	"sync"
)

type Registry struct {
	mu        sync.RWMutex
	providers map[string]any
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]any)}
}

func (r *Registry) Register(name string, provider any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name == "" || provider == nil {
		return fmt.Errorf("capability: invalid provider registration")
	}
	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("capability %q already registered", name)
	}
	r.providers[name] = provider
	return nil
}

func (r *Registry) Get(name string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.providers[name]
	return v, ok
}

func (r *Registry) Require(names ...string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, name := range names {
		if _, ok := r.providers[name]; !ok {
			return fmt.Errorf("required capability %q is not configured", name)
		}
	}
	return nil
}
