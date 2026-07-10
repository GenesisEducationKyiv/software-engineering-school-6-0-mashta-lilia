# ADR 0015: Synchronous gRPC with Dedup Ledger

Date: 2026-06-10
Status: Accepted — transport superseded by [ADR 0016](0016-async-notifications-via-rabbitmq.md); the reserve-then-send ledger below was refined to a reserve/confirm ledger once ADR 0016 added broker redelivery (see that ADR's "Why redelivery is safe" section)
Deciders: Project Author

## Context and Problem Statement

After extracting notification, the monolith needs a delivery contract. The
existing product semantics are at-most-once for release notifications: duplicate
emails are worse than missed emails. The notifier also needs its own database
because service-owned data must not live in the monolith schema.

We need to choose how the monolith asks the notifier to send email, how the
notifier prevents duplicates, and what failures mean to callers.

## Considered Options

* Synchronous gRPC call plus notifier-owned dedup ledger.
* Asynchronous outbox/event bus with notifier consumers.
* Keep SMTP in-process and only move templates.

## Decision Outcome

Use synchronous unary gRPC. The notifier first reserves a ledger row in
`sent_notifications` using a unique `dedup_key`, then sends the email only when
the insert wins. A duplicate key returns a successful business response with
`delivered=false`.

Dedup keys are stored as the sha256 hex digest of a logical key:

* confirmations: `sha256("confirm:{token}")`
* release notifications: `sha256("release:{repo}:{tag}:{email}")`

Hashing fixes the column width at 64 chars (no varchar overflow however long
the repo, tag, or email gets) and keeps confirmation tokens and subscriber
emails out of the ledger key, log fields, and error strings that cross the
gRPC boundary into monolith logs.

The monolith distinguishes transport errors from business no-ops. Any non-OK
gRPC status is returned as an error. A successful response with
`delivered=false` is logged as a dedup no-op and is not treated as failure.
This preserves subscription rollback behavior for real transport/send failures
while making duplicate delivery idempotent from the caller's point of view.

Trace IDs are propagated over gRPC metadata (`x-request-id` and `traceparent`).
The notifier's server interceptor re-injects the trace ID into `context.Context`
so structured logs keep correlation across the network boundary.

### Failure Window

Reserve-then-send originally preserved at-most-once with a known miss window:
if the notifier inserted the ledger row and then SMTP or the
monolith-to-notifier connection failed, the row remained and the notification
was never retried, so a release email could be missed but not duplicated. This
was the same product trade-off as ADR 0007.

**Update (ADR 0016):** once notification commands started flowing through a
requeueing broker, that miss window silently defeated the broker's own retry —
a redelivery after an SMTP failure would find the row already reserved and get
acked as a dedup no-op without ever resending. The ledger now has the `PENDING`
/ `FAILED` state this ADR originally deferred: `sent_at` is set only when
`Confirm` runs after a successful send, so an unconfirmed row is retried on
redelivery and only a confirmed row is treated as a true duplicate. See ADR
0016's "Why redelivery is safe" section for the current mechanics.

Confirmations are keyed by fresh subscription tokens, so a user retry creates a
new `confirm:{token}` key and can send another confirmation. Release
notifications are the deliberately deduped at-most-once (now: at-least-once
until confirmed) path.

### Consequences

* Good, because duplicate release emails are prevented by the notifier's own DB.
* Good, because the monolith keeps a simple synchronous control flow.
* Good, because the gRPC proto is the cross-service contract.
* Bad, because transport errors cannot always prove whether SMTP delivery
  happened after a reservation.
* Bad, because the reserve/confirm split narrows but does not close the
  duplicate risk: a crash or DB failure between a successful send and the
  `Confirm` write leaves the row unconfirmed, so a redelivery can resend. This
  trades a (smaller) duplicate risk for the missed-notification risk described
  above; see ADR 0016.
