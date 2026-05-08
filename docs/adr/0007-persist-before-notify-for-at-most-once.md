# ADR 0007: Persist `last_seen_tag` Before Sending Notifications

Date: 2026-05-08
Status: Accepted
Deciders: Project Author

## Context and Problem Statement

When the scanner detects a new release, it must do two things:

1. **Persist** the new tag in `tracked_repositories.last_seen_tag` so the
   release is not detected again on the next scan.
2. **Notify** all active subscribers via email.

These two operations are not part of a single transaction (one is a Postgres
write, the other is a sequence of SMTP calls). If the process crashes between
them, the system enters an inconsistent state.

The order of these operations determines whether the system is **at-most-once**
(some users may miss a notification on crash) or **at-least-once** (some
users may receive duplicates on crash).

## Decision Drivers

* User-experience cost of a missed notification: minor — the user notices
  on the next release, possibly via the project's own release page.
* User-experience cost of a duplicate notification: high — duplicates feel
  like spam and lead directly to unsubscribes.
* Crashes are rare but real (deploys, OOM kills, host failures). The
  system must behave correctly when they happen, not assume they don't.
* No external transactional layer is available (no XA, no distributed
  transaction manager).

## Considered Options

* Option 1: **Persist `last_seen_tag` first, then send emails**
  (at-most-once on crash).
* Option 2: Send emails first, then persist `last_seen_tag`
  (at-least-once on crash).
* Option 3: Per-recipient outbox row written transactionally, then drained
  by a worker (true at-least-once with deduplication key).

## Decision Outcome

Chosen option: **Option 1 — persist first, notify second**, because the
asymmetry of UX cost ("missed" vs. "duplicate") strongly favours the
at-most-once side.

Implementation (`internal/service/scanner.go`):

```go
// 1. Update last_seen_tag FIRST
if err := s.repos.UpdateLastSeen(ctx, repo.ID, release.TagName); err != nil {
    slog.Error(...); continue
}

// 2. THEN fan out emails (failures are logged, do not roll back the update)
for _, email := range emails {
    if err := s.mailer.SendReleaseNotification(...); err != nil {
        slog.Error(...)
    }
}
```

If the process crashes between steps 1 and 2, on the next scan the new tag
is already stored — the release is not detected again, and unsent
notifications are simply lost.

### Consequences

* Good, because **no user ever receives a duplicate notification** under
  any single-instance crash scenario.
* Good, because the implementation is straightforward — no outbox table, no
  background worker, no idempotency key.
* Good, because the failure case is observable in logs (errored emails are
  per-recipient logged) and can be re-driven manually if catastrophic.
* Bad, because users on a partial-fan-out crash silently miss that release.
  No automatic recovery exists.
* Bad, because under multi-instance deployment, two scanners hitting the
  same repo simultaneously could each see the old `last_seen_tag` and
  duplicate the work — the in-process mutex does not protect this. Out of
  scope today (single-instance deployment); see
  [ADR 0006](0006-synchronous-email-fan-out.md) future direction.

## Pros and Cons of the Options

### Persist first (at-most-once)

* + No duplicates ever.
* + Simplest code.
* − Missed notifications are silent on crash.

### Notify first (at-least-once)

* + No missed notifications on crash.
* − Duplicates on every crash. UX-hostile.
* − In practice the user reaction to "got it twice" is worse than the
  reaction to "got it never" — they unsubscribe.

### Outbox + idempotency

* + Both no-loss and no-duplicate guarantees, with proper keys.
* − Requires schema additions, worker process, idempotency key per
  (release, recipient).
* − Right answer at scale; over-engineered now.

## Recovery Procedure

If a crash leaves a release partially fan-out, an operator can:

1. Identify the offending tag (`SELECT owner, name, last_seen_tag,
   last_checked_at FROM tracked_repositories WHERE last_checked_at > <crash time>`).
2. Manually `UPDATE tracked_repositories SET last_seen_tag = <previous tag>`
   to force re-detection on the next scan.
3. Accept that all subscribers will be re-notified, including those who
   received the email before the crash.

This is intentional: forcing the operator to opt in to the duplicate path
keeps the default semantics clean.
