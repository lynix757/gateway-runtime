# Storage Targets + Access Policy + MinIO

This compose stack demonstrates target-level storage authorization behind Envoy.

## Architecture

```text
Client
  |
  v
Envoy :38080
  |
  +-- Host: storage.localhost
          |
          v
    gateway-runtime
      role=storage
          |
          +--> Storage Target Registry
          |      asset          -> minio-main / assets
          |      archive        -> minio-main / archive (read-only)
          |      audit-evidence -> minio-main / audit-evidence (read-only)
          |
          +--> Access Policy Service
          |      subject + action + target -> allow / deny
          |
          +--> MinIO Signer
                 |
                 v
               MinIO

Object bytes use the presigned URL and go directly between the client and MinIO.
```

## Start

The `rebff` service builds `gateway-runtime:v0.1.0` once. The storage, streaming and websocket services reuse the same image.

```bash
docker compose -f deploy/compose/docker-compose.minio.yml up -d --build
```

Endpoints:

```text
Envoy ingress     http://127.0.0.1:38080
Envoy admin       http://127.0.0.1:39901
MinIO S3          http://minio.localhost:39000
MinIO console     http://127.0.0.1:39001
```

## Target registry

The compose example configures:

```json
{
  "asset": {
    "provider": "minio-main",
    "bucket": "assets"
  },
  "archive": {
    "provider": "minio-main",
    "bucket": "archive",
    "read_only": true
  },
  "audit-evidence": {
    "provider": "minio-main",
    "bucket": "audit-evidence",
    "read_only": true
  }
}
```

When `STORAGE_TARGETS_JSON` is configured, the client sends `target`, not an arbitrary bucket. The runtime resolves the configured bucket server-side.

## Access policy

Canonical actions:

```text
storage.upload
storage.download
storage.metadata
storage.multipart
```

The test policy contains:

```text
alice
  role=asset-user
  allow asset upload/download/metadata/multipart

auditor
  role=auditor
  allow audit-evidence download/metadata

admin
  role=archive-admin
  allow archive download/metadata
```

Example ALLOW:

```bash
curl -sS -X POST http://127.0.0.1:38080/storage/upload \
  -H 'Host: storage.localhost' \
  -H 'X-Auth-Subject: alice' \
  -H 'Content-Type: application/json' \
  -d '{"target":"asset","object_key":"P1/demo.txt","content_type":"text/plain","expires_in_seconds":300}'
```

Example DENY:

```bash
curl -i \
  -H 'Host: storage.localhost' \
  -H 'X-Auth-Subject: auditor' \
  'http://127.0.0.1:38080/storage/download?target=asset&object_key=P1/demo.txt'
```

Expected: HTTP 403 `access_denied`.

A write to a read-only target is rejected before the signer is called:

```bash
curl -i -X POST http://127.0.0.1:38080/storage/upload \
  -H 'Host: storage.localhost' \
  -H 'X-Auth-Subject: admin' \
  -H 'Content-Type: application/json' \
  -d '{"target":"archive","object_key":"2026/demo.txt"}'
```

Expected: HTTP 403 `storage_target_read_only`.

If the subject is absent the target-mode API returns HTTP 401. If the policy dependency fails, authorization fails closed with HTTP 503.

## Production identity boundary

`X-Auth-Subject` is only a compose-test trusted-upstream identity input. Production must derive the actor from a validated identity/token boundary and Envoy must strip or overwrite spoofable client identity headers.

Storage Access Policy is target-level authorization only. Application/domain services remain responsible for resource-level rules such as tenant ownership, project membership, object ownership and business state.

## Multi-provider status

The target model already carries a `provider` field, but v0.1.0 currently wires one storage signer instance. Therefore:

```text
multiple buckets on one provider   supported
target-level access policy         supported
multiple provider instances        next step
```

The next extension is a Storage Provider Registry mapping provider names such as `minio-main`, `minio-archive` and `s3-backup` to separate signer/provider clients.

## Multi-provider registry

For backend API use cases, callers should send a logical storage target and object key. The runtime resolves the target to a provider and bucket; backend business code does not need to know the physical storage endpoint.

Example:

~~~text
asset-api -> target=asset -> minio-main/assets
audit-api -> target=audit-evidence -> minio-main/audit-evidence
archive-api -> target=archive -> minio-archive/archive
~~~

Configure provider signer endpoints with:

~~~dotenv
STORAGE_PROVIDERS_JSON={"minio-main":{"signer_base_url":"http://minio-signer-main:19002"},"minio-archive":{"signer_base_url":"http://minio-signer-archive:19002"}}
~~~

Then bind targets to providers:

~~~dotenv
STORAGE_TARGETS_JSON={"asset":{"provider":"minio-main","bucket":"assets"},"audit-evidence":{"provider":"minio-main","bucket":"audit-evidence","read_only":true},"archive":{"provider":"minio-archive","bucket":"archive","read_only":true}}
~~~

When STORAGE_PROVIDERS_JSON is configured, every configured target provider must exist in the registry. Missing providers fail closed. The legacy STORAGE_SIGNER_BASE_URL remains supported for single-provider deployments.

Backend/domain services remain authoritative for resource-level and business authorization. The storage runtime only enforces runtime/target-level access and resolves logical targets to infrastructure providers.

## Backend service contract

For service-to-service use, prefer the intent/reference API:

~~~text
POST /storage/intents/upload
POST /storage/intents/download
GET  /storage/references/metadata
~~~

These endpoints use logical targets. A backend service should never need to know the bucket, signer endpoint, MinIO instance, or S3 provider selected behind that target.

Existing /storage/upload, /storage/download, /storage/presign, /storage/metadata, and multipart routes remain available for backward compatibility.

## Upload verification

Backend services can confirm a direct upload with:

~~~text
POST /storage/intents/verify
~~~

REBFF compares authoritative object metadata against optional expected size, content type, and checksums and returns a logical StorageReference plus verified=true/false.

REBFF does not persist PENDING, UPLOADING, READY, REJECTED, or QUARANTINED state. Those states belong to the backend/domain service.

## Service identity and actor context

Backend service calls may send:

~~~text
X-Service-Identity: asset-api
X-Actor-Subject: user-001
X-Tenant-ID: tenant-01
X-Project-ID: project-22
X-Request-ID: req-123
X-Trace-ID: trace-456
~~~

The access-policy request continues to use actor as subject and receives service_id and correlation/scoping fields in its context map. X-Auth-Subject remains a compatibility fallback.

The compose setup treats identity headers as trusted-upstream test data only. In production, Envoy or another authenticated upstream must strip externally supplied identity headers and overwrite them from validated user/workload identity.

## Workload JWT authentication

Optional production-style service authentication:

~~~dotenv
STORAGE_WORKLOAD_AUTH_MODE=oidc
STORAGE_WORKLOAD_AUTH_REQUIRED=true
STORAGE_WORKLOAD_OIDC_ISSUER=https://sso.example.com/realms/platform
STORAGE_WORKLOAD_OIDC_AUDIENCE=storage-runtime
~~~

Backend services send a bearer token in Authorization. The runtime verifies it using OIDC discovery/JWKS and derives service identity from verified claims. X-Service-Identity is not trusted when token verification is active.

The existing compose example may leave workload auth disabled for local testing.
