# Architecture

## Logical boundaries

```text
Browser
  |
  +-- static / assets ----------------------> CDN / Web Server
  |
  +-- /auth/*, /api/* ---------------------> REBFF role
                                               |
                                               +--> IdP / Keycloak
                                               +--> Domain Services
                                               +--> Capability Providers
                                               |
                                               +--> Session Store
                                               +--> Token Store
                                               +--> Optional Cache
                                               +--> Audit Event Sink

Browser <----- short-lived credential -------- Application / Gateway role
Browser ---------------- specialized data ---> Storage / Streaming / WebSocket
```

## Runtime composition model

The reusable runtime is modeled as:

```text
Runtime
  |
  +-- Role
        |
        +-- Capabilities
```

A **role** defines a workload family, protocol behavior, security boundary, scaling model, and connection lifecycle.

A **capability** is a feature exposed within that role.

The same binary/container image may support multiple roles, while production deployments should normally activate one workload family per deployment and multiple related capabilities within that role.

### Initial roles

```text
rebff
storage
streaming
websocket
```

### Example capability matrix

| Role | Example capabilities |
| --- | --- |
| `rebff` | `oidc`, `session`, `api-proxy`, `user-context` |
| `storage` | `upload`, `download`, `multipart`, `presign`, `metadata` |
| `streaming` | `playback`, `manifest`, `token`, `origin-select` |
| `websocket` | `connect`, `publish`, `subscribe`, `presence` |

Example:

```yaml
role: storage
capabilities:
  - upload
  - download
  - multipart
  - presign
```

This is preferred over modeling every operation as a separate role such as `storage-upload` and `storage-download`.

## Deployment rule

Default production rule:

```text
one deployment
= one workload family / role
= multiple related capabilities
```

Examples:

```text
storage-gateway
  role=storage
  capabilities=upload,download,multipart,presign

websocket-gateway
  role=websocket
  capabilities=connect,publish,subscribe
```

Multiple roles in one deployment are allowed only when their protocol, scaling, lifecycle, security, and failure characteristics are sufficiently similar.

For example:

```text
storage(upload,download)          -> normal
storage + streaming              -> possible only with explicit justification
storage + websocket              -> discouraged
rebff + websocket                -> discouraged
```

The default remains separate deployments even when all deployments use the same image.

## Core boundaries

### REBFF role = browser/client security boundary

Owns browser-oriented authentication/session handling, coarse authorization, browser security controls, frontend context, and controlled API forwarding.

It MUST NOT absorb domain/business ownership.

### Storage role = object access gateway

Owns storage-specific access mechanics such as short-lived presigned operations, multipart coordination, metadata handling, and provider abstraction.

Business authorization for the underlying resource remains with the Application/Domain boundary.

Large object payloads should normally transfer directly between client and object storage rather than through the gateway.

### Streaming role = playback access gateway

Owns playback bootstrap, short-lived playback credentials, manifest/origin/CDN selection, and streaming-provider integration.

The media payload should normally bypass the gateway after authorization bootstrap.

### WebSocket role = realtime connection gateway

Owns long-lived connection handling, connection-token validation, heartbeat, connection lifecycle, routing, fanout, and realtime transport concerns.

Unlike storage and streaming bootstrap roles, the WebSocket role is intentionally part of the long-lived data path.

### Domain service = business boundary

Owns business invariants, transactions, entities, workflows, resource ownership, and final business/data authorization.

### Identity provider = authentication boundary

Owns credentials, authentication ceremonies, SSO, MFA, and identity assertions.

### Identity management = identity lifecycle boundary

Optional module initially. Administrative user lifecycle can later move into a dedicated service without changing browser session architecture.

### Audit = evidence boundary

Receives append-only events. Producers do not own the durable global audit model.

## Control plane and specialized data plane

The project-wide routing decision is recorded in
[`ADR-002: Control Plane and Specialized Data Paths`](ADR-002-control-plane-and-specialized-data-paths.md).

```text
Control path
Browser -> REBFF -> Application API

Specialized data paths
Browser -> Object Storage
Browser -> Streaming / CDN
Browser -> WebSocket Gateway
```

Storage and streaming normally use the control path for authorization/bootstrap and then move data directly over specialized paths.

WebSocket uses the control path for connection bootstrap, then the WebSocket gateway owns the long-lived connection.

## Composition rule

Shared Platform Core + Runtime Role + Enabled Capabilities + Replaceable Adapters.

The composition root selects roles, capabilities, and providers explicitly at startup.

Unsupported role/capability combinations must fail startup validation rather than silently enable fallback behavior.

## Dependency direction

```text
transport / role handlers
    |
application/use-case layer
    |
contracts/interfaces
    |
adapters/providers
```

Provider packages must not leak vendor-specific types into core contracts.

Role packages should depend on shared platform contracts, not directly on one another.


## Canonical audit architecture

All gateway roles and Application APIs use one canonical audit contract defined in `docs/AUDIT_CONTRACT.md`.

```text
Browser -> REBFF -------------------+
                                    |
Mobile -> Application API ----------+
                                    |
Storage Gateway --------------------+
Streaming Gateway ------------------+--> Canonical Audit Event
WebSocket Gateway ------------------+        |
                                             v
                                      Audit Collector / Sink
                                             |
                                             v
                                      Audit Store / SIEM
```

Authoritative audit evidence is generated server-side. Browser/mobile telemetry is supplemental only.

Canonical action names describe semantics such as `asset.read`, `authorization.allow`, and `storage.upload.request`; they do not encode the client channel.

REBFF authorization and Application API business authorization remain separate events. Related events share the same `request_id` and `trace_id`.

Browser, mobile, desktop, and service clients use the same event shape; origin is represented with `source.*` and `client.*` fields.
