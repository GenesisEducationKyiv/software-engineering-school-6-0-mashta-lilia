# Testing Guide

This project follows the **Practical Test Pyramid** ([Fowler](https://martinfowler.com/articles/practical-test-pyramid.html), [Scott](https://alisterscott.github.io/TestingPyramids.html)):

```
       /\         E2E   — browser drives the running stack (Playwright)
      /  \
     /----\       Integration — real Postgres + Mailpit, all HTTP endpoints
    /      \
   /--------\     Unit — fast, in-process, fakes/mocks
```

Each layer has its own one-command runner and a dedicated CI workflow under [`.github/workflows/`](.github/workflows/).

---

## Prerequisites

A fresh clone needs **only** these tools installed on the host:

| Layer        | git | docker | Go            | Node       |
|--------------|-----|--------|---------------|------------|
| Unit         | ✓   |        | ≥ go.mod ver  |            |
| Integration  | ✓   | ✓      | ≥ go.mod ver  |            |
| E2E          | ✓   | ✓      |               | ≥ 20.x     |

Docker daemon must be running for the integration and E2E layers. Everything else (Postgres, Redis, Mailpit, the app image) is started automatically.

---

## Running the tests

### Unit tests

```bash
make test
```

Equivalent to `go test -short ./... -race`. Skips anything guarded by `testing.Short()` (i.e. testcontainers-backed tests). Runs in seconds. No services required.

### Integration tests

```bash
make test-integration
```

Equivalent to `go test ./tests/... -race -timeout 10m`. Uses [`testcontainers-go`](https://golang.testcontainers.org/) to pull and start:

- `postgres:16-alpine` — applies the project migrations before the suite starts.
- `axllent/mailpit:latest` — captures the confirmation emails so tests can assert on subject and body via Mailpit's HTTP API.

The fake GitHub client is in-process (see [`tests/api/main_test.go`](tests/api/main_test.go)) — the GitHub client itself has its own unit tests under [`internal/client/github`](internal/client/github).

First run pulls the container images (~150 MB total). Subsequent runs use the local cache. Typical runtime: 1–3 min.

### E2E tests

```bash
make test-e2e
```

Wraps [`e2e/scripts/run.sh`](e2e/scripts/run.sh), which:

1. Boots [`e2e/docker-compose.e2e.yml`](e2e/docker-compose.e2e.yml) — Postgres, Redis, Mailpit, and the app built from the project [`Dockerfile`](Dockerfile).
2. Installs npm deps and the Playwright Chromium browser if missing.
3. Runs [`e2e/tests/*.spec.ts`](e2e/tests/) against the running stack on `http://localhost:8081`.
4. Tears the stack down (even on failure, via `trap`).

Typical runtime: 2–5 min including container boot.

### Everything

```bash
make test-all
```

Runs unit → integration → E2E in order. Stops on the first failing layer.

---

## CI mirror

Each command above corresponds 1:1 to a separate workflow on push and PR:

| Local command         | CI workflow                                                  |
|-----------------------|--------------------------------------------------------------|
| `make test`           | [`.github/workflows/unit-tests.yml`](.github/workflows/unit-tests.yml)               |
| `make test-integration` | [`.github/workflows/integration-tests.yml`](.github/workflows/integration-tests.yml) |
| `make test-e2e`       | [`.github/workflows/e2e-tests.yml`](.github/workflows/e2e-tests.yml)                 |

The Docker image build runs as its own [`build.yml`](.github/workflows/build.yml), in parallel with the test pipelines.

---

## What lives at each layer

| Layer       | Path                                                                | Examples                                                                                  |
|-------------|---------------------------------------------------------------------|-------------------------------------------------------------------------------------------|
| Unit        | `internal/**/*_test.go`                                             | Service rules, middleware behavior, handler error mapping, mailer templates, config parsing |
| Integration | [`tests/repository/`](tests/repository/) + [`tests/api/`](tests/api/) | DB schema + Chi router + middleware + every HTTP endpoint against real Postgres + Mailpit |
| E2E         | [`e2e/tests/`](e2e/tests/)                                          | Chromium opens the confirm and unsubscribe links a real user would receive in email      |

**No duplication across layers.** Branching logic (every validation variant, every status transition) stays in unit tests; integration tests prove *wiring and contract*; E2E proves the user journey works through real network + real browser.

---

## Troubleshooting

| Symptom                                                                | Fix                                                                                                       |
|------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------|
| `Cannot connect to the Docker daemon`                                  | Start Docker Desktop / `dockerd`. Required by integration + E2E.                                          |
| `port is already allocated` (5433, 6380, 1026, 8026, 8081)             | Stop whatever owns the port. E2E intentionally uses off-by-one host ports to coexist with `docker-up`.    |
| `pq: SSL is not enabled on the server`                                 | E2E sets `DB_SSLMODE=disable` in [`docker-compose.e2e.yml`](e2e/docker-compose.e2e.yml); make sure the override is in effect. |
| Playwright report missing in CI                                         | Failure artifacts upload to the `playwright-report` artifact on the e2e workflow.                          |
| `testcontainers` slow first run                                        | Image pulls (~150 MB). Subsequent runs reuse the local cache.                                              |
| Want to keep the e2e stack up after a failed run for manual poking     | Re-run with `PROJECT_NAME=grn-e2e docker compose -f e2e/docker-compose.e2e.yml up`; the script's `trap` runs only on its own exit. |
