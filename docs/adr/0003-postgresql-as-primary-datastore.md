# ADR 0003: PostgreSQL as the Primary Datastore

Date: 2026-05-08
Status: Accepted
Deciders: Project Author

## Context and Problem Statement

The service has two persistent entities (`subscriptions`, `tracked_repositories`)
related by a composite foreign key, with a multi-state status workflow
(`pending → active → unsubscribed`) and a uniqueness constraint that depends
on row state ("only one active subscription per email+repo, but unbounded
unsubscribed history").

Read patterns are point lookups (by `token`, by `email`, by `repo`) and a
small full-table scan in the background scanner. Write volume is bounded by
human subscription rate (low) plus scanner updates (one per repo per scan).
There is no temporal/OLAP need, no full-text search, and no document or graph
shape.

## Decision Drivers

* Need for a partial unique index conditional on `status` — see
  [ADR 0008](0008-partial-unique-index-for-resubscription.md).
* Composite foreign key `subscriptions(repo_owner, repo_name) →
  tracked_repositories(owner, name)` with `ON DELETE CASCADE`.
* Single-binary deployment; one DB instance is sufficient at projected scale.
* Operational familiarity — Postgres is widely understood by engineers
  reading this codebase.

## Considered Options

* Option 1: **PostgreSQL** with `lib/pq` and `golang-migrate`.
* Option 2: SQLite (embedded, single-file).
* Option 3: MongoDB or another document store.
* Option 4: A managed key-value store (DynamoDB, Redis as primary).

## Decision Outcome

Chosen option: **Option 1 — PostgreSQL**, because the data model is
relational (composite FK, partial unique indexes, CHECK constraints,
triggers), and Postgres handles every requirement with mature, documented
features. Two of the schema features that justify Postgres specifically:

* `CREATE UNIQUE INDEX ... WHERE status != 'unsubscribed'` — partial
  unique indexes are not an SQL standard feature; SQLite supports them, MySQL
  does not.
* `BEFORE UPDATE` trigger calling a `plpgsql` function to maintain
  `updated_at` server-side, eliminating timestamp drift from forgotten
  application updates.

Driver: `github.com/lib/pq` (chosen over `pgx` for its `database/sql`
compatibility — it interoperates with `golang-migrate`'s sql-driver family
without an extra adapter).

Connection pooling: `MaxOpenConns=25`, `MaxIdleConns=10`,
`ConnMaxLifetime=5m`, `ConnMaxIdleTime=1m` (see
`internal/repository/postgres.go`). At Postgres's default
`max_connections=100` this leaves headroom for psql shells and migration
tools.

### Consequences

* Good, because partial unique indexes encode a complex business rule at the
  database layer instead of in application code (where it would be subject
  to TOCTOU races).
* Good, because `golang-migrate` provides a battle-tested up/down migration
  workflow with version tracking.
* Good, because `testcontainers-go` allows integration tests to run against
  a real Postgres in CI, catching driver-specific bugs.
* Bad, because adds a runtime dependency — the service cannot start without
  Postgres reachable at boot (`PingContext` fails fast — see
  `internal/repository/postgres.go`).
* Bad, because `lib/pq` is in maintenance mode; if performance becomes a
  bottleneck, migrating to `pgx/v5` is a follow-up cost.

## Pros and Cons of the Options

### PostgreSQL

* + Partial indexes, CHECK constraints, triggers, composite FKs all native.
* + Excellent ecosystem — migrations, ORMs, observability.
* − Operational overhead vs. embedded options.

### SQLite

* + Zero-ops, single file, supports partial indexes and triggers.
* − No `TIMESTAMPTZ` type; timezone handling is cumbersome.
* − Single-writer; concurrency model differs from Postgres.
* − Not what the production target environment uses; testing would diverge.

### MongoDB / document store

* + Schema-less, fast prototyping.
* − Composite FK enforcement and partial unique indexes do not map cleanly.
* − Two-document transactions exist but are complex compared to a single
  Postgres `INSERT … ON CONFLICT`.

### Key-value store (DynamoDB, Redis as primary)

* + Horizontal scale comes free.
* − No relational integrity. The two-entity model with CASCADE would have
  to be rebuilt in application code.
* − Querying "all active subscribers for repo X" requires a secondary
  index, with operational complexity.
