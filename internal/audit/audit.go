package audit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

type Event struct {
	ID            string
	OccurredAt    time.Time
	Actor         string
	Action        string
	Target        string
	Outcome       string
	CorrelationID string
	TraceID       string
	Attributes    map[string]string
}

type Sink interface {
	Append(ctx context.Context, event Event) error
}

func NewEvent(action, target, outcome string) Event {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return Event{
		ID:         hex.EncodeToString(b[:]),
		OccurredAt: time.Now().UTC(),
		Action:     action,
		Target:     target,
		Outcome:    outcome,
		Attributes: map[string]string{},
	}
}
