.PHONY: run build test test-integration lint docker-up docker-down migrate-up migrate-down

COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)

run:
	go run ./main

build:
	go build -ldflags "$(LDFLAGS)" -o bin/server ./main

test:
	go test -short ./... -v -count=1

test-integration:
	go test ./... -v -count=1 -timeout 180s

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
