# Asset Portal Example

This is a separate Go module that uses REBFF as its application core.

## 1. Verify the consumer boundary

From the REBFF repository root:

```bash
make example-asset-portal-verify
```

The target fails if the application imports `gateway-runtime/internal/*`.

## 2. Start Keycloak and Redis integration dependencies

From the REBFF repository root:

```bash
make integration-up
```

Keycloak:

- URL: http://localhost:18081
- realm: `rebff`
- client: `rebff-bff`
- client secret: `rebff-dev-secret`
- Alice: `alice` / `alice-password` / role `admin`
- Bob: `bob` / `bob-password` / role `viewer`

## 3. Start the mock domain services

Open a second terminal:

```bash
cd examples/asset-portal
go run ./cmd/mock-services
```

This starts:

- Asset API on `:19001`
- Storage signing API on `:19002`

Both mock services require a Bearer token. This proves that REBFF forwards the server-side access token to downstream services.

## 4. Configure the Asset Portal

Open a third terminal:

```bash
cd examples/asset-portal
set -a
source .env.example
set +a
```

For this example, change the store to Redis so the runtime matches the integration environment:

```bash
export BFF_STORE_BACKEND=redis
export BFF_REDIS_URL=redis://localhost:16379/0
export BFF_REDIS_PREFIX=asset-portal
```

## 5. Start Asset Portal

```bash
go run ./cmd/asset-portal
```

The BFF listens on:

```text
http://localhost:18083
```

## 6. Login

Open:

```text
http://localhost:18083/auth/login?return_to=/api/me
```

Login as Alice:

```text
alice
alice-password
```

After the callback, REBFF creates a server-side session and uses the HTTP-development cookie `bff_session`.

For HTTPS production deployments the cookie automatically changes back to the hardened `__Host-bff_session` form.

## 7. Check user context

Open:

```text
http://localhost:18083/api/me
```

Alice receives the `admin` role, which the example maps to:

```text
asset.read
storage.upload
```

## 8. Read an asset

Using the same authenticated browser session:

```text
GET http://localhost:18083/api/assets/A001
```

Flow:

```text
Browser
  -> REBFF session validation
  -> permission asset.read
  -> server-side access token
  -> Asset API :19001
  -> normalized JSON response
```

Expected demo response:

```json
{
  "id": "A001",
  "name": "Demo Notebook",
  "serial": "SN-DEMO-001",
  "status": "active",
  "project_id": "PRJ-001"
}
```

## 9. Request an upload URL

The browser first reads the CSRF cookie `bff_csrf`, then sends the same value in the `X-CSRF-Token` header.

```http
POST /api/assets/A001/attachments/upload-url
Content-Type: application/json
X-CSRF-Token: <value from bff_csrf cookie>

{
  "filename": "invoice.pdf",
  "content_type": "application/pdf"
}
```

REBFF enforces:

- authenticated session
- CSRF
- `storage.upload` permission
- request-body limit
- content-type allowlist
- filename validation
- server-derived object key
- outbound timeout, metrics, bulkhead, and circuit breaker

The Storage API receives the access token server-to-server and returns a presigned URL.

## 10. Observe the platform features

Metrics:

```text
http://localhost:18083/metrics
```

Also inspect application logs for:

- request ID
- trace ID
- authorization decisions
- audit events
- outbound service metrics

## 11. Logout

Use the CSRF cookie/header and call:

```http
POST /auth/logout
```

REBFF deletes the server-side session and token state.

## 12. Stop dependencies

From the REBFF root:

```bash
make integration-down
```

## Application code vs REBFF core

Asset Portal implements only:

- application route definitions
- permissions (`asset.read`, `storage.upload`)
- Asset API adapter
- Storage API adapter
- domain-specific validation
- response composition

REBFF supplies:

- OIDC Authorization Code + PKCE
- browser session and token lifecycle
- Redis shared state
- CSRF
- permission enforcement
- `/api/me`
- server-to-server access-token forwarding
- refresh-token handling
- audit / metrics / trace
- rate and concurrency protection
- outbound retry / timeout / circuit / bulkhead
- health / readiness
- graceful shutdown
