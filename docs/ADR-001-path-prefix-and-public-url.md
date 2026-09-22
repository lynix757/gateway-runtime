# ADR-001: Path Prefix and Public URL

Status: Accepted

## Context

The reusable BFF may be deployed at the origin root or below a reverse-proxy / ingress path such as `/portal` or `/bff`. OIDC callback URLs must also be deterministic and must not trust arbitrary request Host or forwarded headers.

## Decision

The BFF has two independent deployment settings:

- `BFF_HTTP_BASE_PATH`: optional external route prefix, e.g. `/portal`
- `BFF_HTTP_PUBLIC_URL`: authoritative externally visible origin, e.g. `https://example.com`

Application handlers are registered against root-relative internal paths such as `/auth/login` and `/api/me`. The HTTP composition root mounts the complete application router under `BFF_HTTP_BASE_PATH`.

OIDC redirect and logout URLs are constructed from the configured public URL plus the normalized base path. They are not derived from untrusted Host or X-Forwarded-* headers.

## Examples

Root mode:

```text
BFF_HTTP_BASE_PATH=
BFF_HTTP_PUBLIC_URL=https://example.com

/auth/login
/auth/callback
/api/me
```

Prefix mode:

```text
BFF_HTTP_BASE_PATH=/portal
BFF_HTTP_PUBLIC_URL=https://example.com

/portal/auth/login
/portal/auth/callback
/portal/api/me
```

The identity provider must allow the exact externally visible callback/logout URIs.
