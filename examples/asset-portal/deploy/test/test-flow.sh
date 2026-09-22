#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:18083}"
KEYCLOAK_RESOLVE=(--resolve "keycloak.test:18081:127.0.0.1")
KEYCLOAK_DISCOVERY="http://keycloak.test:18081/realms/rebff/.well-known/openid-configuration"
COOKIE_JAR="$(mktemp)"
LOGIN_HTML="$(mktemp)"
LOGIN_RESULT="$(mktemp)"
A001_BODY="$(mktemp)"
A002_BODY="$(mktemp)"
PRESIGN_BODY="$(mktemp)"
trap 'rm -f "$COOKIE_JAR" "$LOGIN_HTML" "$LOGIN_RESULT" "$A001_BODY" "$A002_BODY" "$PRESIGN_BODY"' EXIT

echo "==> wait for Keycloak"
for _ in $(seq 1 90); do
  if curl -fsS "${KEYCLOAK_RESOLVE[@]}" "$KEYCLOAK_DISCOVERY" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
curl -fsS "${KEYCLOAK_RESOLVE[@]}" "$KEYCLOAK_DISCOVERY" >/dev/null

echo "==> wait for BFF"
for _ in $(seq 1 90); do
  if curl -fsS "${BASE_URL}/health/ready" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
curl -fsS "${BASE_URL}/health/ready"
echo

echo "==> login page via BFF -> Keycloak"
curl -fsS -L "${KEYCLOAK_RESOLVE[@]}"   -c "$COOKIE_JAR" -b "$COOKIE_JAR"   "${BASE_URL}/auth/login?return_to=/api/me"   -o "$LOGIN_HTML"

ACTION="$(python3 - "$LOGIN_HTML" <<'PY'
import html
import pathlib
import re
import sys

body = pathlib.Path(sys.argv[1]).read_text()
match = re.search(r'(?s)<form[^>]*id="kc-form-login"[^>]*action="([^"]+)"', body)
if not match:
    raise SystemExit("Keycloak login form action not found")
print(html.unescape(match.group(1)))
PY
)"

echo "==> submit Alice credentials"
curl -fsS -L "${KEYCLOAK_RESOLVE[@]}"   -c "$COOKIE_JAR" -b "$COOKIE_JAR"   -H 'Content-Type: application/x-www-form-urlencoded'   --data-urlencode 'username=alice'   --data-urlencode 'password=alice-password'   "$ACTION"   -o "$LOGIN_RESULT"

echo "==> /api/me"
curl -fsS -b "$COOKIE_JAR"   -H 'X-Request-ID: demo-me'   -H 'X-Trace-ID: demo-trace-me'   "${BASE_URL}/api/me"
echo

echo "==> A001: REBFF asset.read ALLOW + app province scope ALLOW"
A001_STATUS="$(curl -sS -o "$A001_BODY" -w '%{http_code}'   -b "$COOKIE_JAR"   -H 'X-Request-ID: demo-a001'   -H 'X-Trace-ID: demo-trace-a001'   "${BASE_URL}/api/assets/A001")"
cat "$A001_BODY"
echo
echo "status=${A001_STATUS}"
test "$A001_STATUS" = "200"

echo "==> A002: REBFF asset.read ALLOW + app province scope DENY"
A002_STATUS="$(curl -sS -o "$A002_BODY" -w '%{http_code}'   -b "$COOKIE_JAR"   -H 'X-Request-ID: demo-a002'   -H 'X-Trace-ID: demo-trace-a002'   "${BASE_URL}/api/assets/A002")"
cat "$A002_BODY"
echo
echo "status=${A002_STATUS}"
test "$A002_STATUS" = "403"

CSRF="$(awk '$6 == "bff_csrf" {print $7}' "$COOKIE_JAR" | tail -1)"
if [ -z "$CSRF" ]; then
  echo "bff_csrf cookie not found" >&2
  exit 1
fi

echo "==> presign upload URL"
PRESIGN_STATUS="$(curl -sS -o "$PRESIGN_BODY" -w '%{http_code}'   -b "$COOKIE_JAR"   -H 'X-Request-ID: demo-presign'   -H 'X-Trace-ID: demo-trace-presign'   -H "X-CSRF-Token: $CSRF"   -H "Origin: $BASE_URL"   -H 'Content-Type: application/json'   --data '{"filename":"invoice.pdf","content_type":"application/pdf"}'   "${BASE_URL}/api/assets/A001/attachments/upload-url")"
cat "$PRESIGN_BODY"
echo
echo "status=${PRESIGN_STATUS}"
test "$PRESIGN_STATUS" = "200"

echo "==> metrics: authorization + outbound"
curl -fsS "${BASE_URL}/metrics" | grep -E 'rebff_(authorization_decisions_total|outbound_requests_total)' || true

echo "==> demo flow PASS"
