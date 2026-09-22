# Integration environment

This environment validates the real OIDC and shared-state behavior of rebff.

## Components

- Keycloak 26.7.4 on http://localhost:18081
- Redis 8.2 on localhost:16379
- BFF replicas run in-process from the Go integration test on ports 18080 and 18082

Running the BFF on the host keeps the OIDC issuer identical for browser-facing URLs and server-side discovery/token verification. No /etc/hosts changes are required.

## Test realm

Realm: rebff

OIDC client:

- client_id: rebff-bff
- confidential client
- Authorization Code flow enabled
- PKCE is supplied by rebff
- Direct Access Grants disabled

Test users:

- alice / alice-password, role admin
- bob / bob-password, role viewer

The realm fixture adds a protocol mapper that exposes realm roles in a top-level roles claim because the reusable rebff OIDC adapter consumes that provider-neutral claim shape.

## Run

Full disposable integration run:

    make integration

Or manage the environment manually:

    make integration-up
    make integration-test
    make integration-down

The integration target tears containers and volumes down automatically after the test.

## Coverage

The Redis multi-replica test verifies:

1. a session written through replica A is readable through replica B
2. derived user context and permissions are consistent across replicas
3. deleting the authoritative session invalidates access on the other replica even if user-context cache was previously populated

The OIDC E2E test verifies:

1. GET /auth/login creates state, nonce, and PKCE
2. browser follows the Keycloak Authorization Code flow
3. Keycloak authenticates alice
4. callback exchanges the authorization code
5. ID token and nonce validation succeeds
6. server-side session/token state is written to Redis
7. /api/me returns identity, admin role, permission, and capability context
8. the same session works against a second BFF replica
9. logout requires the CSRF cookie/header and same-origin request
10. logout through replica B invalidates the shared session
11. /api/me through replica A returns 401 afterward

## Local memory

The current Colima environment exposes about 1.91 GiB to Docker. Keycloak was initially OOM-killed with exit 137 during schema initialization. The integration compose file therefore constrains the Keycloak JVM heap to:

    -Xms128m -Xmx512m

and uses local cache mode. This is a development/test setting, not a production Keycloak sizing recommendation.

## Security notes

The realm file contains integration-only credentials and client secret. Do not reuse them in production.

Direct password grant is intentionally disabled. The E2E test drives the browser Authorization Code flow instead of bypassing it with Resource Owner Password Credentials.
