package token

import (
	"context"
	"sync"
	"time"
)

type memorySemaphore chan struct{}

type MemoryLocker struct {
	mu    sync.Mutex
	locks map[string]memorySemaphore
}

func NewMemoryLocker() *MemoryLocker {
	return &MemoryLocker{locks: make(map[string]memorySemaphore)}
}

func (l *MemoryLocker) Lock(ctx context.Context, key string, _ time.Duration) (func() error, error) {
	l.mu.Lock()
	sem := l.locks[key]
	if sem == nil {
		sem = make(memorySemaphore, 1)
		sem <- struct{}{}
		l.locks[key] = sem
	}
	l.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-sem:
		return func() error {
			sem <- struct{}{}
			return nil
		}, nil
	}
}
