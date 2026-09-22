# Production deployment

## Runtime model

The project is designed around a reusable gateway runtime:

```text
single codebase
single binary
single container image
multiple runtime roles
multiple capabilities per role
separate deployments by workload family
```

Initial roles:

- `rebff`
- `storage`
- `streaming`
- `websocket`

Production default:

```text
one deployment
= one role / workload family
= multiple related capabilities
```

Example:

```yaml
role: storage
capabilities:
  - upload
  - download
  - multipart
  - presign
```

This allows the same image digest to be deployed independently as REBFF, storage gateway, streaming gateway, or WebSocket gateway.

## Multiple capabilities per deployment

Related capabilities should normally be grouped within the same role.

Example:

```text
storage-gateway
  role=storage
  capabilities=upload,download,multipart,presign
```

Upload and download do not need separate images or deployments unless independent scaling, security isolation, operational ownership, or failure isolation requires it.

## Multiple roles per deployment

Multiple roles MAY be enabled in the same deployment, but this is not the production default.

Before combining roles, evaluate:

- protocol compatibility;
- connection lifecycle;
- traffic shape;
- security boundary;
- scaling signal;
- resource profile;
- blast radius;
- deployment ownership.

Reasonable combinations require explicit justification.

Examples:

```text
storage(upload,download)       -> normal single-role deployment
storage + streaming           -> possible with explicit justification
storage + websocket           -> discouraged
rebff + websocket             -> discouraged
```

For local development, demos, and integration testing, a multi-role or all-in-one process may be useful.

## REBFF production baseline

The REBFF role should normally run multiple replicas behind an ingress or reverse proxy, with Redis as the shared auth/session/token/cache backend.

Recommended baseline:

- 3 REBFF replicas
- Redis HA/managed Redis
- explicit OIDC issuer and client configuration
- HTTPS public URL
- readiness/liveness/startup probes
- Prometheus scraping every replica
- immutable container image
- secrets provided by a secret manager or Kubernetes Secret integration

## Role-specific scaling

Each role should scale independently using workload-appropriate signals.

Examples:

| Role | Typical scaling signals |
| --- | --- |
| `rebff` | HTTP RPS, CPU, latency, session workload |
| `storage` | control RPS, multipart coordination, provider latency |
| `streaming` | playback bootstrap RPS, token issuance, provider latency |
| `websocket` | active connections, connection rate, messages/sec, memory |

Do not force all roles to use the same autoscaling policy merely because they use the same image.

## Container security

The supplied Dockerfile builds a statically linked Go binary and uses a scratch runtime image.

Runtime properties:

- non-root UID/GID 65532
- no shell/package manager
- CA certificates only
- read-only root filesystem supported
- no Linux capabilities required

The builder currently uses Go 1.27.1 and limits Go package build parallelism to reduce peak build memory.

## Kubernetes

Files in `deploy/k8s` provide the current REBFF baseline Deployment, Service, ConfigMap, Secret example, PodDisruptionBudget, and NetworkPolicy.

As role-based runtime support is implemented, deployments should reuse the same immutable image digest with different role/capability configuration.

Before production use:

- replace image references with immutable digests;
- source secrets from the secret-management platform;
- tune NetworkPolicy per role;
- tune CPU/memory independently per role;
- configure trusted proxy CIDRs for externally facing HTTP roles;
- expose only the ports/endpoints required by enabled capabilities.

## Probes

With `BFF_HTTP_BASE_PATH=/portal` for the current REBFF role:

- liveness: `/portal/health/live`
- readiness: `/portal/health/ready`
- metrics: `/portal/metrics`

Role-specific implementations should preserve common operational endpoints where practical.

Successful liveness/readiness probes may be suppressed from access logs; failed probes must remain visible.

## Metrics

Metrics are process-local. Prometheus should scrape every pod and aggregate by deployment/role.

Future role-aware metrics should include low-cardinality labels such as:

```text
role
capability
route
outcome
```

Avoid labels derived from raw resource IDs or unbounded user data.

## CI security gates

The GitLab pipeline runs:

- gofmt verification
- go vet
- unit tests
- race tests
- Trivy filesystem vulnerability/secret/misconfiguration scan
- CycloneDX SBOM generation with Syft
- static Linux binary build

The same image should pass common platform tests plus role/capability-specific tests before release.

## Secrets

Never commit real values for:

- OIDC client secrets;
- Redis credentials;
- provider API keys;
- signing credentials;
- storage credentials;
- streaming provider credentials;
- realtime broker credentials.

Secrets should be scoped to the minimum role/capability that requires them.


## Audit production model

All trusted server-side producers should emit the canonical schema from `docs/AUDIT_CONTRACT.md`.

```text
REBFF / Application API / Gateway role
        |
        v
Canonical Audit Contract
        |
        v
Audit Collector / durable buffer
        |
        v
Audit Store / SIEM / analytics
```

The authoritative source is the server-side component that made or enforced the security/business decision. Client telemetry may add platform or app-version context but must not be treated as the audit decision itself.

Production storage/index design should support correlation by `trace_id`, `request_id`, `actor.subject`, `action`, and target identity.
