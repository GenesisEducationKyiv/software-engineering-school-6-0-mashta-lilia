# ADR 0008: Partial Unique Index to Allow Re-subscription After Unsubscribe

Date: 2026-05-08
Status: Accepted
Deciders: Project Author

## Context and Problem Statement

The product rule is: **"a given email address can have at most one
non-terminal subscription per repo"**, where non-terminal means `pending` or
`active`. After a user unsubscribes, they are allowed to re-subscribe to the
same repo, and the unsubscribed history must be preserved (it is the audit
trail of consent, needed for GDPR-style "prove the user consented at time T").

A naïve `UNIQUE(email, repo_owner, repo_name)` constraint enforces uniqueness
across **all** rows, including unsubscribed ones — so re-subscription would
fail with a constraint violation even though the previous subscription is no
longer active. Soft-deleting (a `deleted_at` column) doesn't fix this on its
own; uniqueness still applies.

The rule must be enforced in the database, not application code, to eliminate
the TOCTOU race where two concurrent subscribe requests both pass a
`SELECT … LIMIT 1` check and both `INSERT`.

## Considered Options

* Option 1: **Partial unique index** with predicate `WHERE status != 'unsubscribed'`.
* Option 2: Hard delete on unsubscribe. Rejected: loses the audit trail —
  cannot answer "did this user ever subscribe?" — and cascade delete has
  surprising effects on related rows.
* Option 3: Application-level uniqueness check. Rejected: TOCTOU race (see
  Context), and every code path that creates a subscription must remember the
  check.
* Option 4: Separate history table. Rejected: doubles the schema and forces an
  INSERT-here + DELETE-there transactional dance for a problem the partial
  index already solves.

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

The index also supports the `Exists()` lookup in the subscription service that
prevents creating a second active subscription on a duplicate request.

### Consequences

* Good, because the database **rejects** racing duplicate INSERTs at the
  storage layer. The application cannot accidentally create two active
  subscriptions even under concurrent load.
* Good, because unsubscription history is preserved indefinitely, and the rule
  lives in one DDL statement rather than scattered across application code.
* Bad, because the uniqueness predicate is PostgreSQL-specific. MySQL does not
  support partial indexes (would require a workaround such as a generated
  column that is `NULL` for unsubscribed rows).
* Bad, because unsubscribed rows accumulate without bound. A future cleanup
  job (e.g., delete `unsubscribed` rows older than 1 year) is the natural
  evolution but is not implemented.
* Bad, because re-subscription creates a **new** row, not a status update. The
  UI / API must handle the new row's `id` and `token`; the old unsubscribed
  row remains as history.

## Related

* The composite FK `(repo_owner, repo_name) → tracked_repositories(owner, name)`
  ensures every subscription points to a real tracked repo
  (see `migrations/000001_init_schema.up.sql`).
* The status state machine (`pending → active → unsubscribed`) is enforced by
  a `CHECK` constraint, not by application code.
