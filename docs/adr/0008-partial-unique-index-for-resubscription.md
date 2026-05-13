# ADR 0008: Partial Unique Index to Allow Re-subscription After Unsubscribe

Date: 2026-05-08
Status: Accepted
Deciders: Project Author

## Context and Problem Statement

The product rule is: **"a given email address can have at most one
non-terminal subscription per repo"**, where non-terminal means `pending` or
`active`. After a user unsubscribes, they are allowed to re-subscribe to the
same repo, and the unsubscribed history must be preserved (it is the
audit trail of consent).

A naïve `UNIQUE(email, repo_owner, repo_name)` constraint enforces uniqueness
across **all** rows, including unsubscribed ones — so re-subscription would
fail with a constraint violation even though the previous subscription is
no longer active. Soft-deleting (a `deleted_at` column) doesn't fix this on
its own; uniqueness still applies.

## Decision Drivers

* Preserve unsubscription history for auditability and GDPR-style "right to
  prove the user consented at time T."
* Allow unbounded re-subscription cycles per (email, repo) pair.
* Enforce the uniqueness rule in the database, not in application code,
  to eliminate TOCTOU races (two concurrent subscribe requests creating two
  rows).

## Considered Options

* Option 1: **Partial unique index** with the predicate
  `WHERE status != 'unsubscribed'`.
* Option 2: Hard delete on unsubscribe — remove the row entirely.
* Option 3: Application-level uniqueness check (`SELECT … WHERE … LIMIT 1`
  before `INSERT`).
* Option 4: Move unsubscribed rows to a separate history table.

## Decision Outcome

Chosen option: **Option 1 — partial unique index**, because PostgreSQL
supports the construct natively, and the predicate cleanly captures the
business rule in one place.

```sql
CREATE UNIQUE INDEX idx_subscriptions_email_repo_active
    ON subscriptions(email, repo_owner, repo_name)
    WHERE status != 'unsubscribed';
```

This means:
* Two `pending`/`active` rows for the same (email, repo) → violates uniqueness.
* Any number of `unsubscribed` rows for the same (email, repo) → allowed.
* Adding an `unsubscribed` row does not conflict with a fresh
  `pending`/`active` row inserted later.

The index also supports the `Exists()` lookup in the subscription service
that prevents creating a second active subscription on a duplicate request.

### Consequences

* Good, because the database **rejects** racing duplicate INSERTs at the
  storage layer. The application cannot accidentally create two
  active subscriptions even under concurrent load.
* Good, because unsubscription history is preserved indefinitely. Audit
  trail is intact.
* Good, because the rule lives in one DDL statement, not scattered across
  application code.
* Bad, because the uniqueness predicate is now PostgreSQL-specific. MySQL
  does not support partial indexes (would require a workaround such as a
  generated column that is `NULL` for unsubscribed rows).
* Bad, because unsubscribed rows accumulate without bound. A future cleanup
  job (e.g., delete `unsubscribed` rows older than 1 year) is the natural
  evolution but is not implemented.
* Bad, because re-subscription creates a **new** row, not a status update.
  The UI / API must handle the new row's `id` and `token`; the old
  unsubscribed row remains as history.

## Pros and Cons of the Options

### Partial unique index

* + Atomic, race-free, expressed in DDL.
* + Free (no extra table, no extra column).
* − Postgres-specific.

### Hard delete on unsubscribe

* + Simpler index (`UNIQUE(email, repo_owner, repo_name)`).
* − Loses audit trail. Cannot answer "did this user ever subscribe?".
* − Cascade delete may have surprising effects on related rows (audit logs,
  analytics).

### Application-level uniqueness check

* + Database-portable.
* − TOCTOU race: two concurrent subscribes both pass the check, both INSERT.
* − Forces every code path that creates a subscription to remember the
  check.

### Separate history table

* + Clear separation: `subscriptions` is "live", `subscription_history` is
  audit.
* + Bounded growth in the live table.
* − Two tables to query for "has this user ever subscribed".
* − INSERT on unsubscribe + DELETE from live = transactional dance.
* − Doubles the schema for a problem the partial index already solves.

## Related

* The composite FK `(repo_owner, repo_name) → tracked_repositories(owner, name)`
  ensures every subscription points to a real tracked repo
  (see `migrations/000001_init_schema.up.sql`).
* The status state machine (`pending → active → unsubscribed`) is enforced
  by a `CHECK` constraint, not by application code.
