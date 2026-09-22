# Feature overhead benchmarking

M15 adds explicit telemetry feature flags and an in-process benchmark suite so optional observability features can be measured independently.

## Feature flags

    BFF_METRICS_ENABLED=true
    BFF_AUDIT_ENABLED=true
    BFF_ACCESS_LOG_ENABLED=true
    BFF_TRACE_ENABLED=true

Defaults are enabled.

Behavior when disabled:

- metrics: no collector is created and /metrics is not registered
- audit: audit calls go to audit.NoopSink
- access log: request access-log middleware is not installed
- trace: traceparent parsing/generation and X-Trace-ID are disabled
- request ID remains enabled in all modes

The security/authentication semantics are unchanged by these flags.

## Microbenchmark

Run:

    make bench-feature

The benchmark reports ns/op and allocs/op for:

- baseline handler
- request ID
- trace
- HTTP metrics
- rate limiter
- access log
- representative full hot path
- audit noop sink
- audit slog sink writing to discard

These numbers are useful for relative comparisons on the same machine. They are not production capacity claims.

## End-to-end profiles

Use the telemetry flags to compare k6 runs with identical workload:

Baseline/minimal telemetry:

    BFF_METRICS_ENABLED=false
    BFF_AUDIT_ENABLED=false
    BFF_ACCESS_LOG_ENABLED=false
    BFF_TRACE_ENABLED=false

Then enable one feature at a time, keeping rate, duration, pod resources, Redis, and downstream behavior unchanged.

Suggested comparison sequence:

1. minimal telemetry
2. + metrics
3. + trace
4. + access log
5. + audit
6. all telemetry enabled

Record:

- achieved RPS
- P50/P95/P99
- CPU
- memory
- allocations/GC if profiled
- Redis latency
- log throughput
- rejection counters

## Interpretation

Trace ID generation should normally remain much cheaper than network I/O.

Access logging can become expensive when stdout/log collectors are saturated.

Metrics can show lock contention at very high RPS because the current implementation uses a shared mutex.

The rate limiter also has a shared per-process mutex around the bucket map.

These are benchmark targets, not assumed bottlenecks. Optimize only after measured evidence.

## Release gate

Store accepted benchmark results outside source-controlled hard-coded thresholds when runner hardware is not stable.

For a dedicated benchmark runner, compare new results to an approved baseline and fail only on material regression, for example:

- P95 latency regression > agreed threshold
- ns/op or allocs/op regression on hot-path benchmark
- sustainable RPS/pod regression
- CPU/request regression

Do not compare results from different CPU architectures or materially different runner resource limits as if they were equivalent.


## Concurrent contention baseline

Local M2 Max parallel benchmarks:

- metrics HTTP record: ~293 ns/op
- mixed metrics operations: ~674 ns/op
- rate limiter, same client: ~201 ns/op
- rate limiter, many clients: ~404 ns/op

Contention is measurable but remained sub-microsecond in this local benchmark. This is not a production scalability guarantee.

## Local end-to-end matrix

A validated 2,000 RPS local matrix is recorded in `docs/BENCHMARK_BASELINE_LOCAL.md`.

Key observation from that run:

- metrics: no measurable CPU increase above local noise
- trace: ~3.5% average BFF CPU increase
- access log: ~25.6% average BFF CPU increase
- telemetry all-on: ~31.6% average BFF CPU increase
- every profile sustained the requested 2,000 RPS with zero HTTP failures

The endpoint was `/health/live`, so audit events were not emitted during the matrix. Do not attribute the all-on delta to audit processing. Audit end-to-end overhead requires a separate event-producing profile.

The runner now fails if its benchmark port is already occupied and checks that each profile's BFF process is alive before generating load, preventing an orphan process from producing a false comparison.
