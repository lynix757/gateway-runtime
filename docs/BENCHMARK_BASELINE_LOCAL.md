# Local benchmark baseline

Date: 2026-09-21

This file records a local engineering baseline for relative feature-overhead comparison. It is not a production capacity claim.

## Environment

- Host CPU: Apple M2 Max
- Host architecture: darwin/arm64
- BFF: host-native compiled binary
- Load generator: k6 2.2.0 in Colima Docker
- Store: memory
- Endpoint: GET /health/live
- General/auth rate limit: disabled for the matrix
- Requested load: 2,000 iterations/second
- Duration: 10 seconds per profile
- Pre-allocated VUs: 100
- Max VUs: 1,000

The benchmark runner verifies the test port is free and verifies the newly started BFF process remains alive before load is generated. This was added after an earlier invalid run exposed an orphan process from a prior go-run based runner.

## Feature matrix

| Profile | RPS | P95 ms | Avg ms | Fail rate | Avg CPU | Max CPU | Avg RSS MiB | P95 delta | CPU delta |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| minimal | 1999.73 | 0.603 | 0.363 | 0.0000 | 23.64% | 28.90% | 21.32 | baseline | baseline |
| metrics | 1999.92 | 0.594 | 0.349 | 0.0000 | 23.49% | 28.00% | 22.07 | -1.47% | -0.62% |
| trace | 2000.01 | 0.590 | 0.391 | 0.0000 | 24.47% | 30.30% | 21.71 | -2.15% | +3.51% |
| access-log | 2000.00 | 0.664 | 0.384 | 0.0000 | 29.70% | 34.50% | 21.81 | +10.05% | +25.64% |
| all-on | 1999.94 | 0.652 | 0.388 | 0.0000 | 31.11% | 38.60% | 22.09 | +8.08% | +31.60% |

All profiles sustained the requested 2,000 RPS with zero HTTP failures.

Do not interpret small negative P95 deltas as feature speedups. They are within run-to-run/system jitter. CPU deltas are more useful here than sub-millisecond latency differences.

## Concurrent microbenchmarks

On the same host:

| Benchmark | Result |
| --- | ---: |
| Metrics concurrent HTTP record | 293.1 ns/op |
| Metrics concurrent mixed operations | 673.7 ns/op |
| Rate limiter concurrent, same client | 200.6 ns/op |
| Rate limiter concurrent, many clients | 403.9 ns/op |

Earlier single-path microbenchmarks:

| Feature | Result | Allocation |
| --- | ---: | ---: |
| baseline handler | 3.94 ns/op | 0 B/op |
| rate limiter | 90.32 ns/op | 0 B/op |
| metrics | 192.0 ns/op | 35 B/op |
| trace | 465.9 ns/op | 480 B/op |
| request ID | 471.1 ns/op | 464 B/op |
| access log | 1,124 ns/op | 288 B/op |
| full middleware hot path | 2,821 ns/op | 1,393 B/op |
| audit NoopSink | ~0.3 ns/op | 0 B/op |
| audit slog to discard | 1,104 ns/op | 253 B/op |

## Interpretation

At this tested load:

- metrics did not show a measurable CPU penalty relative to local run noise
- trace had small CPU overhead and was not a bottleneck
- access logging had the largest individually measured telemetry CPU overhead
- the all-on profile remained well below the starter 300 ms P95 SLO and sustained the requested arrival rate
- RSS differences were small, around 21-22 MiB

The access-log result includes slog formatting/output into the benchmark log file. Production collectors, container stdout plumbing, backpressure, disk/network shipping, and log indexing can increase the effective cost.

The all-on health endpoint does not emit audit events. Therefore the all-on profile does not include per-request audit-event cost. Audit cost is represented only by the audit microbenchmark here. A separate authenticated-event/load profile is required before making an end-to-end audit overhead claim.

## Not covered by this baseline

This matrix intentionally does not cover:

- Redis session/token round trips
- Keycloak/OIDC network traffic
- downstream API calls
- TLS termination
- ingress/service mesh
- Kubernetes CPU limits/throttling
- production log collectors
- audit service/Kafka sinks
- real user-context resolution
- token refresh storms
- login storms

Those need production-like integration/load tests.

## Reproduce

    make bench-feature
    make bench-contention

For the feature matrix:

    RATE=2000 DURATION=10s PRE_ALLOCATED_VUS=100 MAX_VUS=1000 make perf-feature-matrix

Raw matrix output is placed under benchmark-results/ and is intentionally ignored by Git.
