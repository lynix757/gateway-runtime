package observability

import (
	"testing"
	"time"
)

func BenchmarkMetricsConcurrent(b *testing.B) {
	m := NewMetrics()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.RecordHTTPRequest("GET", "GET /api/items/{id}", 200, 250*time.Microsecond)
		}
	})
}

func BenchmarkMetricsConcurrentMixed(b *testing.B) {
	m := NewMetrics()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.RecordHTTPRequest("GET", "GET /api/items/{id}", 200, 250*time.Microsecond)
			m.RecordAuthorization("allowed", "managed-item.read")
			m.AddInflight(1)
			m.AddInflight(-1)
		}
	})
}
