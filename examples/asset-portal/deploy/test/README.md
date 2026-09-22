# Asset Portal Docker Compose Test Stack

This stack runs the full local demo as five processes:

- `keycloak` - OIDC provider on host port 18081
- `redis` - REBFF session/token store on host port 16379
- `bff` - Asset Portal REBFF on host port 18083
- `mock-asset-api` - mock application API on host port 19001
- `mock-storage-api` - mock presign API on host port 19002

The Keycloak container uses `keycloak.test` as its issuer. Docker resolves that name through a network alias. The curl harness maps the same name to host loopback with `--resolve`, so no host `/etc/hosts` change is required.

## Start

From `examples/asset-portal`:

```bash
docker compose -f deploy/test/docker-compose.yml up -d --build
```

Check status:

```bash
docker compose -f deploy/test/docker-compose.yml ps
curl http://localhost:18083/health/ready
```

## Tail BFF and mock-service logs

```bash
./deploy/test/tail-logs.sh
```

Or separately:

```bash
docker compose -f deploy/test/docker-compose.yml logs -f bff
docker compose -f deploy/test/docker-compose.yml logs -f mock-asset-api
docker compose -f deploy/test/docker-compose.yml logs -f mock-storage-api
```

The mock Asset API logs:

- incoming request
- request ID
- resolved identity
- allowed province scope
- requested asset province
- allow/deny decision

The mock Storage API logs:

- incoming request
- request ID
- derived object key
- presign decision

## Run the curl flow

```bash
./deploy/test/test-flow.sh
```

The script:

1. waits for REBFF readiness
2. starts login through REBFF
3. submits Alice credentials to Keycloak
4. calls `/api/me`
5. calls `A001` and expects HTTP 200
6. calls `A002` and expects HTTP 403
7. requests a presigned upload URL with CSRF
8. prints REBFF authorization and outbound metrics

Expected authorization behavior:

```text
Alice allowed_provinces = [50]

A001 province=50
REBFF asset.read -> ALLOW
Mock Asset API   -> ALLOW
HTTP 200

A002 province=10
REBFF asset.read -> ALLOW
Mock Asset API   -> DENY
HTTP 403
```

Correlation IDs used by the script:

```text
demo-me
demo-a001
demo-a002
demo-presign
```

These IDs are visible in both the BFF and mock-service logs.

## Stop

```bash
docker compose -f deploy/test/docker-compose.yml down -v
```

## Demo-only security note

The mock Asset API decodes the JWT payload only to demonstrate identity-to-data-scope behavior. It does not cryptographically validate the JWT.

A production resource server must validate at least:

- JWT signature / JWKS
- issuer
- audience
- expiry / not-before
- required claims

before constructing its application `Principal`.
