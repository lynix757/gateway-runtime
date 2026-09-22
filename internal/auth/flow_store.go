package auth

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrFlowNotFound = errors.New("auth flow not found")

type Flow struct {
	State        string
	Nonce        string
	CodeVerifier string
	ReturnTo     string
	ExpiresAt    time.Time
}

type FlowStore interface {
	Put(ctx context.Context, flow Flow) error
	Take(ctx context.Context, state string) (Flow, error)
}

type MemoryFlowStore struct {
	mu   sync.Mutex
	data map[string]Flow
	now  func() time.Time
}

func NewMemoryFlowStore() *MemoryFlowStore {
	return &MemoryFlowStore{data: make(map[string]Flow), now: time.Now}
}

func (s *MemoryFlowStore) Put(_ context.Context, flow Flow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[flow.State] = flow
	return nil
}

func (s *MemoryFlowStore) Take(_ context.Context, state string) (Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flow, ok := s.data[state]
	if !ok {
		return Flow{}, ErrFlowNotFound
	}
	delete(s.data, state)
	if !flow.ExpiresAt.After(s.now()) {
		return Flow{}, ErrFlowNotFound
	}
	return flow, nil
}
