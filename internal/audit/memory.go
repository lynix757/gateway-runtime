package audit

import (
	"context"
	"sync"
)

type MemorySink struct {
	mu     sync.Mutex
	Events []Event
}

func (m *MemorySink) Append(_ context.Context, e Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Events = append(m.Events, e)
	return nil
}

func (m *MemorySink) Snapshot() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, len(m.Events))
	copy(out, m.Events)
	return out
}
