# GitHub Release Notification API

> A Go service that lets users subscribe to email notifications about new GitHub repository releases. Implements a full subscription lifecycle (subscribe, confirm via email, unsubscribe) with a background poller that checks for new releases and notifies subscribers.

Built with Go, PostgreSQL, Redis, Chi, and SMTP.

## Bonus Points Achieved

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
# Edit .env: set GITHUB_TOKEN, SMTP credentials, and API_KEY

# 2. Start everything (Postgres + Redis + app)
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
  subscription/                     -- Subscription domain: Service, Subscription, errors
  release/                          -- Release domain: Poller, Release, TrackedRepository
  email/                            -- email.Address value object
  repo/                             -- repo.Ref value object
  platform/health/                  -- health.DBChecker
  platform/logger/                  -- slog setup
  platform/postgres/                -- *sql.DB factory + golang-migrate runner
  platform/token/                   -- token.Generator (crypto/rand → hex)
  storage/                          -- PostgreSQL data access (parameterized queries only)
  client/github/                    -- GitHub REST API client + Redis cache decorator
  client/mailer/                    -- SMTP client + email templates
  api/rest/                         -- chi router
    subscription/                   -- subscribe/confirm/unsubscribe/list handlers
    health/                         -- /health handler
    middleware/                     -- API-key auth, per-IP rate limiting, metrics
migrations/                         -- SQL schema (auto-applied via golang-migrate)
tests/storage/                      -- Integration tests (testcontainers, real Postgres)
```

The project follows **clean architecture** with consumer-side interface placement (see [ADR 0009](docs/adr/0009-consumer-side-interface-placement.md)): each domain package declares the small unexported interfaces it actually uses. Outer layers (`storage`, `client`, `api`) provide implementations and satisfy those interfaces via Go's structural typing. Business logic has zero knowledge of HTTP, SQL, or Redis.

**Why this matters**: Every dependency can be swapped or mocked independently. The domain packages are tested with pure in-memory mocks, and the storage layer is tested against a real database. No test touches both concerns at once.

### Subscription Lifecycle

```
User                    API                     Service                  DB                  Email
 |                       |                       |                       |                    |
 |-- POST /subscribe --> |                       |                       |                    |
 |                       |-- Subscribe() ------> |                       |                    |
 |                       |                       |-- normalizeEmail() -->|                    |
 |                       |                       |-- RepoExists() ---->(GitHub API)          |
 |                       |                       |-- Exists() --------> |                    |
 |                       |                       |-- Upsert(repo) ----> | (FK target first)  |
 |                       |                       |-- Create(sub) -----> | (status=pending)   |
 |                       |                       |-- SendConfirmation ->|                    |--> email
 |                       |                       |   (on failure: rollback to unsubscribed)  |
 |                       |<-- 200 OK ----------- |                       |                    |
 |                       |                       |                       |                    |
 |-- GET /confirm/tok -> |-- Confirm() --------> |-- UpdateStatus ----> | (status=active)    |
 |<-- 200 OK ----------- |                       |                       |                    |
```

**Why upsert the tracked repo before creating the subscription?** The `subscriptions` table has a foreign key to `tracked_repositories(owner, name)`. If we create the subscription first, the FK constraint will reject it. The upsert guarantees the FK target exists without creating duplicates (`ON CONFLICT DO NOTHING`).

**Why rollback on email failure?** The database has a partial unique index `WHERE status != 'unsubscribed'` that prevents duplicate active/pending subscriptions for the same email+repo. If the confirmation email fails, the subscription remains in `pending` status, and the user gets a permanent `409 Conflict` on retry. The compensation rollback sets the status to `unsubscribed`, freeing the index slot so the user can try again.

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
| Go 1.24 module target | Docker and local builds should use Go 1.24+ | Matches `go.mod`; the Dockerfile uses `golang:1.24-alpine`. |
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
| `409` | Subscription already exists for this email+repo |
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
| `DB_HOST` | `localhost` | PostgreSQL host (`postgres` in Docker Compose) |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | PostgreSQL user |
| `DB_PASSWORD` | `postgres` | PostgreSQL password |
| `DB_NAME` | `release_notifier` | Database name |
| `DB_SSLMODE` | `require` | PostgreSQL SSL mode (`disable` in local Docker Compose) |
| `GITHUB_TOKEN` | -- | GitHub personal access token (optional, increases rate limit) |
| `SMTP_HOST` | `localhost` | SMTP server host |
| `SMTP_PORT` | `587` | SMTP server port |
| `SMTP_USER` | -- | SMTP username |
| `SMTP_PASSWORD` | -- | SMTP password |
| `SMTP_FROM` | `noreply@example.com` | Sender email address |
| `SCAN_INTERVAL` | `5m` | How often to check for new releases |
| `BASE_URL` | `http://localhost:8080` | Base URL for confirmation/unsubscribe links in emails |
| `API_KEY` | -- | API key for the `GET /api/subscriptions` endpoint |
| `REDIS_ADDR` | `localhost:6379` | Redis address (`redis:6379` in Docker Compose) |
| `REDIS_PASSWORD` | -- | Redis password |
| `REDIS_DB` | `0` | Redis database number |
| `REDIS_CACHE_TTL` | `10m` | Cache TTL for GitHub API responses |
| `TRUSTED_PROXY` | `false` | Set to `true` if running behind a reverse proxy to trust `X-Forwarded-For` |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, or `error` |

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
│   │   └── mailer/              # SMTP transport + email templates
│   ├── config/                  # Environment-based config
│   ├── subscription/            # Subscription domain (Service + types + errors)
│   ├── release/                 # Release domain (Poller + Release/TrackedRepository)
│   ├── email/                   # email.Address value object
│   ├── repo/                    # repo.Ref value object
│   ├── platform/                # health.DBChecker, slog, postgres, token.Generator
│   └── storage/                 # PostgreSQL data access layer
├── tests/storage/               # Integration tests (testcontainers + real Postgres)
├── migrations/                  # SQL schema (auto-applied on startup)
├── docker-compose.yml           # PostgreSQL 16 + Redis 7 + app
├── Dockerfile                   # Multi-stage build (Alpine)
├── Makefile                     # build, test, test-integration, lint, docker-up/down
└── swagger.yaml                 # OpenAPI 3.0 specification
```
