package token

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrNotFound = errors.New("token set not found")

type memoryEntry struct {
	Set       Set
	ExpiresAt time.Time
}

type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]memoryEntry
	now  func() time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string]memoryEntry), now: time.Now}
}

func (s *MemoryStore) Get(_ context.Context, sessionID string) (Set, error) {
	s.mu.RLock()
	v, ok := s.data[sessionID]
	s.mu.RUnlock()
	if !ok || (!v.ExpiresAt.IsZero() && !v.ExpiresAt.After(s.now())) {
		if ok {
			_ = s.Delete(context.Background(), sessionID)
		}
		return Set{}, ErrNotFound
	}
	return v.Set, nil
}

func (s *MemoryStore) Put(_ context.Context, sessionID string, v Set, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[sessionID] = memoryEntry{Set: v, ExpiresAt: expiresAt}
	return nil
}

func (s *MemoryStore) Delete(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, sessionID)
	return nil
}
