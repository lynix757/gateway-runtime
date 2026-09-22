# Resilience and failure semantics

M11 defines how rebff behaves when dependencies fail. The goal is fail-closed behavior without misreporting infrastructure failures as user authorization failures.

## Redis or shared-state outage

When Redis is the configured store backend:

- /health/live remains 200 while the process is alive.
- /health/ready returns 503 when Redis health fails.
- /api/me returns 401 only when the session is absent or expired.
- /api/me returns 503 when the session backend is unavailable.
- permission middleware returns 503 when session lookup fails because of a backend error.
- logout does not report success when shared token/session deletion fails.

This distinction prevents dependency failures from being mistaken for invalid credentials.

## OIDC / Keycloak

OIDC discovery is required at startup when OIDC is configured and is bounded by a 5-second startup timeout.

If discovery cannot complete, the BFF fails startup rather than serving a partially configured authentication surface.

After startup, the IdP is not part of the steady-state readiness check because existing authenticated traffic may still be serviceable from Redis and downstream services. Authentication endpoints may fail independently if the IdP becomes unavailable.

The callback currently fails closed for exchange/verification errors. A future provider-error taxonomy can distinguish invalid authorization codes from transient IdP transport failures more precisely.

## Outbound services

Outbound HTTP clients use explicit per-client timeouts.

Retries are bounded and only enabled for GET, HEAD, and OPTIONS. POST and other mutation requests are never retried automatically.

Retry backoff is context-aware:

- request cancellation stops retry immediately
- context deadline stops retry immediately
- 502/503/504 may be retried only while the request context remains active
- response bodies are bounded

## Session and token expiry

The authoritative session store determines authentication validity. Derived user-context cache cannot extend a deleted or expired session.

The token manager rejects expired access tokens before forwarding them to downstream services.

## Tested failure cases

Automated tests cover:

- readiness failure while liveness remains healthy
- Redis/session backend error -> 503
- missing/expired session -> 401
- flow-store failure during login -> 503
- invalid login return_to -> 400
- slow outbound service -> bounded timeout
- request cancellation -> retry backoff stops promptly
- shared session deletion across replicas
- cached /api/me context cannot bypass deleted session
- real Redis outage simulation using a closed client
- Authorization Code + PKCE flow through Keycloak
- race detector across the full Go test suite

Run:

    make verify
    make integration
    go test -race ./...
