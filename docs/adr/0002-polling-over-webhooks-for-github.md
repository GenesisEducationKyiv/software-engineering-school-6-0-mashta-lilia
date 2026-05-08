# ADR 0002: Poll GitHub for Releases Instead of Using Webhooks

Date: 2026-05-08
Status: Accepted
Deciders: Project Author

## Context and Problem Statement

The service must detect new releases on tracked GitHub repositories and notify
subscribers. GitHub offers two mechanisms:

1. **Webhooks** — GitHub pushes a `release` event to our HTTPS endpoint
   immediately when a release is published.
2. **Polling** — we periodically call the REST API
   (`GET /repos/{owner}/{name}/releases/latest`) and detect changes by
   comparing the returned tag to a stored `last_seen_tag`.

The choice affects deployment topology, latency to detection, public-internet
exposure, and rate-limit pressure on the GitHub API.

## Decision Drivers

* Subscribers can be added for **any public GitHub repo**, not just repos the
  service operator owns.
* The service runs in a single-binary deployment; there is no public ingress
  guaranteed during local development.
* Detection latency tolerance: minutes are acceptable, seconds are not
  required (release notifications are not real-time alerts).
* Operational simplicity for the homework scope — one person maintaining the
  service.

## Considered Options

* Option 1: **Periodic polling** with a configurable `SCAN_INTERVAL`
  (default 5 minutes) using `time.Ticker`.
* Option 2: GitHub webhooks pushed to a public HTTPS endpoint.
* Option 3: Hybrid — webhooks for owned repos, polling for others.
* Option 4: GitHub GraphQL API subscriptions / SSE.

## Decision Outcome

Chosen option: **Option 1 — periodic polling**, because webhooks require the
operator to own (or have admin rights on) every tracked repo, which directly
contradicts the product requirement that any user can subscribe to any public
repo.

The polling implementation lives in `internal/service/scanner.go`:

* Locks a mutex on each tick to skip overlapping scans (a slow scan does not
  cause concurrent fan-out).
* Reads all rows from `tracked_repositories`, calls
  `GitHubClient.GetLatestRelease()` per repo, and compares to
  `last_seen_tag`.
* Persists `last_seen_tag` **before** sending email, guaranteeing
  at-most-once detection — see
  [ADR 0007](0007-persist-before-notify-for-at-most-once.md).

GitHub API rate-limit pressure is mitigated by Redis caching with a 10-minute
TTL — see [ADR 0004](0004-redis-cache-aside-as-decorator.md).

### Consequences

* Good, because the service works for **any** public repo without requiring
  webhook configuration on the upstream.
* Good, because the service has zero public-ingress requirement in
  development — no ngrok / cloudflared tunnels.
* Good, because crashes simply delay detection by one interval; webhooks
  would silently lose events delivered during downtime (GitHub does not
  automatically retry failed webhook deliveries—failed deliveries remain
  failed until manually redelivered via UI, REST API, or scripts within a
  3-day window; see
  [GitHub webhook delivery documentation](https://docs.github.com/en/webhooks/using-webhooks/handling-webhook-deliveries)).
* Bad, because detection latency is bounded below by `SCAN_INTERVAL`
  (5 min default). Subscribers see notifications up to 5 minutes after
  release publication.
* Bad, because every tracked repo costs one API call per scan.
  At GitHub's 5,000 req/hr authenticated limit, the upper bound is
  `5000 / (60/SCAN_INTERVAL_MIN)` distinct repos
  (≈ 416 repos at 5-minute intervals before exhausting the quota).
  Caching mitigates this for popular repos.

## Pros and Cons of the Options

### Periodic polling

* + Works for any public repo, no upstream config needed.
* + Tolerates downtime — the next scan catches up.
* + No public ingress needed.
* − Detection latency = up to `SCAN_INTERVAL`.
* − Linear cost as tracked-repo count grows (one API call per repo per scan
  interval).

### Webhooks

* + Sub-second detection latency.
* + Zero polling traffic — only fires when something happens.
* − Requires admin rights on every tracked repo. Disqualifying for this
  product.
* − Requires public HTTPS endpoint with a stable URL and signature
  verification.
* − Lost-event risk during service downtime.

### Hybrid

* + Best latency where possible, broad coverage where not.
* − Doubles the code paths (two detection mechanisms, two failure modes).
* − Complexity unjustified at current scale.

### GraphQL subscriptions / SSE

* + Server-side push without webhooks.
* − GitHub does not offer this for releases. Disqualified.
