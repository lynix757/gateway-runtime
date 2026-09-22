#!/usr/bin/env bash
set -euo pipefail

RATE="${RATE:-1000}"
DURATION="${DURATION:-15s}"
PORT="${PORT:-18090}"
RESULT_DIR="${RESULT_DIR:-benchmark-results/feature-matrix}"
TARGET_URL="http://host.docker.internal:${PORT}"
BINARY="$RESULT_DIR/rebff-bench"

mkdir -p "$RESULT_DIR"

if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "benchmark port $PORT is already in use" >&2
  lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >&2 || true
  exit 1
fi

go build -trimpath -o "$BINARY" ./cmd/bff

profiles=(
  "minimal:false:false:false:false"
  "metrics:true:false:false:false"
  "trace:false:false:false:true"
  "access-log:false:false:true:false"
  "all-on:true:true:true:true"
)

cleanup() {
  if [[ -n "${SAMPLE_PID:-}" ]] && kill -0 "$SAMPLE_PID" 2>/dev/null; then
    kill "$SAMPLE_PID" 2>/dev/null || true
    wait "$SAMPLE_PID" 2>/dev/null || true
  fi
  if [[ -n "${BFF_PID:-}" ]] && kill -0 "$BFF_PID" 2>/dev/null; then
    kill "$BFF_PID" 2>/dev/null || true
    wait "$BFF_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

for spec in "${profiles[@]}"; do
  IFS=: read -r name metrics audit access trace <<<"$spec"
  cleanup
  unset BFF_PID SAMPLE_PID || true

  log="$RESULT_DIR/${name}.bff.log"
  stats="$RESULT_DIR/${name}.process.csv"
  : > "$stats"

  BFF_HTTP_ADDR=":${PORT}" \
  BFF_HTTP_PUBLIC_URL="http://localhost:${PORT}" \
  BFF_STORE_BACKEND=memory \
  BFF_METRICS_ENABLED="$metrics" \
  BFF_AUDIT_ENABLED="$audit" \
  BFF_ACCESS_LOG_ENABLED="$access" \
  BFF_TRACE_ENABLED="$trace" \
  BFF_LIMIT_RATE_PER_SECOND=0 \
  BFF_LIMIT_AUTH_RATE_PER_SECOND=0 \
  "$BINARY" >"$log" 2>&1 &
  BFF_PID=$!
  sleep 0.1
  if ! kill -0 "$BFF_PID" 2>/dev/null; then
    echo "BFF profile $name failed to start" >&2
    cat "$log" >&2
    exit 1
  fi

  for _ in $(seq 1 50); do
    if curl -fsS "http://localhost:${PORT}/health/live" >/dev/null 2>&1; then
      break
    fi
    sleep 0.1
  done
  curl -fsS "http://localhost:${PORT}/health/live" >/dev/null
  if ! kill -0 "$BFF_PID" 2>/dev/null; then
    echo "BFF profile $name exited before benchmark" >&2
    cat "$log" >&2
    exit 1
  fi

  (
    while kill -0 "$BFF_PID" 2>/dev/null; do
      ps -p "$BFF_PID" -o %cpu=,rss= | awk 'NF==2 {gsub(/ /, "", $1); print $1 "," $2}' >> "$stats" || true
      sleep 0.2
    done
  ) &
  SAMPLE_PID=$!

  docker run --rm --entrypoint /usr/bin/k6 \
    -e TARGET_URL="$TARGET_URL" \
    -e PATH="/health/live" \
    -e RATE="$RATE" \
    -e DURATION="$DURATION" \
    -e PRE_ALLOCATED_VUS="${PRE_ALLOCATED_VUS:-100}" \
    -e MAX_VUS="${MAX_VUS:-1000}" \
    -v "$PWD/test/load/k6:/scripts:ro" \
    -v "$PWD/$RESULT_DIR:/results" \
    grafana/k6:2.2.0 run \
      --summary-export="/results/${name}.summary.json" \
      /scripts/api.js

  kill "$SAMPLE_PID" 2>/dev/null || true
  wait "$SAMPLE_PID" 2>/dev/null || true
  unset SAMPLE_PID
  kill "$BFF_PID" 2>/dev/null || true
  wait "$BFF_PID" 2>/dev/null || true
  unset BFF_PID
done

python3 scripts/summarize_feature_matrix.py "$RESULT_DIR"
