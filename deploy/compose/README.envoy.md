# Gateway Runtime behind Envoy — Docker Compose

This compose stack puts Envoy in front of all gateway runtime roles and exposes only one application ingress port.

## Architecture

```text
Client
  |
  | :28080
  v
Envoy
  |-- rebff.localhost   -> gateway-runtime role=rebff
  |-- storage.localhost -> gateway-runtime role=storage
  |-- stream.localhost  -> gateway-runtime role=streaming
  \-- ws.localhost      -> gateway-runtime role=websocket

Internal only:
  Redis
  Mock Storage Provider
```

Envoy admin is exposed only on loopback at `127.0.0.1:29901`.

## Start

```bash
docker compose -f deploy/compose/docker-compose.envoy.yml up -d --build
```

## Test vhosts

```bash
curl http://rebff.localhost:28080/health/live
curl http://storage.localhost:28080/runtime
curl http://stream.localhost:28080/runtime
curl http://ws.localhost:28080/runtime
```

If the local resolver does not automatically map `*.localhost` to loopback, use the Host header:

```bash
curl -H 'Host: storage.localhost' http://127.0.0.1:28080/runtime
```

## Storage through Envoy

```bash
curl -sS -X POST http://storage.localhost:28080/storage/upload \
  -H 'Content-Type: application/json' \
  -d '{"bucket":"assets","object_key":"P1/demo.pdf","content_type":"application/pdf","expires_in_seconds":300}'
```

Metadata:

```bash
curl -sS 'http://storage.localhost:28080/storage/metadata?bucket=assets&object_key=P1/demo.pdf'
```

## Unknown host

Envoy rejects unknown vhosts rather than routing to a default backend:

```bash
curl -i -H 'Host: unknown.localhost' http://127.0.0.1:28080/
```

Expected: HTTP 404.

## Envoy admin

```bash
curl http://127.0.0.1:29901/ready
curl http://127.0.0.1:29901/clusters
curl http://127.0.0.1:29901/config_dump
```

## Stop

```bash
docker compose -f deploy/compose/docker-compose.envoy.yml down -v
```

For Docker Compose this uses Envoy Proxy directly. In Kubernetes, Envoy Gateway + Gateway API becomes the control plane for equivalent listener/HTTPRoute behavior.
