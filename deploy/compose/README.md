# Gateway Runtime Docker Compose Test

This stack validates the same `gateway-runtime:v0.1.0` image in multiple runtime roles.

## Start

```bash
docker compose -f deploy/compose/docker-compose.test.yml up -d --build
```

## Role endpoints

```text
REBFF      http://localhost:28180
Storage    http://localhost:28182
Streaming  http://localhost:28183
WebSocket  http://localhost:28184
Mock       http://localhost:29012
Redis      localhost:26380
```

Check runtime identity:

```bash
curl http://localhost:28180/health/live
curl http://localhost:28182/runtime
curl http://localhost:28183/runtime
curl http://localhost:28184/runtime
```

Storage upload presign:

```bash
curl -sS -X POST http://localhost:28182/storage/upload \
  -H 'Content-Type: application/json' \
  -d '{"bucket":"assets","object_key":"P1/demo.pdf","content_type":"application/pdf","expires_in_seconds":300}'
```

Storage download:

```bash
curl -sS 'http://localhost:28182/storage/download?bucket=assets&object_key=P1/demo.pdf&expires_in_seconds=300'
```

Metadata:

```bash
curl -sS 'http://localhost:28182/storage/metadata?bucket=assets&object_key=P1/demo.pdf'
curl -sS 'http://localhost:28182/storage/metadata?bucket=assets&object_key=missing.bin'
```

Multipart:

```bash
curl -sS -X POST http://localhost:28182/storage/multipart/initiate \
  -H 'Content-Type: application/json' \
  -d '{"bucket":"assets","object_key":"large.bin","content_type":"application/octet-stream","expires_in_seconds":300}'

curl -sS -X POST http://localhost:28182/storage/multipart/part \
  -H 'Content-Type: application/json' \
  -d '{"bucket":"assets","object_key":"large.bin","upload_id":"upload-demo-001","part_number":1,"expires_in_seconds":300}'

curl -sS -X POST http://localhost:28182/storage/multipart/complete \
  -H 'Content-Type: application/json' \
  -d '{"bucket":"assets","object_key":"large.bin","upload_id":"upload-demo-001","parts":[{"part_number":1,"etag":"etag-1"}]}'
```

Streaming and WebSocket are currently provider placeholders. Their enabled routes intentionally return `503 provider_not_configured`; this verifies role and capability gating.

## Stop

```bash
docker compose -f deploy/compose/docker-compose.test.yml down -v
```
