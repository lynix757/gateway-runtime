package capacity

import (
	"testing"
	"time"
)

func TestCalculateUsesLargerCapacityConstraint(t *testing.T) {
	got, err := Calculate(Input{
		ActiveUsers:            10000,
		RequestsPerUserMinute:  2,
		MeasuredRPSPerReplica:  200,
		P95Latency:             300 * time.Millisecond,
		SafeInflightPerReplica: 50,
		HeadroomMultiplier:     1.3,
		MinReplicas:            3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.RequiredRPS < 333 || got.RequiredRPS > 334 {
		t.Fatalf("required RPS = %f", got.RequiredRPS)
	}
	if got.RecommendedReplicaFloor < 3 {
		t.Fatalf("replica floor = %d", got.RecommendedReplicaFloor)
	}
}
