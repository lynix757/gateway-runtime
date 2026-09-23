# ADR-002: Control Plane and Specialized Data Paths

Status: Accepted

## Context

The gateway runtime supports normal business/API traffic together with specialized workloads such as storage transfer, media streaming, and long-lived realtime connections. Routing all of these traffic shapes through REBFF would couple browser security and business control traffic to bandwidth-heavy or connection-heavy workloads with different scaling, lifecycle, and failure characteristics.

Direct client access to specialized endpoints still requires centralized authentication bootstrap, authorization, and scoped access credentials.

## Decision

Use this project-wide architecture principle:

> Business traffic passes through REBFF. High-bandwidth or long-lived traffic connects directly to a dedicated endpoint, while authorization and session bootstrap remain controlled through REBFF and the Application/Domain boundary.

The paths are separated as follows:

```text
Control path
Client -> REBFF -> Application/Domain API -> Capability Provider

Specialized data paths
Client -> Object Storage
Client -> Streaming Origin/CDN
Client -> WebSocket/Realtime Gateway
```

The control path owns:

- browser authentication and opaque session handling;
- browser-facing authorization and security controls;
- business request routing and response composition;
- application/domain resource authorization;
- issuing or obtaining short-lived, narrowly scoped credentials for a specialized endpoint;
- audit correlation and bootstrap metadata.

The specialized endpoint owns:

- bulk object transfer;
- media delivery;
- long-lived realtime connections;
- protocol-specific lifecycle, limits, and technical authorization enforcement.

A direct specialized path MUST NOT bypass authorization. Access must use a short-lived and narrowly scoped mechanism appropriate to the protocol, such as a presigned URL, playback token, or connection token. The specialized endpoint must validate that credential independently.

OAuth/OIDC tokens, durable application credentials, and unrestricted provider credentials must not be exposed to the client for direct specialized access.

Application/Domain services remain responsible for final business invariants and resource-level authorization. REBFF and specialized gateways do not become domain owners.

## Classification

| Traffic | Default path |
| --- | --- |
| Authentication, session, user context | Client -> REBFF |
| Business commands and queries | Client -> REBFF -> Application/Domain API |
| Storage authorization and presign bootstrap | Client -> REBFF/Application -> Storage Gateway |
| Object upload/download payload | Client -> Object Storage |
| Playback authorization and session bootstrap | Client -> REBFF/Application -> Streaming Gateway |
| HLS/DASH/media payload | Client -> CDN/Origin |
| Realtime connection bootstrap | Client -> REBFF/Application -> WebSocket Gateway |
| WebSocket connection and messages | Client -> WebSocket Gateway |

Exceptions require explicit architecture and security justification. An exception must document why the specialized path is unsuitable, its scaling and failure impact on REBFF, and how credential and resource authorization remain protected.

## Consequences

- REBFF remains focused on browser security and business control traffic.
- Specialized workloads can scale and fail independently.
- Bulk payloads do not consume REBFF bandwidth and connection capacity by default.
- Long-lived connections do not dictate REBFF deployment lifecycle or scaling.
- Credential bootstrap becomes an explicit interface between the control path and each specialized role.
- Observability must correlate control and data paths without logging full signed URLs, tokens, cookies, or credentials.
- Deployments require dedicated endpoints and protocol-appropriate credential validation for enabled specialized roles.
