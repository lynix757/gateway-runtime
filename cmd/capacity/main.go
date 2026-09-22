package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"gateway-runtime/internal/capacity"
)

func main() {
	var (
		users      = flag.Int("active-users", 1000, "concurrently active users in the planning window")
		reqPerMin  = flag.Float64("requests-per-user-minute", 2, "average BFF requests per active user per minute")
		rpsPerPod  = flag.Float64("measured-rps-per-replica", 100, "sustainable measured RPS per replica")
		p95ms      = flag.Float64("p95-ms", 300, "measured P95 BFF latency in milliseconds")
		inflight   = flag.Float64("safe-inflight-per-replica", 128, "safe measured inflight requests per replica")
		headroom   = flag.Float64("headroom", 1.3, "capacity headroom multiplier")
		minReplica = flag.Int("min-replicas", 3, "minimum replica floor")
	)
	flag.Parse()

	result, err := capacity.Calculate(capacity.Input{
		ActiveUsers:            *users,
		RequestsPerUserMinute:  *reqPerMin,
		MeasuredRPSPerReplica:  *rpsPerPod,
		P95Latency:             time.Duration(*p95ms * float64(time.Millisecond)),
		SafeInflightPerReplica: *inflight,
		HeadroomMultiplier:     *headroom,
		MinReplicas:            *minReplica,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("required_rps=%.2f\n", result.RequiredRPS)
	fmt.Printf("estimated_inflight_at_p95=%.2f\n", result.EstimatedInflight)
	fmt.Printf("replicas_by_throughput=%d\n", result.ReplicasByThroughput)
	fmt.Printf("replicas_by_inflight=%d\n", result.ReplicasByInflight)
	fmt.Printf("recommended_replica_floor=%d\n", result.RecommendedReplicaFloor)
}
