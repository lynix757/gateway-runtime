# Envoy Gateway Reference Architecture

Version: 0.1.0
Status: Reference baseline

## Decision

Envoy Gateway with Kubernetes Gateway API is the primary edge architecture for REBFF.

Responsibility split:

```text
Envoy Gateway
= Kubernetes edge traffic, TLS, routing, load balancing, generic edge protection

REBFF
= browser OIDC/PKCE, server-side session, CSRF, user context, coarse application authorization

Application API
= business rules and final resource/data authorization

Specialized Gateways
= storage, streaming, and realtime workload-specific behavior
```

## Control path

Browser:

```text
Browser -> Envoy Gateway -> REBFF -> Application API
```

Mobile/native:

```text
Mobile -> Envoy Gateway -> Application API
```

Recommended Envoy edge functions:
- TLS termination;
- HTTPRoute/GRPCRoute routing;
- load balancing;
- trusted proxy/client IP handling;
- generic route rate limiting;
- outer request body limits;
- edge JWT validation for direct API/mobile paths;
- request/trace correlation;
- access logging and metrics;
- upstream timeout/retry/circuit protection where appropriate.

Recommended REBFF functions:
- OIDC Authorization Code + PKCE;
- opaque browser session;
- server-side access/refresh token handling;
- refresh de-duplication;
- CSRF protection;
- /api/me;
- coarse application permission;
- session-aware abuse protection;
- canonical audit production;
- trace propagation;
- downstream resilience for dependencies behind REBFF.

## Specialized data paths

Storage:

```text
Control: Client -> Envoy -> REBFF/App -> Storage Gateway -> presigned URL
Data:    Client ---------------------------------------> Object Storage
```

Streaming:

```text
Control: Client -> Envoy -> App -> Streaming Gateway -> signed playback credential
Data:    Client ---------------------------------------> CDN / Origin
```

WebSocket:

```text
Control: Client -> Envoy -> App -> short-lived connection token
Data:    Client -> Envoy -> WebSocket Gateway -> realtime bus
```

Large object/media payloads should not traverse REBFF by default.

## Overlap rule

A duplicated control is allowed only when it protects a different boundary.

Examples:
- Envoy rate limit protects the edge; REBFF auth throttling protects browser/session authentication flows.
- Envoy upstream breaker protects Envoy -> REBFF/App; REBFF breaker protects REBFF -> downstream dependency.
- Envoy request ID originates trusted correlation context; internal services propagate it rather than replacing it.

## Audit

Envoy access logs are infrastructure evidence.
Canonical audit events are security/business evidence.

They correlate through request_id and trace_id.

See docs/AUDIT_CONTRACT.md.
