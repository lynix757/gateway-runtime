package session

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrNotFound = errors.New("session not found")

type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]Session
	now  func() time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string]Session), now: time.Now}
}

func (s *MemoryStore) Get(_ context.Context, id string) (Session, error) {
	s.mu.RLock()
	v, ok := s.data[id]
	s.mu.RUnlock()
	if !ok || (!v.ExpiresAt.IsZero() && !v.ExpiresAt.After(s.now())) {
		if ok {
			_ = s.Delete(context.Background(), id)
		}
		return Session{}, ErrNotFound
	}
	return v, nil
}

func (s *MemoryStore) Put(_ context.Context, v Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[v.ID] = v
	return nil
}

func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
	return nil
}
