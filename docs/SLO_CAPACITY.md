# SLO, capacity, and performance engineering

M14 turns rebff metrics into a measurable capacity plan. The values below are starter targets, not claims about production capacity. Promote them to contractual SLOs only after representative load testing.

## Starter SLO

Availability target:

    99.9% successful service responses over a rolling 30-day window

For this BFF, server-side 5xx responses count against availability. Normal 2xx/3xx/4xx responses do not count as service failures. Rate-limit and authorization outcomes are tracked separately.

Operational endpoints are excluded:

- /health/live
- /health/ready
- /metrics

A 99.9% target corresponds to roughly 43 minutes of error budget in a 30-day window.

Starter latency target:

    P95 BFF request latency < 300 ms

This target should be evaluated separately for important route classes when enough traffic exists. OIDC redirect/login latency and large downstream business operations may need separate objectives.

## Recording and burn-rate rules

deploy/k8s/slo/prometheusrule.yaml provides:

- request rate
- 5xx rate
- 5-minute availability ratio
- P95 latency
- fast burn alert
- slow burn alert
- latency SLO alert

The fast/slow burn alerts use the 0.1% error budget implied by a 99.9% availability target.

## Capacity model

Do not size from registered-user count alone.

Translate user activity into request demand:

    required_rps =
      active_users * requests_per_user_per_minute / 60

Approximate concurrency with Little's Law:

    estimated_inflight =
      required_rps * p95_latency_seconds

Then size replicas from both measured throughput and safe concurrency:

    replicas_by_rps =
      ceil(required_rps * headroom / measured_rps_per_replica)

    replicas_by_inflight =
      ceil(estimated_inflight * headroom / safe_inflight_per_replica)

    replica_floor =
      max(min_replicas, replicas_by_rps, replicas_by_inflight)

The repository includes a calculator:

    go run ./cmd/capacity       -active-users 10000       -requests-per-user-minute 2       -measured-rps-per-replica 200       -p95-ms 300       -safe-inflight-per-replica 128       -headroom 1.3       -min-replicas 3

The measured values must come from a representative environment. Do not reuse laptop numbers as production capacity.

## Planning 1k / 10k / 100k users

Use the same formula at each population.

Example demand only, assuming two BFF requests per active user per minute:

| Active users | Approx request rate |
| ---: | ---: |
| 1,000 | 33.3 RPS |
| 10,000 | 333.3 RPS |
| 100,000 | 3,333.3 RPS |

These are demand examples, not supported-capacity claims.

Real systems usually have a smaller concurrently-active population than total accounts. Model at least:

- normal active population
- peak active population
- login storm after outage/deployment
- refresh-token concentration around token expiry
- downstream degradation
- Redis latency/failover

## Load testing

test/load/k6 contains two profiles.

Smoke:

    docker run --rm       -v "$PWD/test/load/k6:/scripts:ro"       grafana/k6:2.2.0 run /scripts/smoke.js

Arrival-rate test:

    docker run --rm       -e TARGET_URL=http://host.docker.internal:18080       -e PATH=/api/me       -e RATE=100       -e DURATION=5m       -e PRE_ALLOCATED_VUS=50       -e MAX_VUS=1000       -e SESSION_COOKIE=<opaque-session-id>       -v "$PWD/test/load/k6:/scripts:ro"       grafana/k6:2.2.0 run /scripts/api.js

Increase RATE progressively rather than jumping directly to extreme concurrency.

At each step capture:

- achieved RPS
- P50/P95/P99 latency
- error ratio
- BFF CPU/memory
- rebff_http_inflight_requests
- rate/concurrency rejections
- Redis command latency and memory
- downstream latency/error rate

The sustainable RPS/pod is the highest load that remains within SLO and without persistent admission rejection, CPU throttling, memory pressure, or dependency saturation.

## HPA baseline

deploy/k8s/hpa.yaml uses CPU as the initial autoscaling signal:

- min replicas: 3
- max replicas: 20
- target CPU utilization: 65%
- fast scale-up
- 5-minute scale-down stabilization

CPU HPA is deliberately the baseline because it works with standard Kubernetes resource metrics.

A future deployment can add an external/custom metric based on inflight requests or request rate when Prometheus Adapter or KEDA is part of the platform. Do not add a custom-metric HPA manifest until that dependency is explicitly present.

## Redis sizing

Redis capacity depends on concurrent sessions, not total registered accounts.

Each active BFF session can have:

- one session key
- one token-set key
- temporary user-context cache
- occasional refresh lock
- temporary OIDC flow state during login

Do not estimate Redis memory from JSON payload bytes alone. Redis key/object allocator overhead can materially change actual memory.

Measure representative keys in the target Redis version using MEMORY USAGE and derive:

    redis_session_memory =
      concurrent_sessions * measured_bytes_per_session_bundle * headroom

Use at least 30-50% memory headroom before maxmemory, then validate failover/replication overhead separately.

Token payload size varies significantly with issuer, claims, JWT size, and refresh-token length, so production sizing should use sampled real tokens rather than synthetic constants.

## Performance test gates

A release performance gate should compare against an accepted baseline rather than absolute RPS alone.

Suggested regression gates:

- P95 latency does not regress by more than an agreed percentage
- sustainable RPS/pod does not materially regress
- allocation/memory per request remains bounded
- no new rate/concurrency rejection under the baseline load
- no increase in 5xx/error-budget burn

Keep load-test execution outside ordinary unit CI unless the runner is dedicated and resource-stable.
