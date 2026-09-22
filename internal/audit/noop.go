package audit

import "context"

type NoopSink struct{}

func (NoopSink) Append(context.Context, Event) error { return nil }

var _ Sink = NoopSink{}
