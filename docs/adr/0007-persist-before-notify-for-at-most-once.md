# ADR 0007: Persist `last_seen_tag` Before Sending Notifications

Date: 2026-05-08
Status: Accepted
Deciders: Project Author

## Context and Problem Statement

When the release poller detects a new release, it must do two things:

1. **Persist** the new tag in `tracked_repositories.last_seen_tag` so the
   release is not detected again on the next poll.
2. **Notify** all active subscribers via email.

These two operations are not part of a single transaction (one is a Postgres
write, the other is a sequence of SMTP calls). If the process crashes between
them, the system enters an inconsistent state.

The order determines whether the system is **at-most-once** (some users miss a
notification on crash) or **at-least-once** (some users get duplicates on
crash). UX cost is asymmetric: a missed notification is minor (user sees the
release later); a duplicate feels like spam and drives unsubscribes. Crashes
are rare but real (deploys, OOM kills, host failures), and no external
transactional layer (XA, DTM) is available.

## Considered Options

* Option 1: **Persist `last_seen_tag` first, then send emails**
  (at-most-once on crash).
* Option 2: Send emails first, then persist (at-least-once on crash). Rejected:
  duplicates on every crash; users react worse to "got it twice" than "got it
  never" and unsubscribe.
* Option 3: Per-recipient outbox row written transactionally, then drained by a
  worker (true at-least-once with idempotency key). Rejected: right answer at
  scale, over-engineered now — schema additions, worker process, per-recipient
  key.

## Decision Outcome

Chosen option: **Option 1 — persist first, notify second**, because the
asymmetry of UX cost strongly favours the at-most-once side.

Implementation (`internal/release/poller.go`):

```go
// 1. Update last_seen_tag FIRST
if err := p.repos.UpdateLastSeen(ctx, repo.ID, release.TagName); err != nil {
    p.log.Error(...)
    return
}

// 2. THEN fan out emails (failures are logged, do not roll back the update)
for _, email := range emails {
    if err := p.mailer.SendReleaseNotification(...); err != nil {
        p.log.Error(...)
    }
}
```

If the process crashes between steps 1 and 2, on the next poll the new tag is
already stored — the release is not detected again, and unsent notifications
are simply lost. Manual recovery (force re-detection by rewinding
`last_seen_tag`) is an ops runbook item, not a code path; see the operator
notes alongside the poller deployment docs.

### Consequences

* Good, because **no user ever receives a duplicate notification** under any
  single-instance crash scenario.
* Good, because the implementation is straightforward — no outbox table, no
  background worker, no idempotency key.
* Good, because the failure case is observable in logs (errored emails are
  per-recipient logged) and can be re-driven manually if catastrophic.
* Bad, because users on a partial-fan-out crash silently miss that release; no
  automatic recovery exists.
* Bad, because under multi-instance deployment, two pollers hitting the same
  repo could each see the old `last_seen_tag` and duplicate work — the
  in-process mutex does not protect this. Out of scope today (single-instance
  deployment); the natural fix is row-level locking
  (`SELECT … FOR UPDATE SKIP LOCKED`) when we need it.
