# ADR 0001: Layered Architecture with Dependency Inversion

Date: 2026-05-08
Status: Accepted
Deciders: Project Author

## Context and Problem Statement

The service has three concerns that historically bleed into each other in small
Go projects: HTTP transport, business logic, and persistence. When they share
types and call each other directly, swapping a backend (e.g., PostgreSQL →
SQLite for tests) forces touching the entire codebase, and unit tests become
coupled to network and database I/O.

We need a structure that lets the business logic be tested in isolation, lets
the persistence layer be tested against a real database without booting HTTP,
and tolerates future changes (replacing SMTP with a SaaS, swapping Chi for
another router) — without imposing ceremony beyond what a single-author
codebase benefits from.

## Considered Options

* Option 1: **Layered architecture with dependency inversion** — `service/`
  defines interfaces; `repository/`, `client/`, `api/` provide implementations.
* Option 2: Direct calls — handlers call repositories, repositories call DB,
  no interfaces. Rejected: service tests would require a real DB or full
  `*sql.DB` mocks; adding caching would mean modifying call sites.
* Option 3: Full Hexagonal with explicit `ports/` and `adapters/` packages.
  Rejected: ceremony without payoff for a single-author project; doubles the
  package count.
* Option 4: DDD with aggregates, value objects, domain events. Rejected: two
  entities and trivial invariants — overkill.

## Decision Outcome

Chosen option: **Option 1**, because it gives the testability benefits of
hexagonal architecture without separate `ports/` and `adapters/` packages.

The structure is:

```
internal/
  subscription/      ← Subscription domain: Service, Subscription type, errors
  release/           ← Release domain: Poller, Release + TrackedRepository types
  email/             ← email.Address value object
  repo/              ← repo.Ref value object
  token/             ← token.Generator (crypto/rand → hex)
  platform/health/   ← health.DBChecker
  app/               ← bootstrap (was cmd/server/main.go)
  repository/        ← PostgreSQL adapter
  client/github/     ← GitHub HTTP client (+ Redis-cached decorator)
  client/mailer/     ← SMTP transport + email templates
  api/rest/          ← chi router + subscription/, health/, middleware/ sub-pkgs
```

The dependency arrow points **inward**: outer layers (`api`, `repository`,
`client`) import the domain packages (`subscription`, `release`), never the
reverse. Each consumer declares the small unexported interface it needs;
production wiring in `internal/app/deps.go` satisfies them with concrete
adapter types via Go's structural typing
(see [ADR 0009](0009-consumer-side-interface-placement.md)).

### Consequences

* Good, because every domain package is unit-tested with in-memory mocks
  (see `internal/release/poller_test.go`). Zero database, zero network.
* Good, because Redis caching was added as a decorator (`CachedClient` wraps
  `*Client`, both satisfying the consumer-side `githubClient` interface in
  `internal/app/deps.go`) without touching domain logic.
* Good, because the repository layer has its own integration tests using
  `testcontainers-go`, isolated from HTTP concerns.
* Bad, because every new contract requires an interface declaration even with
  one implementation today, and for trivial CRUD the indirection adds friction
  with no immediate payoff.
