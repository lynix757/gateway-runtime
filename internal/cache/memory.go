package cache

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

type memoryEntry struct {
	Data      []byte
	ExpiresAt time.Time
}

type Memory struct {
	mu   sync.RWMutex
	data map[string]memoryEntry
	now  func() time.Time
}

func NewMemory() *Memory {
	return &Memory{data: make(map[string]memoryEntry), now: time.Now}
}

func (m *Memory) Get(_ context.Context, key string, dst any) (bool, error) {
	m.mu.RLock()
	entry, ok := m.data[key]
	m.mu.RUnlock()
	if !ok {
		return false, nil
	}
	if !entry.ExpiresAt.IsZero() && !entry.ExpiresAt.After(m.now()) {
		_ = m.Delete(context.Background(), key)
		return false, nil
	}
	if err := json.Unmarshal(entry.Data, dst); err != nil {
		return false, err
	}
	return true, nil
}

func (m *Memory) Set(_ context.Context, key string, value any, ttl time.Duration) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = memoryEntry{Data: b, ExpiresAt: m.now().Add(ttl)}
	return nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}
