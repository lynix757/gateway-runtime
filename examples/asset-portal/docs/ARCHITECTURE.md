# Asset Portal on REBFF

This example is a separate Go module that consumes only the public `gateway-runtime/core` package.

It deliberately does not import `gateway-runtime/internal/*`.

Flow:

Browser -> REBFF core -> protected application route -> Asset API / Storage API

The application owns:
- domain-specific routes
- permission names
- downstream service configuration
- response composition
- asset validation
- upload object-key derivation

REBFF owns:
- OIDC/PKCE
- server-side session/token lifecycle
- CSRF
- authorization enforcement
- audit/metrics/trace
- rate/concurrency protection
- outbound token forwarding
- timeout/retry/metrics/circuit/bulkhead behavior
- health/readiness
