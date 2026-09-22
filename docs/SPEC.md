# Reusable Gateway Runtime Specification

Version: 0.2.1
Status: Baseline

## 1. Goal

Provide a reusable Go gateway runtime that can operate as multiple specialized runtime roles from the same codebase, binary, and container image while preserving strong boundaries between browser security, business logic, and specialized data-plane workloads.

The runtime model is:

```text
Runtime -> Role -> Capabilities
```

The initial roles are:

- `rebff`
- `storage`
- `streaming`
- `websocket`

Production should normally deploy one workload family per deployment while allowing multiple related capabilities within that role.

## 2. Role and capability model

A **role** represents a distinct workload family with meaningful differences in one or more of:

- protocol;
- connection lifecycle;
- security boundary;
- scaling model;
- traffic shape;
- failure model.

A **capability** represents an operation or feature within a role.

Examples:

```text
role=storage
capabilities=upload,download,multipart,presign
```

```text
role=websocket
capabilities=connect,publish,subscribe
```

The runtime MUST validate configured roles and capabilities at startup.

The runtime MUST NOT silently enable undeclared capabilities.

The runtime SHOULD support one role per production deployment by default.

Multiple roles in one deployment MAY be supported when explicitly configured and when their operational characteristics are compatible.

## 3. REBFF role responsibilities

The REBFF role MAY:

- start and complete OIDC Authorization Code + PKCE flows;
- keep browser sessions opaque;
- keep OAuth/OIDC tokens server-side;
- derive frontend-oriented user context;
- enforce browser-facing coarse authorization and policy checks;
- call domain/external services through typed adapters;
- emit security/audit events;
- expose health, readiness, and metrics endpoints.

The REBFF role MUST NOT become:

- the canonical business domain model owner;
- the canonical enterprise identity directory;
- the permanent organization-wide audit ledger;
- a static asset data plane by default;
- a bulk file proxy when a secure direct data plane is suitable;
- a media-streaming proxy by default;
- a long-lived WebSocket connection gateway by default.

## 4. Storage role

Example capabilities:

- `upload`
- `download`
- `multipart`
- `presign`
- `metadata`

The storage role MAY:

- generate short-lived presigned PUT/GET operations;
- coordinate multipart upload state;
- normalize storage-provider behavior;
- validate storage-specific technical policy;
- expose storage metadata operations.

The storage role MUST NOT decide domain/business ownership of the resource.

For object storage:

1. browser requests an authorized resource operation through the control path;
2. Application API validates business/resource authorization;
3. storage gateway creates a scoped short-lived storage credential or URL;
4. browser transfers data directly to object storage;
5. object storage validates the signed/scoped request.

Do not persist complete presigned URLs as durable data.
Do not log full presigned URLs.

## 5. Streaming role

Example capabilities:

- `playback`
- `manifest`
- `token`
- `origin-select`

The streaming role MAY:

- create playback sessions;
- issue short-lived playback credentials;
- select origin/CDN/provider;
- integrate with HLS/DASH/DRM/provider-specific controls.

The streaming role SHOULD NOT proxy the full media stream by default when direct CDN/origin delivery is suitable.

Business entitlement remains owned by the Application/Domain boundary.

## 6. WebSocket role

Example capabilities:

- `connect`
- `publish`
- `subscribe`
- `presence`

The WebSocket role MAY:

- validate short-lived connection credentials;
- accept protocol upgrade;
- maintain long-lived connections;
- manage heartbeat and connection lifecycle;
- bridge pub/sub or realtime event systems;
- enforce realtime transport/channel controls.

The WebSocket gateway is intentionally part of the long-lived realtime data path.

Application/domain services remain responsible for final business authorization semantics.

## 7. Authentication and session model

For the REBFF role, use OIDC Authorization Code + PKCE.

Browser receives only an opaque secure cookie:

`__Host-bff_session=<opaque-id>`

Cookie expectations:

- Secure
- HttpOnly
- Path=/
- SameSite appropriate to deployment
- no Domain attribute for `__Host-` semantics

Session data and token data are separate abstractions.

### SessionStore

Stores application session metadata, e.g.:

- session ID
- subject
- issuer
- created_at
- expires_at
- last_seen_at
- authentication context
- selected tenant/project context where applicable

### TokenStore

Stores server-side OAuth/OIDC token material keyed by session.

Refresh and token rotation belong to TokenManager, not HTTP handlers.

## 8. SSO across multiple REBFF applications

Each application/REBFF:

- has an independent OIDC client;
- has an independent local session;
- may have independent authorization rules;
- may require different MFA/step-up requirements.

SSO comes from the shared IdP session, not from sharing REBFF cookies.

## 9. `/api/me`

`GET /api/me` is REBFF-owned frontend context, not a raw IdP token dump.

Example semantic fields:

- subject/user ID
- display name
- username/email where permitted
- roles/permissions relevant to this application
- tenant/project context
- authentication assurance / step-up indicators
- UI feature/capability hints derived from policy

Cache only derived UserContext; never treat cache as session/token source of truth.

## 10. Provider model

Shared platform and role code depend on capability contracts, not vendors.

Examples:

- StorageSigner
- IdentityAdmin
- DomainAPI
- Notification
- PolicyEvaluator
- StreamingProvider
- RealtimeBroker

Provider selection is explicit configuration plus startup validation.

Allowed implementation styles:

- built-in provider;
- in-process adapter;
- remote service adapter.

Not allowed by default:

- arbitrary runtime-loaded code plugins;
- silent provider fallback.

## 11. Authorization boundary

The gateway runtime MAY perform transport/security/coarse-grained authorization.

Application/domain services MUST retain final business/data authorization, including:

- ownership;
- tenant/project/province scope;
- resource lifecycle state;
- workflow/business invariants.

Gateway allow does not replace Application API authorization.

## 12. Audit boundary

All roles and Application APIs use the canonical contract defined in `docs/AUDIT_CONTRACT.md`.

The gateway runtime is not the permanent enterprise audit store.

Authoritative audit events MUST be generated server-side. Browser/mobile telemetry MUST NOT be treated as authoritative audit evidence.

Canonical events SHOULD include schema version, event ID/time, source service/role, actor identity, client context, canonical action, target, outcome, request ID, trace ID, and relevant non-secret metadata.

Action names MUST describe security/domain semantics rather than implementation channels. Use names such as `asset.read`, `authorization.allow`, `authorization.deny`, `storage.upload.request`, and `websocket.connect`.

A single request MAY produce multiple events from different services. REBFF coarse authorization and Application API business authorization remain separate events and SHOULD share request/trace correlation.

Mobile and browser requests use the same audit schema; differences are represented in `source` and `client` fields.

## 13. Initial non-functional requirements

- secure defaults
- explicit role/capability boundaries
- one image reusable across deployments
- independent scaling by role
- low coupling
- replaceable adapters
- observable
- configuration validation at startup
- deterministic failure behavior
- no secrets/tokens/presigned URLs in logs
- no implicit cross-role dependencies
