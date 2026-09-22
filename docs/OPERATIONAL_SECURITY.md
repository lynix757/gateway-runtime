# Operational security and abuse protection

M13 adds per-replica safeguards for inbound abuse and outbound dependency saturation.

## Inbound limits

Default values:

    BFF_LIMIT_MAX_REQUEST_BODY_BYTES=1048576
    BFF_LIMIT_MAX_CONCURRENCY=256
    BFF_LIMIT_RATE_PER_SECOND=50
    BFF_LIMIT_RATE_BURST=100
    BFF_LIMIT_RATE_MAX_ENTRIES=10000
    BFF_LIMIT_AUTH_RATE_PER_SECOND=0.2
    BFF_LIMIT_AUTH_RATE_BURST=10

A value of 0 disables the corresponding limiter.

### Global request-body limit

Request bodies are capped before application handlers run.

The default is 1 MiB because this BFF is designed for JSON/control-plane traffic. Large object transfer should use presigned URLs and bypass the BFF data plane.

### Concurrency limit

Each BFF replica admits at most the configured number of concurrent non-operational requests.

When full:

- response: 503
- Retry-After: 1
- metric: rebff_http_rejected_total{reason="concurrency"}

Health and metrics endpoints bypass this limiter so Kubernetes does not restart a healthy process merely because application traffic is saturated.

### General rate limit

The default token bucket is 50 requests/second with burst 100 per derived client IP per replica.

Client IP is derived only from X-Forwarded-For when the immediate proxy is inside BFF_TRUSTED_PROXY_CIDRS.

The limiter keeps at most 10,000 source-IP buckets. Additional previously unseen addresses share a bounded overflow bucket instead of causing unbounded memory growth.

Rejected traffic returns 429 with Retry-After: 1.

### Login-specific rate limit

/auth/login has an additional limiter:

- 0.2 requests/second
- burst 10
- metric reason: auth_rate_limit

This protects creation of OIDC flow state, nonce, and PKCE transactions.

The BFF limiter is intentionally per replica. With N replicas behind a load balancer, a single source may receive an effective aggregate allowance greater than one replica's configured rate.

If a globally enforced limit is required, enforce it at an ingress/API gateway/WAF or implement a distributed limiter. Credential brute-force controls, MFA, and account lockout remain IdP responsibilities.

## Operational endpoint bypass

The following suffixes bypass inbound rate and concurrency admission controls:

- /health/live
- /health/ready
- /metrics

They still pass through request ID, trace ID, security headers, and access logging.

## Outbound bulkhead

outbound.ProtectedClient can wrap any outbound.JSONDoer.

MaxConcurrent limits concurrent calls to one upstream service within a replica.

When the bulkhead is full:

- call fails with outbound unavailable
- upstream is not called
- the circuit breaker is not penalized
- metric: rebff_outbound_rejected_total{service="<service>",reason="bulkhead"}

Bulkheads are per service and per BFF replica.

## Circuit breaker

ProtectedClient opens after a configurable consecutive failure threshold.

Default values when unspecified:

- failure threshold: 5
- open duration: 15s

Only availability/transport failures count toward the circuit. Normal 4xx responses do not trip it. Client cancellation also does not trip it.

After the open interval, only one half-open probe is admitted. A successful probe closes the circuit; a failed probe reopens it.

Circuit rejection metric:

    rebff_outbound_rejected_total{service="<service>",reason="circuit"}

Circuit state is intentionally process-local. This avoids a shared failure-control dependency and allows replicas to recover independently.

## Metrics

New metrics:

    rebff_http_rejected_total{reason}
    rebff_http_inflight_requests
    rebff_outbound_rejected_total{service,reason}

Do not add client IP, session ID, user ID, request ID, trace ID, or raw URL as metric labels.

## Example alerts

deploy/k8s/prometheusrule.example.yaml contains baseline alerts for:

- sustained 5xx ratio
- general rate-limit rejection
- login-specific rate-limit rejection
- concurrency saturation
- outbound bulkhead/circuit rejection
- high P95 latency

Thresholds are examples and must be tuned from real traffic and SLOs.

## Multi-replica model

Inbound limiters, concurrency semaphores, bulkheads, and circuit breakers are process-local.

Prometheus aggregates their metrics across replicas. Redis is not used to synchronize these controls.

This preserves failure isolation and keeps Redis focused on authoritative shared auth/session/token state.
