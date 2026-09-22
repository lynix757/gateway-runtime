package audit

import (
	"context"
	"log/slog"
)

type SlogSink struct{}

func (SlogSink) Append(_ context.Context, e Event) error {
	slog.Info("audit",
		"event_id", e.ID,
		"occurred_at", e.OccurredAt,
		"actor", e.Actor,
		"action", e.Action,
		"target", e.Target,
		"outcome", e.Outcome,
		"correlation_id", e.CorrelationID,
		"trace_id", e.TraceID,
		"attributes", e.Attributes,
	)
	return nil
}
