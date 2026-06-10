.PHONY: run build test test-integration test-e2e test-all lint docker-up docker-down migrate-up migrate-down kibana-bootstrap

COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)
KIBANA_URL ?= http://localhost:5601

run:
	go run ./main

build:
	go build -ldflags "$(LDFLAGS)" -o bin/server ./main

# Unit tests only. Pure Go, no Docker, < 30s. Mirrors unit-tests.yml in CI.
test:
	go test -short ./... -v -count=1 -race

# Integration tests. Spins up postgres + mailpit via testcontainers under
# the hood; the only prerequisite is a running Docker daemon. Mirrors
# integration-tests.yml in CI.
test-integration:
	go test ./tests/... -v -count=1 -timeout 10m -race

# Browser-driven E2E. Boots a self-contained docker-compose stack
# (postgres, redis, mailpit, app), runs Playwright, tears down. Mirrors
# e2e-tests.yml in CI.
test-e2e:
	bash e2e/scripts/run.sh

# Convenience: run every layer of the pyramid in order.
test-all: test test-integration test-e2e

lint:
	golangci-lint run ./...

docker-up:
	docker-compose up --build -d

docker-down:
	docker-compose down

migrate-up:
	migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path migrations -database "$(DATABASE_URL)" down

kibana-bootstrap:
	curl -sS -X POST "$(KIBANA_URL)/api/data_views/data_view" \
		-H "kbn-xsrf: true" \
		-H "Content-Type: application/json" \
		-d '{"data_view":{"title":"app-logs-*","name":"app-logs-*","timeFieldName":"timestamp"},"override":true}'
