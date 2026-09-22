# gateway-runtime

Reusable gateway runtime written in Go. Envoy Gateway + Kubernetes Gateway API is the primary edge architecture.

Container image and Go module use `gateway-runtime`. `rebff` is one runtime role alongside storage, streaming, and websocket.

## Runtime roles

The same image can run as:

- `rebff` — browser OIDC/session/CSRF/API control path;
- `storage` — storage control-plane capability host;
- `streaming` — playback/streaming control-plane capability host;
- `websocket` — realtime connection capability host.

Configuration:

```bash
GATEWAY_ROLE=rebff
GATEWAY_CAPABILITIES=oidc,session,token-refresh,csrf,user-context,api-proxy,coarse-authz,audit,trace-propagation
```

Specialized roles start independently and register only explicitly enabled capability routes. Until a provider is configured, provider-backed operations fail closed with HTTP 503 `provider_not_configured`.

Production default remains one workload family per deployment. Multiple specialized roles may share one process; mixing `rebff` with specialized roles is rejected in v0.1.0.

### Storage role provider

The storage role can bind to a remote signer service through:

```bash
GATEWAY_ROLE=storage
GATEWAY_CAPABILITIES=upload,download,presign

STORAGE_SIGNER_BASE_URL=http://storage-signer:8080
STORAGE_SIGNER_TIMEOUT=5s
STORAGE_PRESIGN_DEFAULT_TTL=15m
STORAGE_PRESIGN_MAX_TTL=1h
```

Production target mode adds a server-side target registry and mandatory target-level access policy:

```bash
STORAGE_TARGETS_JSON='{"asset":{"provider":"minio-main","bucket":"assets"},"archive":{"provider":"minio-main","bucket":"archive","read_only":true}}'
STORAGE_ACCESS_POLICY_URL=http://access-policy:19100
STORAGE_ACCESS_POLICY_TIMEOUT=3s
STORAGE_SUBJECT_HEADER=X-Auth-Subject
```

When `STORAGE_TARGETS_JSON` is configured, clients send a logical `target` instead of choosing an arbitrary bucket. The runtime resolves `target -> provider/bucket` server-side and evaluates `subject + action + target` before calling the storage signer. Policy dependency failure is fail-closed.

Canonical storage actions are `storage.upload`, `storage.download`, `storage.metadata`, and `storage.multipart`. Target-level authorization does not replace application/domain resource authorization.

Legacy bucket mode remains available only when no target registry is configured.

Implemented routes:

```text
POST /storage/upload
GET  /storage/download
POST /storage/presign
```

The remote provider contract is:

```text
POST /api/storage/presign/put
POST /api/storage/presign/get
```

The runtime validates target/bucket resolution, object key, authorization policy, and TTL before calling the signer. Presigned responses use `Cache-Control: no-store`. If a provider-backed capability is enabled but no signer is configured, readiness and operations fail closed.

Multipart is implemented in v0.1.0 through the same remote signer provider:

```text
POST /storage/multipart/initiate
POST /storage/multipart/part
POST /storage/multipart/complete
POST /storage/multipart/abort
```

Provider contract:

```text
POST /api/storage/multipart/initiate
POST /api/storage/multipart/part
POST /api/storage/multipart/complete
POST /api/storage/multipart/abort
```

Multipart data still bypasses REBFF/Gateway Runtime:

```text
Control: Client -> Envoy -> App/Storage Gateway -> signer
Data:    Client -------------------------------> Object Storage
```

The runtime enforces object-key safety, presign TTL, part number range 1..10000, non-empty ETags, no duplicate completed part numbers, and a maximum of 10000 completed parts.

Metadata is implemented in v0.1.0:

```text
GET /storage/metadata?target=<target>&object_key=<key>
```

Provider contract:

```text
POST /api/storage/metadata
```

Canonical metadata fields include:

```text
exists
bucket
object_key
size
content_type
etag
version_id
last_modified
checksums
metadata
```

`exists=false` is a normal successful metadata response; provider transport/errors remain separate failures.


## Purpose

`rebff` is a reusable browser-facing security/control-plane boundary for web applications. It is intentionally not a domain service and should not become a permanent organization-wide audit or identity-lifecycle owner.

The default architecture is:

- Static frontend: CDN / web server, bypasses BFF
- Browser auth/API control plane: BFF
- Business workflows: domain services
- Authentication / SSO / MFA: Keycloak or another OIDC IdP
- Identity administration: optional capability, extractable later
- Audit evidence: separate append-only evidence boundary
- Bulk object data plane: direct browser-to-object-storage using short-lived presigned URLs after BFF authorization

## Approved baseline

- Go
- OIDC Authorization Code + PKCE
- Opaque host-only BFF session cookie: `__Host-bff_session`
- Separate SessionStore and TokenStore
- `/api/me` owned by BFF and returns derived frontend UserContext
- Optional cache; no automatic generic HTTP response cache
- `/api/me` cache is scoped, short-lived, and bypassable on cache failure
- Multiple BFFs share IdP SSO but keep independent application-local sessions
- Local logout and global IdP logout are distinct operations
- Capability providers are explicit and validated at startup
- Built-in, in-process, or remote providers are allowed behind contracts
- No runtime dynamic plugin loading and no silent provider fallback
- Presigned URLs are never persisted as durable application data and should not be logged in full

## Initial routes

- `GET /health/live`
- `GET /health/ready`
- `GET /metrics`
- `GET /auth/login`
- `GET /auth/callback`
- `POST /auth/logout`
- `GET /api/me`

## Repository layout

```text
cmd/gateway-runtime/               executable
internal/app/          composition root
internal/auth/         OIDC/login/logout boundary
internal/session/      opaque application sessions
internal/token/        server-side token storage/refresh
internal/usercontext/  /api/me derived context
internal/cache/        optional cache contract
internal/capability/   capability/provider registry
internal/audit/        audit event emission contract
internal/httpx/        HTTP helpers
docs/                  architecture/spec/ADRs
```

See `docs/SPEC.md` and `docs/ARCHITECTURE.md`.


## Runtime configuration

Minimal local development:

```bash
export BFF_HTTP_ADDR=:8080
export BFF_HTTP_PUBLIC_URL=http://localhost:8080
export BFF_HTTP_BASE_PATH=
go run ./cmd/gateway-runtime
```

OIDC-enabled deployment:

```bash
export BFF_HTTP_PUBLIC_URL=https://app.example.com
export BFF_HTTP_BASE_PATH=/portal

export BFF_OIDC_ISSUER=https://sso.example.com/realms/demo
export BFF_OIDC_CLIENT_ID=portal-bff
export BFF_OIDC_CLIENT_SECRET='replace-me'
export BFF_OIDC_SCOPES=openid,profile,email

go run ./cmd/gateway-runtime
```

The externally registered OIDC callback in prefix mode is:

```text
https://app.example.com/portal/auth/callback
```

The OIDC adapter uses provider discovery and remains vendor-neutral. Keycloak, Entra ID, Okta, Auth0, and other standards-compliant OIDC providers can be integrated through the same contract.

Security properties in the current M1 baseline:

- Authorization Code flow with PKCE S256
- cryptographically random state and nonce
- one-time state consumption
- ID token signature/audience/issuer/time validation through `go-oidc`
- explicit ID token nonce validation
- local-only `return_to`
- OAuth/OIDC tokens remain server-side
- opaque browser session cookie
- local and provider/global logout paths

Current development storage is in-memory. Redis-backed session/token/flow stores are the next productionization milestone.


## Store backend

Development default:

```bash
BFF_STORE_BACKEND=memory
```

Production Redis example:

```bash
BFF_STORE_BACKEND=redis
BFF_REDIS_URL=redis://:password@redis:6379/0
BFF_REDIS_PREFIX=rebff
```

Redis key namespaces:

```text
rebff:auth:flow:<state>
rebff:session:<session-id>
rebff:token:<session-id>
```

Auth flow state is consumed atomically. Session and token records expire with the BFF session TTL. Redis mode performs a startup connectivity check and is also included in `/health/ready`; loss of Redis availability makes readiness return HTTP 503.


## Security middleware

Current middleware baseline:

- request/correlation ID via `X-Request-ID`
- security response headers
- CSRF double-submit token plus same-origin Origin/Referer validation for unsafe methods
- explicit trusted-proxy CIDRs for `X-Forwarded-For`
- access logging with sensitive URL query redaction
- helper redaction for Authorization/Cookie/Set-Cookie headers and token/signature query parameters

Trusted proxy example:

```bash
BFF_TRUSTED_PROXY_CIDRS=10.0.0.0/8,192.168.0.0/16
```

If this setting is empty, forwarded client-IP headers are not trusted.

For browser state-changing requests, first obtain the `__Host-bff_csrf` cookie from a safe request, then send its value back in:

```text
X-CSRF-Token: <cookie-value>
```

The configured public origin must also match the request Origin/Referer. OIDC callback remains a GET flow protected independently by OIDC state, PKCE, and nonce.


## User context

`GET /api/me` returns a BFF-owned derived user context rather than raw identity-provider claims.

Example shape:

```json
{
  "sub": "user-1",
  "display_name": "Alice",
  "email": "alice@example.com",
  "roles": ["admin"],
  "permissions": ["asset.read"],
  "capabilities": ["asset-admin"],
  "auth_level": "authenticated"
}
```

The identity snapshot needed for this view is stored with the BFF session; OAuth/OIDC tokens remain in the separate token store.

Derived user context is cached for 60 seconds by default:

- memory backend -> in-memory cache
- Redis backend -> `rebff:cache:userctx:<session-id>`

Cache failure degrades to recomputation. The cache is not a session or authorization source of truth and is invalidated on logout. HTTP responses from `/api/me` use `Cache-Control: no-store`.

The current role-to-permission/capability policy is an explicit static policy placeholder with empty mappings at startup. Project/domain authorization rules should be supplied through a policy capability/provider rather than hard-coded into the reusable BFF core.


## Policy provider

Authorization policy is exposed through a provider-neutral `policy.Evaluator` contract. The same evaluator is used to derive `/api/me` permissions/capabilities and can protect application routes through `RequirePermission`.

Current provider:

```bash
BFF_POLICY_PROVIDER=static
BFF_POLICY_ROLE_PERMISSIONS='{"admin":["asset.read","asset.write"],"viewer":["asset.read"]}'
BFF_POLICY_ROLE_CAPABILITIES='{"admin":["asset-admin"]}'
```

Example semantics:

```text
OIDC roles
   |
   v
Policy Evaluator
   +--> permissions
   +--> capabilities
   |
   +--> /api/me
   +--> route authorization
```

Static policy is intended for simple deployments and template bootstrapping. Future OPA or remote authorization providers can implement the same evaluator contract without changing the BFF auth/session model.


## Observability and audit

Current baseline includes:

- Prometheus-compatible `GET /metrics`
- request ID correlation through `X-Request-ID`
- W3C `traceparent` trace-ID extraction/generation
- `X-Trace-ID` response propagation
- structured HTTP access logs containing request ID and trace ID
- audit sink contract plus structured slog sink
- audit events for login success/failure, logout success/failure, and authorization denied
- metrics for total requests, HTTP 5xx, auth success/failure, and authorization denied

Current metric names:

```text
rebff_http_requests_total
rebff_http_errors_total
rebff_auth_success_total
rebff_auth_fail_total
rebff_authorization_denied_total
```

Audit events carry:

```text
event_id
occurred_at
actor
action
target
outcome
correlation_id
trace_id
attributes
```

Tokens, session IDs, cookies, authorization headers, OIDC authorization codes, and presigned signatures must not be emitted as audit attributes.

The current trace support establishes propagation/correlation without requiring an OpenTelemetry SDK. OTLP exporting can be added later behind the observability boundary without changing auth/session/policy contracts.


## Outbound and external services

The BFF now includes a reusable typed outbound HTTP foundation for domain and capability adapters.

Current behavior:

- fixed absolute upstream base URL
- per-client timeout
- server-side bearer-token forwarding from the BFF token store
- request ID and trace ID propagation
- JSON request/response helpers
- bounded response-body size
- normalized upstream errors
- bounded retry for idempotent GET/HEAD/OPTIONS requests only
- retries only on transport failure or HTTP 502/503/504
- POST requests are not retried automatically

Example normalized error classes:

```text
bad_request
unauthorized
forbidden
not_found
conflict
rate_limited
unavailable
upstream_error
```

A sample typed DomainAPI client and remote `StorageSigner` adapter are included. The storage adapter obtains short-lived presigned operations from a trusted server-side service; the browser remains the object data plane.

No new browser-facing domain or storage route is exposed by this milestone. Application-specific routes must add explicit permission checks before invoking these adapters.


## Application route composition

Application-specific routes can now be registered through an optional route registrar without changing the core router.

Example composition:

```text
GET /api/managed-items/{id}
    -> session required
    -> permission: managed-item.read
    -> typed DomainAPI call
    -> normalized upstream response/error

POST /api/storage/upload-url
    -> CSRF middleware
    -> session required
    -> permission: storage.upload
    -> request size / JSON validation
    -> object-key scope validation
    -> presign TTL validation
    -> StorageSigner.PresignPut
    -> Cache-Control: no-store
```

Routes are capability-gated: if the required DomainAPI or StorageSigner is not configured, the corresponding route is not registered and returns 404.

The upload example requires an explicit bucket and one or more allowed object-key prefixes. Path traversal, absolute keys, backslash paths, keys outside the approved prefixes, and excessive presign TTLs are rejected before the signer is called.

Upstream errors are mapped to stable browser-facing HTTP status codes instead of passing provider-specific error bodies through directly.

This milestone provides the reusable composition pattern and tested example handlers. Deployment-specific upstream URLs, object-key ownership rules, and business resource models remain application configuration/extensions rather than BFF-core assumptions.


## Integration environment

A disposable Keycloak + Redis integration environment is available under `deploy/integration`.

Run the full suite with:

```bash
make integration
```

It starts Keycloak 26.7.4 and Redis, executes the real browser-style Authorization Code + PKCE flow against two BFF replicas sharing Redis, then removes the containers and volumes.

The integration suite verifies:

- OIDC discovery and authorization-code exchange
- PKCE, state, nonce, and ID-token verification
- Keycloak role mapping into derived permissions/capabilities
- shared session continuity across BFF replicas
- `/api/me` consistency across replicas
- CSRF-protected logout
- logout/session deletion propagating across replicas
- cached user context never bypassing authoritative session validation

See `docs/INTEGRATION.md` for fixture credentials, ports, coverage, and local-memory notes.


## Resilience semantics

The BFF distinguishes authentication state from dependency failure:

- missing or expired session -> 401
- session backend unavailable -> 503
- permission check backend failure -> 503
- invalid login return_to -> 400
- login flow-store/provider operational failure -> 503
- logout store failure -> 503
- Redis readiness failure -> /health/ready 503 while /health/live remains 200

OIDC discovery at startup is bounded to 5 seconds. Outbound retries are context-aware, bounded, and limited to idempotent methods.

See `docs/RESILIENCE.md` for failure-mode details and tested behavior.


## Token and session lifecycle

Browser session lifetime is independent from OAuth access-token lifetime.

Defaults:

- `BFF_SESSION_MAX_LIFETIME=8h`
- `BFF_SESSION_IDLE_TIMEOUT=30m`
- `BFF_SESSION_TOUCH_INTERVAL=1m`

Access tokens approaching expiry are refreshed server-side. Refresh-token rotation is persisted without extending the absolute BFF session lifetime.

Concurrent refresh is collapsed through a refresh lock:

- memory mode: `token.MemoryLocker`
- multi-replica Redis mode: `redisstore.RefreshLocker`

After acquiring the lock the token manager re-reads token state, so another replica that already completed refresh prevents duplicate refresh.

A failed/revoked refresh invalidates both token state and the BFF session, forcing re-authentication rather than falling back to an expired access token.

See `docs/TOKEN_LIFECYCLE.md` for the detailed lifecycle and integration coverage.


## Operational abuse protection

Per-replica operational safeguards are enabled by configuration:

- global request body cap
- global concurrency admission limit
- general per-client-IP token-bucket rate limit
- stricter `/auth/login` rate limit
- bounded rate-limit bucket map
- outbound bulkhead wrapper
- outbound circuit breaker wrapper

Operational health/metrics endpoints bypass rate and concurrency admission controls so saturation does not cause false liveness failures.

New metrics include:

```text
rebff_http_rejected_total{reason}
rebff_http_inflight_requests
rebff_outbound_rejected_total{service,reason}
```

Inbound rate/concurrency controls and outbound circuit/bulkhead state are intentionally per replica. Use ingress/WAF/API-gateway controls when a globally enforced client quota is required across all replicas.

See `docs/OPERATIONAL_SECURITY.md` and `deploy/k8s/prometheusrule.example.yaml`.


## SLO and capacity engineering

The repository now includes a starter SLO/capacity baseline:

- 99.9% availability starter target
- P95 latency starter target below 300 ms
- SLI recording rules and multi-window error-budget burn alerts
- CPU-based HPA baseline (3-20 replicas, 65% target)
- k6 smoke and constant-arrival-rate load profiles
- a capacity calculator that sizes replica floor from measured throughput and inflight limits

Run:

```bash
make capacity-example
make perf-smoke
RATE=100 DURATION=5m SESSION_COOKIE=<opaque-session-id> make perf-api
```

Capacity is intentionally derived from measured RPS/replica, P95 latency, and safe inflight rather than from registered-user count alone.

See `docs/SLO_CAPACITY.md`, `deploy/k8s/hpa.yaml`, and `deploy/k8s/slo/prometheusrule.yaml`.


## Local feature-overhead baseline

The feature matrix runner can compare telemetry profiles at identical arrival rate while sampling BFF CPU/RSS.

```bash
make bench-feature
make bench-contention
RATE=2000 DURATION=10s make perf-feature-matrix
```

The current M2 Max engineering baseline sustained 2,000 RPS with zero failures across minimal, metrics, trace, access-log, and all-on telemetry profiles. Access logging showed substantially more CPU overhead than trace or metrics in this local test.

See `docs/BENCHMARK_BASELINE_LOCAL.md` for exact results and limitations.

### Backend API storage intent

Backend services should use logical storage intents rather than physical buckets or endpoints:

~~~json
{
  "target": "asset",
  "operation": "upload",
  "object_key": "tenant-01/project-22/assets/A-100/01KXYZ.pdf",
  "content_type": "application/pdf"
}
~~~

Business authorization such as tenant membership, project ownership, asset state, and workflow rules stays in the backend API. The storage runtime applies target-level authorization and resolves:

~~~text
target -> Storage Target Registry -> Storage Provider Registry -> signer/provider
~~~

Multi-provider configuration:

~~~dotenv
STORAGE_PROVIDERS_JSON={"minio-main":{"signer_base_url":"http://minio-signer-main:19002"},"minio-archive":{"signer_base_url":"http://minio-signer-archive:19002"}}
STORAGE_TARGETS_JSON={"asset":{"provider":"minio-main","bucket":"assets"},"archive":{"provider":"minio-archive","bucket":"archive","read_only":true}}
~~~

STORAGE_SIGNER_BASE_URL is retained as the backward-compatible single-provider configuration.

### Backend storage contract

Backend services should treat storage as a logical capability, not as a MinIO/S3 SDK dependency.

Recommended lifecycle:

~~~text
Backend API
  -> business/resource authorization
  -> POST /storage/intents/upload
  -> receive UploadGrant
  -> client uploads directly to object storage
  -> GET /storage/references/metadata
  -> persist StorageReference with the domain record
~~~

Upload intent:

~~~http
POST /storage/intents/upload
X-Auth-Subject: user-001
Content-Type: application/json

{
  "target": "asset",
  "object_key": "tenant-01/project-22/assets/A-100/file.pdf",
  "content_type": "application/pdf",
  "expires_in_seconds": 300
}
~~~

Upload grant:

~~~json
{
  "url": "https://storage.example/...",
  "method": "PUT",
  "expires_at": "2026-09-22T15:00:00Z",
  "headers": {
    "Content-Type": "application/pdf"
  }
}
~~~

Download intent uses POST /storage/intents/download with target, object_key, and optional expires_in_seconds.

A stable storage reference is obtained from:

~~~http
GET /storage/references/metadata?target=asset&object_key=tenant-01/project-22/assets/A-100/file.pdf
~~~

Example reference:

~~~json
{
  "target": "asset",
  "object_key": "tenant-01/project-22/assets/A-100/file.pdf",
  "version_id": "v1",
  "etag": "abc123",
  "size": 528331,
  "content_type": "application/pdf",
  "checksums": {
    "sha256": "..."
  }
}
~~~

Do not persist presigned URLs. Persist the logical StorageReference; request a fresh upload/download grant when transport access is needed.

The backend API remains authoritative for tenant, project, resource ownership, workflow state, and business authorization. REBFF remains authoritative only for runtime/target policy and storage target/provider resolution.

### Upload confirmation lifecycle

REBFF does not own business upload state. The backend service remains the source of truth for attachment/document lifecycle.

Recommended flow:

~~~text
Backend API
  PENDING
    |
    | request upload intent
    v
  UPLOADING
    |
    | client uploads directly to object storage
    |
    | POST /storage/intents/verify
    v
  verified=true  -> READY
  verified=false -> REJECTED / QUARANTINED / retry
~~~

Verification request:

~~~http
POST /storage/intents/verify
X-Auth-Subject: user-001
Content-Type: application/json

{
  "target": "asset",
  "object_key": "tenant-01/project-22/assets/A-100/file.pdf",
  "expected_size": 528331,
  "expected_content_type": "application/pdf",
  "expected_checksums": {
    "sha256": "..."
  }
}
~~~

Successful verification:

~~~json
{
  "verified": true,
  "reference": {
    "target": "asset",
    "object_key": "tenant-01/project-22/assets/A-100/file.pdf",
    "etag": "abc123",
    "size": 528331,
    "content_type": "application/pdf"
  }
}
~~~

A verification mismatch is returned as HTTP 200 with verified=false because it is a business/validation result rather than a transport failure. The response includes mismatch entries for fields such as object existence, size, content type, and checksums.

Provider/network errors remain 5xx responses. Invalid request contracts remain 4xx responses.

The verify endpoint currently uses storage.metadata authorization because verification is implemented through authoritative provider metadata lookup.

### Service identity and actor context

Backend-to-REBFF storage calls distinguish the calling workload from the user being represented.

Recommended trusted context:

~~~text
X-Service-Identity: asset-api
X-Actor-Subject: user-001
X-Tenant-ID: tenant-01
X-Project-ID: project-22
X-Request-ID: req-123
X-Trace-ID: trace-456
~~~

Semantics:

~~~text
service = workload making the call
actor   = user or principal the workload is acting for
tenant/project = business scoping context
request/trace   = correlation context
~~~

Storage authorization keeps the actor as the policy subject for backward compatibility and adds service_id, tenant_id, project_id, request_id, and trace_id to policy context.

Legacy X-Auth-Subject remains accepted when X-Actor-Subject is absent.

Security boundary: these headers are trusted context only after a trusted ingress/BFF/workload-authentication layer has stripped client-supplied values and injected verified values. Do not expose the storage runtime directly to untrusted clients while trusting these headers. Production deployments should derive service identity from mTLS/SPIFFE, signed internal credentials, or another authenticated workload identity mechanism; headers are transport carriers, not proof by themselves.

### Service-to-service workload authentication

Storage runtime can verify signed workload JWTs through OIDC discovery/JWKS before trusting service identity.

Recommended production configuration:

~~~dotenv
STORAGE_WORKLOAD_AUTH_MODE=oidc
STORAGE_WORKLOAD_AUTH_REQUIRED=true
STORAGE_WORKLOAD_OIDC_ISSUER=https://sso.example.com/realms/platform
STORAGE_WORKLOAD_OIDC_AUDIENCE=storage-runtime
~~~

Backend service request:

~~~http
POST /storage/intents/upload
Authorization: Bearer <signed-workload-token>
X-Actor-Subject: user-001
X-Tenant-ID: tenant-01
X-Project-ID: project-22
X-Request-ID: req-123
X-Trace-ID: trace-456
~~~

When OIDC workload authentication is configured, REBFF validates token signature, issuer, audience, and expiry through the issuer discovery/JWKS endpoint. Service identity is derived from token claims in this order:

~~~text
client_id -> azp -> sub
~~~

Any caller-supplied X-Service-Identity value is ignored after successful token verification.

The actor remains separate from workload identity:

~~~text
workload token -> service = asset-api
X-Actor-Subject -> actor = user-001
~~~

This supports delegated user actions without confusing the backend service identity with the human/principal identity.

Missing or invalid workload tokens return HTTP 401. If workload authentication is configured as required but no verifier is available, readiness fails and requests fail closed.

For local/dev compatibility, workload authentication may remain disabled. In that mode X-Service-Identity may still be carried by a trusted upstream, but it is not cryptographic proof.
