# ADR 0006: Synchronous Email Fan-out from the Scanner Loop

Date: 2026-05-08
Status: Accepted
Deciders: Project Author

## Context and Problem Statement

When the scanner detects a new release for repo R with N subscribers, it must
deliver an email to each of the N subscribers. The naïve in-loop send works,
but raises three classic distributed-systems concerns:

1. **Throughput** — N sequential SMTP round-trips per release dominate the
   scanner cycle for popular repos.
2. **Failure isolation** — if one SMTP send fails, do we abort the whole
   batch, retry that single recipient, or skip and continue?
3. **At-most-once vs. at-least-once** — does a crash mid-loop double-send
   to recipients who were already notified?

The alternative is to enqueue each notification onto a message broker (Kafka,
RabbitMQ, NATS, AWS SQS) and let a separate worker pool drain the queue.

## Decision Drivers

* Operational simplicity — every additional infrastructure component adds
  deployment, monitoring, and on-call surface area.
* Volume — release events are rare relative to subscription counts; even
  popular open-source repos publish releases on the order of weeks, not
  seconds.
* Acceptable failure semantics — for this product, a missed notification is
  far less harmful than a duplicate ("you got this email twice"). Users
  tolerate occasional missed releases; they unsubscribe over duplicates.
* Single-instance deployment target — scaling out to multiple replicas is
  not a current requirement.

## Considered Options

* Option 1: **Sequential synchronous fan-out** in the scanner loop —
  `for each email: mailer.Send(...)`.
* Option 2: Bounded worker-pool fan-out — N goroutines draining a per-tick
  channel.
* Option 3: Push notifications onto a queue (Redis Streams, NATS, RabbitMQ);
  a separate worker pool consumes.
* Option 4: Outbox pattern in Postgres — write a row per notification, a
  separate process reads and sends.

## Decision Outcome

Chosen option: **Option 1 — sequential synchronous fan-out**, because the
volume does not justify any of the alternatives at the current scope. The
trade-off is explicitly accepted: when a release fan-out is large,
the scanner mutex (`internal/service/scanner.go`) holds long enough that
the next ticker fire is skipped, deferring detection of other repos by one
interval. This is acceptable.

Failure semantics:
* Per-recipient errors are logged and **do not abort** the loop. A single
  bad address does not block the rest of the recipients.
* `last_seen_tag` is persisted **before** the fan-out begins, guaranteeing
  at-most-once detection — see
  [ADR 0007](0007-persist-before-notify-for-at-most-once.md).
* Email log fields exclude the recipient address — the repo identifier is
  enough to debug, and avoids PII in log aggregators.

### Consequences

* Good, because zero new infrastructure: no broker, no queue worker, no
  separate dead-letter-queue process.
* Good, because the failure model is trivial to reason about — one loop,
  one mutex, one log line per failure.
* Good, because the trade-off is documented loudly in the README and this
  ADR, so future contributors know exactly when to revisit (when fan-out
  duration approaches `SCAN_INTERVAL`).
* Bad, because for a release with 1,000 subscribers and a 200-ms SMTP
  round-trip, fan-out takes 200 seconds — longer than the default scan
  interval. The scanner mutex will skip subsequent ticks, delaying
  detection of releases on other tracked repos.
* Bad, because there is no retry on transient SMTP failure (e.g., a
  greylisting 421). The recipient is logged and lost.
* Bad, because the scanner cannot run on multiple instances safely without
  introducing distributed coordination — see "Future Direction."

## Pros and Cons of the Options

### Sequential synchronous

* + Zero new infrastructure.
* + Simplest possible failure model.
* − Throughput-limited; scales linearly with subscriber count.
* − Single instance only.

### Bounded worker pool

* + 5–10× throughput improvement at trivial complexity cost.
* + Still no new infrastructure.
* − Requires per-recipient context handling, careful shutdown, mutex tuning.

### Message queue (Redis Streams / RabbitMQ / NATS)

* + Decouples detection from delivery; horizontal scale of workers.
* + Built-in retry, dead-letter, observability.
* − New runtime dependency.
* − Introduces at-least-once delivery semantics — duplicate-email risk that
  must be mitigated (idempotency key per (release, recipient)).

### Postgres outbox

* + Works with existing infrastructure.
* + Strong durability — notifications survive crashes.
* − Polling on top of Postgres for queue semantics is anti-pattern at
  scale.
* − Requires a second worker process or goroutine.

## Future Direction

The natural escalation path, in order of complexity:

1. Move to **bounded worker pool** (Option 2). Smallest change.
2. If multi-instance deployment is needed, switch to **outbox pattern**
   (Option 4) with row-level locking (`SELECT … FOR UPDATE SKIP LOCKED`).
3. Only consider a real queue (Option 3) when the outbox throughput becomes
   a Postgres bottleneck — typically not until thousands of notifications
   per minute.
