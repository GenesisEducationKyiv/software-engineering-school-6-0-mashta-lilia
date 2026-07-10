# GitHub Release Notification API

> A Go service that lets users subscribe to email notifications about new GitHub repository releases. Implements a full subscription lifecycle (subscribe, confirm via email, unsubscribe) with a background poller that checks for new releases and notifies subscribers.

Built with Go, PostgreSQL, Redis, Chi, and SMTP.

## Bonus Points Achieved

- **Message Broker (RabbitMQ)** -- Notification commands are published to a durable RabbitMQ exchange and consumed asynchronously by the notification service, decoupling the monolith from the notifier's availability. The consumer's decode/dispatch logic is unit-tested (ack/drop/requeue policy, validation, trace propagation). See [Message Broker](#message-broker-rabbitmq).
- **Redis Caching** -- GitHub API responses are cached using a cache-aside (decorator) pattern with a 10-minute TTL. If Redis is unavailable, the system gracefully degrades to direct API calls with zero downtime.
- **Integration Tests** -- 12 database integration tests run against a real PostgreSQL instance via `testcontainers-go`, verifying migrations, partial unique indexes, FK constraints, cascade deletes, and database triggers.
- **Prometheus Metrics** -- `/metrics` endpoint exposes `http_requests_total` (counter by method/path/status), `http_request_duration_seconds` (histogram), and `http_requests_in_flight` (gauge). Uses Chi route patterns to avoid high-cardinality labels from dynamic path segments like tokens.

## Quick Start

**Prerequisites**: Docker and Docker Compose.

```bash
# 1. Clone and configure
git clone <repo-url>
cd github-subscription-api
cp .env.example .env
# Edit .env: set GITHUB_TOKEN and API_KEY (SMTP credentials belong to the
# notification service; docker-compose points it at the bundled mailpit)

# 2. Start everything (Postgres x2 + Redis + RabbitMQ + mailpit + notifier + app)
docker compose up --build
```

The API is available at `http://localhost:8080`. Database migrations run automatically on startup.

```bash
# Stop
docker compose down
```

## Architecture & Logic

### Why This Structure

```
main/main.go                       -- Thin entrypoint that loads config and calls app.Run
internal/
  app/                              -- Bootstrap: wiring, migrations, HTTP, graceful shutdown
  config/                           -- Parse env vars once at startup
  subscription/                     -- Subscription domain: Service, Repo, Subscription, errors
  release/                          -- Release domain: Poller, Release
  repository/                       -- Repository entity, Ref value object, Store (PG)
  email/                            -- email.Address value object
  platform/health/                  -- health.DBChecker
  platform/logger/                  -- slog setup
  platform/postgres/                -- *sql.DB factory + golang-migrate runner
  platform/token/                   -- token.Generator (crypto/rand → hex)
  client/github/                    -- GitHub REST API client + Redis cache decorator
  client/notification/              -- notification command publisher (RabbitMQ) + gRPC client
  messaging/                        -- reconnecting RabbitMQ publisher + consumer (transport-agnostic)
  notifyevent/                      -- broker wire contract (envelope + commands) shared by both sides
  gen/notification/v1/              -- generated protobuf/gRPC stubs (buf)
  api/rest/                         -- chi router
    subscription/                   -- subscribe/confirm/unsubscribe/list handlers
    health/                         -- /health handler
    middleware/                     -- API-key auth, per-IP rate limiting, metrics
services/notification/              -- Notification microservice (RabbitMQ consumer + gRPC, own Postgres)
  (package notification)            -- domain: value types + application service + ports
  app/                              -- composition root (lifecycle, DB, consumer + gRPC bootstrap)
  consumer/                         -- broker consumer: decode envelope + dispatch to service
  grpcserver/                       -- gRPC transport mapping (incl. VerifyEmail)
  resthttp/                         -- REST verify-email endpoint (HW10, kept alongside gRPC)
  smtp/, store/                     -- SMTP mailer + sent_notifications ledger
  main/                             -- notifier entrypoint
proto/notification/v1/              -- gRPC contract between monolith and notifier
migrations/                         -- SQL schema (auto-applied via golang-migrate)
tests/repository/                   -- Integration tests (testcontainers, real Postgres)
```

The project follows **clean architecture** with consumer-side interface placement (see [ADR 0009](docs/adr/0009-consumer-side-interface-placement.md)): each domain package declares the small unexported interfaces it actually uses. Outer layers (`client`, `api`, persistence) provide implementations and satisfy those interfaces via Go's structural typing. Business logic has zero knowledge of HTTP, SQL, or Redis.

**Why this matters**: Every dependency can be swapped or mocked independently. The domain packages are tested with pure in-memory mocks, and the persistence layer is tested against a real database. No test touches both concerns at once.

### Subscription Lifecycle

```
User                    API                     Service                  DB              Notifier
 |                       |                       |                       |                    |
 |-- POST /subscribe --> |                       |                       |                    |
 |                       |-- Subscribe() ------> |                       |                    |
 |                       |                       |-- normalizeEmail() -->|                    |
 |                       |                       |-- RepoExists() ---->(GitHub API)          |
 |                       |                       |-- GetByEmailAndRepo->|                    |
 |                       |                       |-- Upsert(repo) ----> | (FK target first)  |
 |                       |                       |-- Create(sub) -----> | (status=pending)   |
 |                       |                       |-- SendConfirmation ->|                    |--> email
 |                       |                       |   (on failure: rollback to unsubscribed)  |
 |                       |<-- 200 OK ----------- |                       |                    |
 |                       |                       |                       |                    |
 |-- GET /confirm/tok -> |-- Confirm() --------> |-- UpdateStatus ----> | (status=active)    |
 |<-- 200 OK ----------- |                       |                       |                    |
```

> **Notifier boundary:** `SendConfirmation` **publishes a command to RabbitMQ** instead of blocking on the notifier. The notification microservice consumes it asynchronously and owns SMTP delivery plus its own dedup ledger. The monolith no longer sends email directly (see [ADR 0014](docs/adr/0014-extract-notification-microservice.md)) and is now decoupled from the notifier's availability (see [Message Broker](#message-broker-rabbitmq) below).

**Why upsert the tracked repo before creating the subscription?** The `subscriptions` table has a foreign key to `tracked_repositories(owner, name)`. If we create the subscription first, the FK constraint will reject it. The upsert guarantees the FK target exists without creating duplicates (`ON CONFLICT DO NOTHING`).

**Why rollback / refresh on a stuck `pending`?** The database has a partial unique index `WHERE status != 'unsubscribed'` that prevents duplicate active/pending subscriptions for the same email+repo, so a stranded `pending` row would otherwise give the user a permanent `409 Conflict` on retry. Two mechanisms keep re-subscription open: (1) if the **publish** to RabbitMQ fails, the monolith rolls the row back to `unsubscribed`, freeing the index slot; (2) because an async SMTP failure on the consumer side is invisible to the monolith (no rollback fires), re-subscribing over an existing `pending` row **refreshes its token and resends** the confirmation in place rather than returning `409`. An `active` subscription still returns `ErrAlreadyExists`, and so does the loser of two concurrent refreshes on the same `pending` row (the token update is a compare-and-swap on the old token).

### Message Broker (RabbitMQ)

Both notification paths — subscription confirmations and new-release alerts — are **commands published to RabbitMQ** by the monolith and **consumed asynchronously** by the notification service. This decouples the producer from the consumer: the monolith returns to the user without waiting on SMTP, and a notifier restart never drops work because the queue is durable.

```
Monolith (publisher)                 RabbitMQ                  Notification service (consumer)
 |                                       |                                  |
 |-- publish {type, trace_id, payload} ->| exchange "notifications"         |
 |        routing key = type             |   (direct, durable)              |
 |                                       |-- routed to queue -------------->| "notifications.email" (durable)
 |                                       |                                  |-- decode envelope by type
 |                                       |                                  |-- dedup ledger + SMTP send
 |                                       |<------------- ack / nack --------|
```

- **Contract** (`internal/notifyevent`): a JSON `Envelope{ type, trace_id, payload }` carries one `ConfirmationCommand` or `ReleaseCommand`. The shared package keeps publisher and consumer from drifting, and `trace_id` propagates the request trace across the async hop.
- **Topology**: one durable `direct` exchange `notifications`; one durable queue `notifications.email` bound by the command type used as the routing key. Both sides declare it idempotently, so no command is lost while the consumer is still starting.
- **Delivery semantics** (`internal/messaging`): persistent messages, manual ack, prefetch 16. The consumer **acks** on success, **drops** (nack, no requeue) permanently bad input — malformed JSON, unknown type, missing fields — so it cannot poison-loop, and **requeues** (nack, requeue) on transient send failures so they are retried. Both the publisher and consumer reconnect automatically when the broker blips.
- **gRPC retained**: the notifier still serves its gRPC endpoint (health surface + in-process integration tests). The broker is the production path; see the consumer logic in `services/notification/consumer`.

### Subscribe Saga (Orchestrated)

`POST /subscribe` runs as an **orchestrated saga** — a distributed transaction across the monolith (the subscription row) and the notification service (the confirmation email), with compensation:

```
Subscriber   Monolith (orchestrator)        RabbitMQ          Notifier (participant)
   | POST /subscribe   |                        |                        |
   |------------------>|-- reserve pending row  |                        |
   |                   |-- persist saga --------|                        |
   |                   |-- SendConfirmation --->| saga.commands -------->|-- dedup + SMTP
   |                   |                        |<-- confirmation_sent --|
   |                   |<- saga.replies --------|                        |
   |<--- 200 / 503 ----|  (confirmation_failed or timeout -> cancel the subscription)
```

- **Orchestrator** (`internal/saga`): persists each saga in `saga_instances`, blocks the request for the outcome via an in-memory waiter the durable reply consumer signals, and returns the real `200`/`503` — not a fire-and-forget `202`.
- **Compensation**: a `confirmation_failed` reply (or a timeout) cancels the subscription, freeing the partial-unique-index slot. A `time.Ticker` **reaper** compensates sagas whose reply never arrives, so a notifier crash cannot strand a subscription.
- **Idempotency**: state transitions are single-winner conditional `UPDATE`s, the participant is deduped by the confirm-URL ledger, and duplicate replies are no-ops.

### REST → gRPC Migration (verify-email)

HW10 migrates one **synchronous inter-service call** from REST to gRPC, keeping the REST implementation alongside for comparison.

```
Monolith (Verifier)                         Notification service
  |  transport = grpc (default) | rest      |
  |--- VerifyEmail(email, confirm_url, repo) over chosen transport -->|
  |                                          |-- same SendConfirmation service logic
  |<------------------ delivered ------------|   (dedup ledger + SMTP)
```

- **Contract** (`proto/notification/v1`): a new Unary RPC `VerifyEmail(VerifyEmailRequest) → VerifyEmailResponse`. A *new* RPC (not a reuse of `SendConfirmation`) keeps the before/after migration story explicit. `buf lint` guards the contract; `buf generate` (`make proto`) regenerates the stubs.
- **gRPC status codes** (`services/notification/grpcserver`): missing `email`/`confirm_url`/`repo` → `InvalidArgument`; a downstream send failure → `Internal`; success → `delivered`. The REST handler mirrors this as `400` / `500` / `200`.
- **REST kept alongside** (`services/notification/resthttp`): `POST /api/v1/verify-email` serves the same JSON contract on `REST_ADDR` (default `:8081`), so both transports run against identical service logic.
- **Swappable client** (`internal/client/notification`): both the gRPC `Client` and the `RESTClient` satisfy one `Verifier` interface; `NewVerifier(transport, target, log)` selects `grpc` (default) or `rest`.

**Benchmark** (`make bench` — in-process server, no-op sender, so the delta is pure transport cost; 13th-gen i7, 16 logical CPUs):

| Scenario | gRPC | REST | Winner |
| --- | --- | --- | --- |
| **Sequential latency** (1 caller) | ~392 µs/op (~2.5k req/s) | ~95 µs/op (~10.5k req/s) | **REST ~4×** |
| **Parallel throughput** (16 cores) | ~31 µs/op (~32k req/s) | ~71 µs/op (~14k req/s) | **gRPC ~2.3×** |
| **Allocations under load** | 10.5 KB/op | 32 KB/op | **gRPC ~3× less** |

The honest takeaway: for a **single small synchronous call on loopback, REST/JSON has lower latency** — gRPC's HTTP/2 framing and flow-control overhead per call has nothing to amortize against. gRPC's structural advantages appear **under concurrency**: it multiplexes all RPCs over one HTTP/2 connection and pulls ~2.3× ahead on throughput with ~3× less memory churn, while HTTP/1.1 is bottlenecked by one in-flight request per connection. gRPC also wins on the qualities a benchmark can't show — a typed, versioned `.proto` contract, code generation, and first-class streaming — which is why it is the default transport here. The reaper- and broker-based production path is unchanged; this migration covers the one synchronous request/response hop.

### Background Poller Logic

```
Every SCAN_INTERVAL (default 5m):
  1. Lock mutex (skip if previous poll still running)
  2. SELECT * FROM tracked_repositories
  3. For each repo:
     a. GET /repos/{owner}/{name}/releases/latest from GitHub (or Redis cache)
     b. Compare release.tag_name with repo.last_seen_tag
     c. If same tag or no release: update last_checked_at and skip
     d. If new tag:
        i.   UPDATE last_seen_tag = new_tag  (persist FIRST)
        ii.  SELECT emails WHERE repo = this AND status = 'active'
        iii. For each email: send notification
  4. Unlock mutex
```

**Why persist-before-notify?** If the app crashes after sending 50 of 100 emails but before updating `last_seen_tag`, the next poll will re-detect the same release and send all 100 emails again -- resulting in 50 duplicates. By updating the tag first, we guarantee at-most-once detection. The trade-off is that if the app crashes between persisting the tag and sending emails, some users miss the notification. This is acceptable: a missed notification is far less harmful than repeated spam.

**Why a mutex?** If a poll takes longer than `SCAN_INTERVAL` (e.g., many repos or slow SMTP), the ticker will fire again. Without the mutex, two polls could run concurrently and send duplicate notifications for the same release. The mutex ensures only one poll runs at a time; the overlapping tick is skipped with a log message.

### Rate Limit Handling (GitHub API)

The GitHub client uses a 3-tier retry strategy for `429 Too Many Requests`:

1. **`Retry-After` header** -- GitHub explicitly tells us how many seconds to wait. Highest priority.
2. **`X-RateLimit-Reset` header** -- Unix timestamp when the rate limit resets. Used if `Retry-After` is absent. Capped at 120 seconds to avoid waiting excessively on clock skew.
3. **Exponential backoff** -- 1s, 2s, 4s. Fallback when neither header is present.

After 3 retries, the client returns an error rather than blocking indefinitely. All waits are context-aware: if the caller's context is cancelled, the retry loop exits immediately.

### Redis Caching (Decorator Pattern)

```
Service --> GitHubClient interface
                |
          CachedClient (decorator)
                |
            base *Client (actual HTTP calls)
```

`CachedClient` wraps the base `*Client` and satisfies the same GitHub-facing interfaces. The domain packages don't know caching exists -- they call the same `RepoExists()` and `GetLatestRelease()` methods.

**Cache-aside flow**: Check Redis -> on miss, call GitHub API -> store in Redis -> return. On Redis error (connection lost, timeout), log the error and fall through to the API. The system never fails due to Redis being unavailable.

**What is NOT cached**: `nil` releases (repo with no releases). Caching a nil would mean we'd miss the first release for up to 10 minutes. Only positive results are cached.

### Prometheus Metrics (Observability)

The `/metrics` endpoint exposes three metrics following the RED method (Rate, Errors, Duration):

| Metric | Type | Labels | Purpose |
|--------|------|--------|---------|
| `http_requests_total` | Counter | `method`, `path`, `status` | Request throughput and error rate |
| `http_request_duration_seconds` | Histogram | `method`, `path` | Latency distribution (p50, p90, p99) |
| `http_requests_in_flight` | Gauge | -- | Current concurrent request load |

**High-cardinality prevention**: The `path` label uses `chi.RouteContext().RoutePattern()` (e.g., `/api/confirm/{token}`) instead of the raw URL (e.g., `/api/confirm/abc123...`). Without this, every unique token would create a new Prometheus time series, causing unbounded memory growth.

**Self-scrape exclusion**: The middleware skips recording metrics for `GET /metrics` itself. Without this, every Prometheus scrape (every 15-30s) would inflate `http_requests_total` with noise unrelated to actual API traffic.

**Interface safety**: The `statusRecorder` wrapper implements `http.Flusher` in addition to `http.ResponseWriter`, preventing panics if any handler ever uses streaming responses.

### Database Design Decisions

**Partial unique index** (`WHERE status != 'unsubscribed'`): A regular unique index on `(email, repo_owner, repo_name)` would prevent re-subscribing after unsubscribing. The partial index only enforces uniqueness for `pending` and `active` rows, allowing unlimited `unsubscribed` history rows for the same email+repo pair.

**Foreign key with CASCADE**: `subscriptions.repo_owner/repo_name` references `tracked_repositories.owner/name` with `ON DELETE CASCADE`. If a tracked repo is removed, all its subscriptions are automatically cleaned up. This prevents orphaned subscription rows.

**`updated_at` trigger**: A database trigger automatically sets `updated_at = NOW()` on every UPDATE to the `subscriptions` table. This means the application never manually sets this field, eliminating bugs from forgotten timestamp updates.

**Connection pooling**: 25 max open connections, 10 max idle, 5-minute max lifetime. These defaults handle moderate concurrency without exhausting PostgreSQL's default `max_connections = 100`.

### Security Measures

- **Constant-time API key comparison**: `crypto/subtle.ConstantTimeCompare` prevents timing attacks on the `X-API-Key` header.
- **Email header injection prevention**: The SMTP client strips `\r` and `\n` from all header values (To, From) and uses MIME Q-encoding for the Subject line.
- **Trusted proxy gating**: `X-Forwarded-For` and `X-Real-IP` headers are only read when `TRUSTED_PROXY=true`. Without this, any client could spoof their IP and bypass rate limiting. The config is parsed once at startup and injected into the rate limiter struct -- zero `os.Getenv` calls in the hot path.
- **Token generation**: 32 bytes from `crypto/rand`, hex-encoded to 64 characters. This gives 256 bits of entropy, making brute-force infeasible.
- **Token hidden from API responses**: The `Subscription.Token` field is tagged `json:"-"`, so it's never serialized in JSON responses. Tokens only appear in confirmation emails.
- **Parameterized SQL**: Every query uses `$1, $2, ...` placeholders. No string interpolation. No SQL injection surface.
- **URL path escaping**: Owner and repo names are passed through `url.PathEscape()` before being interpolated into GitHub API URLs, preventing path traversal.

## Trade-offs & Assumptions

| Decision | Trade-off | Rationale |
|----------|-----------|-----------|
| Structured logging with `log/slog` | Text output is simpler than a full JSON logging pipeline | Standard library logging keeps dependencies low while preserving useful fields. |
| Sequential email sending in poller | Slow for repos with many subscribers | Simpler to reason about; a worker pool would be the next improvement |
| In-memory rate limiter | Lost on restart; doesn't work across multiple instances | No external dependency; sufficient for single-instance deployment |
| Go 1.25 module target | Docker and local builds should use Go 1.25+ | Matches `go.mod`; the Dockerfiles use `golang:1.25-alpine`. |
| First poll sends notifications for existing releases | Users may get a notification for a release that was already published | Treating the first detection as "new" is simpler than adding a separate "first seen" flag; the alternative risks silently missing real new releases |

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/subscribe` | Rate limited | Subscribe to release notifications |
| `GET` | `/api/confirm/{token}` | -- | Confirm subscription via email link |
| `GET` | `/api/unsubscribe/{token}` | -- | Unsubscribe via email link |
| `GET` | `/api/subscriptions?email=` | `X-API-Key` | List active subscriptions for an email |
| `GET` | `/health` | -- | Database health check |
| `GET` | `/metrics` | -- | Prometheus metrics (requests, latency, in-flight) |

### Example: Subscribe

```bash
curl -X POST http://localhost:8080/api/subscribe \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com", "repo": "golang/go"}'
```

Response: `200 OK`
```json
{"message": "Subscription created. Please confirm via email."}
```

### Example: List Subscriptions

```bash
curl http://localhost:8080/api/subscriptions?email=user@example.com \
  -H "X-API-Key: your-api-key"
```

### Error Responses

| Status | When |
|--------|------|
| `400` | Invalid email, invalid repo format, or malformed JSON |
| `401` | Missing or invalid API key (subscriptions endpoint only) |
| `404` | Repository not found on GitHub, or invalid confirmation/unsubscribe token |
| `409` | An **active** subscription already exists for this email+repo (a still-`pending` one is refreshed and the confirmation resent) |
| `429` | Rate limit exceeded (includes `Retry-After` header) |
| `503` | SMTP server unavailable (subscription was rolled back, safe to retry) |
| `500` | Internal server error |

Full API specification: [`swagger.yaml`](swagger.yaml)

## Testing

### Unit Tests

```bash
make test
```

Runs with `-short` flag, skipping integration tests. No external dependencies required.

| Area | What is tested |
|------|----------------|
| Subscription service | Full lifecycle: subscribe, confirm, unsubscribe, idempotency, validation, error propagation |
| Email normalization | `"User <USER@Example.COM>"` normalizes to `user@example.com` |
| SMTP rollback | Subscription is rolled back when email delivery fails, returning `ErrEmailSendFailed` |
| GitHub client | Rate-limit retry logic (Retry-After, X-RateLimit-Reset, exponential backoff), context cancellation |
| Redis cache | Cache hit/miss, TTL expiry (via `miniredis` fast-forward), nil not cached, graceful degradation when Redis is down |
| Release poller | New release detection, duplicate prevention, persist-before-notify ordering, context cancellation |

### Integration Tests (12 tests)

```bash
make test-integration
```

Requires Docker. Uses `testcontainers-go` to spin up a real PostgreSQL 16 instance, run migrations, and verify:

- Upsert idempotency for tracked repositories
- `UpdateLastSeen` timestamp tracking
- Subscription CRUD and `GetByToken` lookup
- Partial unique index: allows re-subscribe after unsubscribe, blocks duplicate active/pending
- Foreign key constraint: cannot create subscription for non-existent repo
- `ON DELETE CASCADE`: deleting a tracked repo removes all its subscriptions
- `updated_at` database trigger fires on status changes

## Environment Variables

Copy `.env.example` to `.env` and configure:

```bash
cp .env.example .env
```

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_PORT` | `8080` | HTTP server port |
| `BASE_URL` | `http://localhost:8080` | Public base URL used to build confirmation links in emails |
| `DB_HOST` | `localhost` | PostgreSQL host (`postgres` in Docker Compose) |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | PostgreSQL user |
| `DB_PASSWORD` | `postgres` | PostgreSQL password |
| `DB_NAME` | `release_notifier` | Database name |
| `DB_SSLMODE` | `require` | PostgreSQL SSL mode (`disable` in local Docker Compose) |
| `GITHUB_TOKEN` | -- | GitHub personal access token (optional, increases rate limit) |
| `RABBITMQ_URL` | `amqp://localhost:5672/` | Message broker the monolith publishes notification commands to (`amqp://guest:guest@rabbitmq:5672/` in Docker Compose) |
| `SCAN_INTERVAL` | `5m` | How often to check for new releases |
| `SAGA_TIMEOUT` | `30s` | How long the subscribe saga waits for the confirmation outcome before the reaper compensates |
| `API_KEY` | -- | API key for the `GET /api/subscriptions` endpoint |
| `REDIS_ADDR` | `localhost:6379` | Redis address (`redis:6379` in Docker Compose) |
| `REDIS_PASSWORD` | -- | Redis password |
| `REDIS_DB` | `0` | Redis database number |
| `REDIS_CACHE_TTL` | `10m` | Cache TTL for GitHub API responses |
| `TRUSTED_PROXY` | `false` | Set to `true` if running behind a reverse proxy to trust `X-Forwarded-For` |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, or `error` |

### Notification service (separate process)

| Variable | Default | Description |
|----------|---------|-------------|
| `GRPC_ADDR` | `:50051` | gRPC listen address (health surface + integration tests; serves `VerifyEmail`) |
| `REST_ADDR` | `:8081` | REST listen address for `POST /api/v1/verify-email` (HW10 transport kept alongside gRPC) |
| `RABBITMQ_URL` | `amqp://localhost:5672/` | Message broker the notifier consumes notification commands from |
| `DB_HOST` / `DB_PORT` / `DB_USER` / `DB_PASSWORD` | `localhost` / `5432` / `postgres` / `postgres` | Notifier's own PostgreSQL (DB-per-service; `postgres-notifier` in Docker Compose) |
| `DB_NAME` | `notification` | Notifier database name |
| `SMTP_HOST` | `localhost` | SMTP server host (`mailpit` in Docker Compose) |
| `SMTP_PORT` | `587` | SMTP server port |
| `SMTP_USER` / `SMTP_PASSWORD` | -- | SMTP credentials |
| `SMTP_FROM` | `noreply@example.com` | Sender email address |
| `SMTP_TIMEOUT` | `30s` | Per-message SMTP delivery timeout (bounds the broker-consumer path, which has no request deadline) |

## Project Structure

```
.
├── main/main.go                 # Thin entrypoint
├── internal/
│   ├── app/                     # Bootstrap: wiring, migrations, HTTP, shutdown
│   ├── api/rest/
│   │   ├── subscription/        # subscribe / confirm / unsubscribe / list handlers
│   │   ├── health/              # /health handler
│   │   └── middleware/          # API key auth, rate limiter, Prometheus metrics
│   ├── client/
│   │   ├── github/              # GitHub API client + Redis cache decorator
│   │   └── notification/        # Notification command publisher (RabbitMQ) + gRPC client
│   ├── messaging/               # Reconnecting RabbitMQ publisher + consumer (transport-agnostic)
│   ├── notifyevent/             # Broker wire contract (envelope + commands), shared by both sides
│   ├── gen/notification/v1/     # Generated protobuf/gRPC stubs (buf)
│   ├── config/                  # Environment-based config
│   ├── subscription/            # Subscription domain (Service + Repo + types + errors)
│   ├── release/                 # Release domain (Poller + Release)
│   ├── repository/              # repository.Repository entity + Ref + Store (PG)
│   ├── email/                   # email.Address value object
│   └── platform/                # health.DBChecker, slog, postgres, token.Generator
├── services/notification/       # Notification microservice (RabbitMQ consumer + gRPC, own Postgres)
│   ├── (package notification)   # Domain: value types + application service + ports
│   ├── app/                     # Composition root (lifecycle, DB, consumer + gRPC bootstrap)
│   ├── consumer/                # Broker consumer: decode envelope + dispatch to service
│   ├── grpcserver/              # gRPC transport mapping (incl. VerifyEmail)
│   ├── resthttp/                # REST verify-email endpoint (HW10, kept alongside gRPC)
│   ├── smtp/                    # SMTP mailer + templates
│   ├── store/                   # sent_notifications dedup ledger (PG)
│   ├── migrations/              # Notifier schema (embedded, auto-applied on startup)
│   └── main/                    # Notifier entrypoint
├── proto/notification/v1/       # gRPC contract (buf generate)
├── tests/repository/            # Integration tests (testcontainers + real Postgres)
├── migrations/                  # SQL schema (auto-applied on startup)
├── docker-compose.yml           # PostgreSQL 16 x2 + Redis 7 + RabbitMQ + mailpit + notifier + app
├── Dockerfile                   # Multi-stage build (Alpine), monolith
├── Dockerfile.notifier          # Multi-stage build (Alpine), notification service
├── Makefile                     # build, proto, test, test-integration, lint, docker-up/down
└── swagger.yaml                 # OpenAPI 3.0 specification
```
