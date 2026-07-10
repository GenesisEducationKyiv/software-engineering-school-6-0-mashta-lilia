# ADR 0015: Synchronous gRPC with Dedup Ledger

Date: 2026-06-10
Status: Accepted
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

Reserve-then-send preserves at-most-once, but it has a known miss window: if the
notifier inserts the ledger row and then SMTP or the monolith-to-notifier
connection fails, the row remains. The notification will not be retried
automatically, so a release email can be missed but not duplicated. This is the
same product trade-off as ADR 0007.

Confirmations are keyed by fresh subscription tokens, so a user retry creates a
new `confirm:{token}` key and can send another confirmation. Release
notifications are the deliberately deduped at-most-once path.

### Consequences

* Good, because duplicate release emails are prevented by the notifier's own DB.
* Good, because the monolith keeps a simple synchronous control flow.
* Good, because the gRPC proto is the cross-service contract.
* Bad, because transport errors cannot always prove whether SMTP delivery
  happened after a reservation.
* Bad, because the current ledger has no `PENDING` or `FAILED` state. A future
  upgrade can add statuses and a retry worker if the product wants fewer misses.
