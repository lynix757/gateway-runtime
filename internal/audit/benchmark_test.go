package audit

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func BenchmarkAuditSink(b *testing.B) {
	event := NewEvent("benchmark.action", "benchmark", "success")
	ctx := context.Background()

	b.Run("noop", func(b *testing.B) {
		sink := NoopSink{}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = sink.Append(ctx, event)
		}
	})

	b.Run("slog_discard", func(b *testing.B) {
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
		b.Cleanup(func() { slog.SetDefault(oldLogger) })
		sink := SlogSink{}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = sink.Append(ctx, event)
		}
	})
}
