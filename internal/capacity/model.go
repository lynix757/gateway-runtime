package capacity

import (
	"fmt"
	"math"
	"time"
)

type Input struct {
	ActiveUsers            int
	RequestsPerUserMinute  float64
	MeasuredRPSPerReplica  float64
	P95Latency             time.Duration
	SafeInflightPerReplica float64
	HeadroomMultiplier     float64
	MinReplicas            int
}

type Result struct {
	RequiredRPS             float64
	EstimatedInflight       float64
	ReplicasByThroughput    int
	ReplicasByInflight      int
	RecommendedReplicaFloor int
}

func Calculate(in Input) (Result, error) {
	if in.ActiveUsers < 0 || in.RequestsPerUserMinute < 0 {
		return Result{}, fmt.Errorf("user demand must be >= 0")
	}
	if in.MeasuredRPSPerReplica <= 0 {
		return Result{}, fmt.Errorf("measured RPS per replica must be > 0")
	}
	if in.P95Latency < 0 {
		return Result{}, fmt.Errorf("P95 latency must be >= 0")
	}
	if in.SafeInflightPerReplica <= 0 {
		return Result{}, fmt.Errorf("safe inflight per replica must be > 0")
	}
	if in.HeadroomMultiplier < 1 {
		return Result{}, fmt.Errorf("headroom multiplier must be >= 1")
	}
	if in.MinReplicas < 1 {
		in.MinReplicas = 1
	}

	requiredRPS := float64(in.ActiveUsers) * in.RequestsPerUserMinute / 60
	inflight := requiredRPS * in.P95Latency.Seconds()
	throughputReplicas := int(math.Ceil(requiredRPS * in.HeadroomMultiplier / in.MeasuredRPSPerReplica))
	inflightReplicas := int(math.Ceil(inflight * in.HeadroomMultiplier / in.SafeInflightPerReplica))

	floor := max(in.MinReplicas, throughputReplicas, inflightReplicas)
	return Result{
		RequiredRPS:             requiredRPS,
		EstimatedInflight:       inflight,
		ReplicasByThroughput:    throughputReplicas,
		ReplicasByInflight:      inflightReplicas,
		RecommendedReplicaFloor: floor,
	}, nil
}
