.PHONY: example-asset-portal-verify fmt vet test test-race verify build docker-build integration-up integration-test integration-down integration perf-smoke perf-api capacity-example bench-feature bench-contention perf-feature-matrix

fmt:
	gofmt -w $$(find . -name '*.go' -type f)

vet:
	go vet ./...

test:
	go test ./...

test-race:
	CGO_ENABLED=1 go test -race ./...

verify:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test ./...

build:
	CGO_ENABLED=0 go build -trimpath -o bin/gateway-runtime ./cmd/gateway-runtime

docker-build:
	docker build -t gateway-runtime:local .


integration-up:
	docker compose -f deploy/integration/docker-compose.yml up -d redis keycloak

integration-test:
	@for i in $$(seq 1 40); do 		if curl -fsS http://localhost:18081/realms/rebff/.well-known/openid-configuration >/dev/null 2>&1; then break; fi; 		sleep 2; 	done
	curl -fsS http://localhost:18081/realms/rebff/.well-known/openid-configuration >/dev/null
	REBFF_INTEGRATION=1 go test -v ./test/integration

integration-down:
	docker compose -f deploy/integration/docker-compose.yml down -v

integration:
	@set -e; 	trap 'docker compose -f deploy/integration/docker-compose.yml down -v' EXIT; 	docker compose -f deploy/integration/docker-compose.yml up -d redis keycloak; 	for i in $$(seq 1 40); do 		if curl -fsS http://localhost:18081/realms/rebff/.well-known/openid-configuration >/dev/null 2>&1; then break; fi; 		sleep 2; 	done; 	curl -fsS http://localhost:18081/realms/rebff/.well-known/openid-configuration >/dev/null; 	REBFF_INTEGRATION=1 go test -v ./test/integration

perf-smoke:
	docker run --rm --entrypoint /usr/bin/k6 -v "$$(pwd)/test/load/k6:/scripts:ro" grafana/k6:2.2.0 run /scripts/smoke.js

perf-api:
	docker run --rm --entrypoint /usr/bin/k6 \
		-e TARGET_URL="$${TARGET_URL:-http://host.docker.internal:18080}" \
		-e PATH="$${PATH:-/api/me}" \
		-e RATE="$${RATE:-100}" \
		-e DURATION="$${DURATION:-2m}" \
		-e PRE_ALLOCATED_VUS="$${PRE_ALLOCATED_VUS:-50}" \
		-e MAX_VUS="$${MAX_VUS:-1000}" \
		-e SESSION_COOKIE="$${SESSION_COOKIE:-}" \
		-v "$$(pwd)/test/load/k6:/scripts:ro" \
		grafana/k6:2.2.0 run /scripts/api.js

capacity-example:
	go run ./cmd/capacity \
		-active-users 10000 \
		-requests-per-user-minute 2 \
		-measured-rps-per-replica 200 \
		-p95-ms 300 \
		-safe-inflight-per-replica 128 \
		-headroom 1.3 \
		-min-replicas 3


bench-feature:
	go test -run '^$$' -bench 'BenchmarkMiddlewareOverhead|BenchmarkAuditSink' -benchmem ./internal/httpx/middleware ./internal/audit


bench-contention:
	go test -run '^$$' -bench 'BenchmarkMetricsConcurrent|BenchmarkRateLimiterConcurrent' -benchmem ./internal/observability ./internal/httpx/middleware

perf-feature-matrix:
	bash scripts/run_feature_matrix.sh

example-asset-portal-verify:
	@! grep -R 'gateway-runtime/internal/' -n examples/asset-portal --include='*.go'
	cd examples/asset-portal && go mod tidy && go test ./...
