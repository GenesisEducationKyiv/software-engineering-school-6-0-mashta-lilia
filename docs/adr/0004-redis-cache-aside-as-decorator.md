# ADR 0004: Cache GitHub API Responses with Redis Using a Decorator

Date: 2026-05-08
Status: Accepted
Deciders: Project Author

## Context and Problem Statement

The polling scanner (see [ADR 0002](0002-polling-over-webhooks-for-github.md))
calls `GET /repos/{owner}/{name}/releases/latest` once per tracked repo per
tick. With a 5-minute interval and 200 tracked repos, that is ≈ 2,400 calls
per hour, which is well below GitHub's authenticated rate limit of 5,000/hr —
but the budget shrinks linearly as the catalogue grows or `SCAN_INTERVAL`
shortens.

Most calls return the same response: a release tag changes infrequently
relative to scan frequency. The repeated calls are wasteful and bring the
service close to rate-limit cliffs that would otherwise require backoff
(see GitHub client retry logic).

## Decision Drivers

* Reduce GitHub API call volume to avoid 429 retries during normal operation.
* Caching must be **optional** — the service must run without Redis (e.g.,
  local development, minimal deployments).
* Caching must not change the public contract of `GitHubClient` — adding it
  should not require touching service-layer code.
* Stale data is acceptable for short windows; serving last week's tag is not.

## Considered Options

* Option 1: **Cache-aside (lazy) caching with Redis**, implemented as a
  **decorator** that wraps the base GitHub client and satisfies the same
  `GitHubClient` interface.
* Option 2: Write-through caching — every API response is written to Redis
  synchronously; reads always come from cache.
* Option 3: In-process LRU cache (e.g., `groupcache`).
* Option 4: No caching; rely on GitHub API rate-limit retry only.

## Decision Outcome

Chosen option: **Option 1 — cache-aside with a decorator**, because it
delivers the call-volume reduction without touching `service/` code and
gracefully degrades when Redis is unavailable.

Implementation (`internal/client/github/cache.go`):

```
service ──→ GitHubClient (interface)
              ▲
              │ same contract
              │
        CachedClient (decorator)
              │
              ▼
          *Client (HTTP, calls api.github.com)
```

`CachedClient.GetLatestRelease()`:
1. `GET` the cache key `gh:release:{owner}/{name}` from Redis.
2. On hit: deserialize and return.
3. On miss or any Redis error: log the error, call the wrapped client,
   `SET` the result with a 10-minute TTL, return.
4. **Negative results are not cached** (`nil` releases for repos with no
   releases). Caching `nil` would mean the service silently misses the
   first release for up to 10 minutes — see Consequences.

Wiring (`cmd/server/main.go`):

```go
ghClient = github.NewClient(token)
if rdb := tryConnectRedis(cfg); rdb != nil {
    ghClient = github.NewCachedClient(ghClient, rdb, cfg.RedisCacheTTL)
}
```

The service layer always calls the same interface; it never knows whether
caching is active.

### Consequences

* Good, because adding caching required zero changes to `service/`. The
  decorator pattern is the cleanest expression of "augment a behavior
  without modifying its consumers."
* Good, because the service tolerates Redis being down or unreachable —
  every Redis error falls through to the API. Caching is best-effort.
* Good, because the 10-minute TTL is significantly shorter than typical
  release publication frequency for tracked repos (most release weekly or
  less), so cache hits dominate.
* Bad, because not caching nil-results means a repo that has never released
  pays the full API call every scan, forever. Acceptable trade-off given the
  cost of missing the first release.
* Bad, because Redis adds an operational dependency in deployments where it
  is enabled. The graceful-degradation path mitigates this but does not
  remove the additional moving part.
* Bad, because cache invalidation is purely TTL-based — there is no
  "release just published" signal that flushes the cache. A subscriber may
  see up to 10-minute additional latency on top of `SCAN_INTERVAL`.

## Pros and Cons of the Options

### Cache-aside + decorator

* + Zero impact on service layer.
* + Graceful degradation on Redis failure.
* + Naturally extends to cache other GitHub endpoints in the future.
* − Cache invalidation is TTL-only.

### Write-through

* + Slightly simpler logic — every read is a cache read.
* − Failure semantics are worse: if Redis is down, writes have no fallback.
* − No real benefit here over cache-aside; the workload is read-dominated.

### In-process LRU

* + No external dependency.
* − Lost on every restart, defeating the purpose for a polling scanner.
* − Does not share state across replicas (relevant for future scale).

### No caching

* + Simplest code path.
* − Burns the rate-limit budget linearly with `tracked_repos × scan_freq`.
* − Forces aggressive rate-limit retry logic on the hot path.
