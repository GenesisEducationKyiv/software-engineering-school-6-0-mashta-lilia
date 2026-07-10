# ADR 0016: Asynchronous Notifications via RabbitMQ

Date: 2026-06-21
Status: Accepted
Deciders: Project Author
Supersedes: the transport half of [ADR 0015](0015-sync-grpc-and-dedup-ledger.md)

## Context and Problem Statement

[ADR 0015](0015-sync-grpc-and-dedup-ledger.md) chose synchronous unary gRPC for
the monolith-to-notifier hop and explicitly deferred the "asynchronous
outbox/event bus" option. That coupling means a subscribe request blocks on SMTP
latency, and a notifier outage surfaces as user-facing `5xx` and subscription
rollbacks even though the work could simply wait.

We now want the monolith to hand off notification work and return immediately,
and we want a notifier restart to lose no work.

## Considered Options

* Keep synchronous gRPC (status quo from ADR 0015).
* Publish commands to a durable message broker; the notifier consumes them.
* Transactional outbox in the monolith DB plus a relay.

## Decision Outcome

Publish **commands** to RabbitMQ. The monolith's `SendConfirmation` and
`SendReleaseNotification` ports are now backed by a publisher
(`internal/client/notification`) that emits a JSON envelope to a durable
`direct` exchange; the notifier runs a consumer (`services/notification/consumer`)
that decodes the envelope and dispatches to the same application `Service` used
before.

* **Contract** (`internal/notifyevent`): `Envelope{ type, trace_id, payload }`
  carrying a `ConfirmationCommand` or `ReleaseCommand`. One shared package keeps
  publisher and consumer from drifting; `trace_id` preserves log correlation
  across the async hop (replacing the gRPC-metadata propagation of ADR 0015).
* **Topology** (`internal/messaging`): durable exchange `notifications`, durable
  queue `notifications.email` bound by command type. Both sides declare it
  idempotently, so a command published before the consumer starts is not lost.
* **Settlement**: manual ack, prefetch 16. Ack on success; **drop** (nack, no
  requeue) permanently bad input — malformed JSON, unknown type, missing fields
  — so a poison message cannot loop; **requeue** on transient send failures.

We keep the gRPC server in the notifier as the container health surface and the
in-process transport for the integration test harness. The broker is the
production path.

### Why redelivery is safe (idempotent consumer)

A broker that requeues gives at-least-once delivery, which would normally risk
duplicate emails — the exact thing ADR 0015's dedup ledger exists to prevent.
The ledger makes redelivery safe, but a plain reserve-before-send ledger (the
original ADR 0015 design) is *too* safe: it also swallows the requeue this ADR
relies on for transient send failures, since a redelivered command would always
find its row already reserved and get acked as a no-op without ever reaching
SMTP again — the send is silently lost, not retried.

The ledger therefore distinguishes reserved-but-unconfirmed from confirmed:
`sent_notifications.sent_at` is set only after the send actually succeeds
(`Service.reserveAndSend` calls `dedup.Confirm` post-send). A redelivery of a
command whose send never got that far re-reserves the same row and genuinely
retries SMTP; a redelivery of an already-confirmed send finds `sent_at` set and
is acked as a true dedup no-op. This preserves the spirit of the at-most-once
product semantics of [ADR 0007](0007-persist-before-notify-for-at-most-once.md)
— duplicates are still the rare, narrow case — while making the broker's
retry-on-transient-failure behavior real instead of a documented no-op. See
ADR 0015's "Failure Window" section for the narrow duplicate-risk window this
trades in.

### Consequences

* Good, because the monolith no longer blocks on SMTP and is decoupled from
  notifier availability — a notifier outage drains from the durable queue on
  recovery instead of failing user requests.
* Good, because the existing dedup ledger already makes the consumer idempotent,
  so at-least-once redelivery needs no new machinery.
* Good, because publisher and consumer share one wire contract package.
* Bad, because there is now a broker to operate, and "the email was accepted" no
  longer means "the email was queued at SMTP" synchronously — observability
  moves to consumer logs and the queue.
* Bad, because dropped (permanently bad) messages are discarded rather than
  parked; a dead-letter queue is a natural future addition.
