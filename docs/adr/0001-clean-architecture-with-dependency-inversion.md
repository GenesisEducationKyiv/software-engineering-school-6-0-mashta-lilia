# ADR 0001: Layered Architecture with Dependency Inversion

Date: 2026-05-08
Status: Accepted
Deciders: Project Author

## Context and Problem Statement

The service has three concerns that historically tend to bleed into each other
in small Go projects: HTTP transport, business logic, and persistence. When
they share types and call each other directly, swapping a backend (e.g.,
PostgreSQL → SQLite for tests) forces touching the entire codebase, and unit
tests become coupled to network and database I/O.

We need a structure that lets the business logic be tested in isolation, lets
the persistence layer be tested against a real database without booting HTTP,
and tolerates future changes (replacing SMTP with a SaaS, swapping Chi for
another router).

## Decision Drivers

* Business logic must be unit-testable without a database, HTTP server, or
  network access.
* Repository code must be testable against real PostgreSQL (integration tests)
  without dragging in HTTP handlers.
* The transport layer (HTTP/REST) must be replaceable without changing
  business logic.
* The team is one person — the architecture must not impose ceremony beyond
  what the codebase actually benefits from.

## Considered Options

* Option 1: **Layered architecture with dependency inversion** — `service/`
  defines interfaces; `repository/`, `client/`, `api/` provide implementations.
* Option 2: Direct calls — handlers call repositories, repositories call DB,
  no interfaces.
* Option 3: Full Hexagonal/Ports-and-Adapters with explicit "ports" and
  "adapters" packages.
* Option 4: DDD with aggregates, value objects, domain events.

## Decision Outcome

Chosen option: **Option 1**, because it gives the testability benefits of
hexagonal architecture without the ceremony of separate `ports/` and
`adapters/` packages.

The structure is:

```
internal/
  service/           ← business logic + interfaces.go (defines contracts)
  repository/        ← PostgreSQL implementations of SubscriptionRepo, RepoStore
  client/github/     ← HTTP implementations of GitHubClient (+ Redis decorator)
  client/mailer/     ← SMTP implementation of Mailer
  api/rest/          ← HTTP handlers calling service.SubscriptionService
```

The dependency arrow points **inward**: outer layers (`api`, `repository`,
`client`) import `service`, never the reverse. `service/interfaces.go`
declares the four contracts (`SubscriptionRepo`, `RepoStore`, `GitHubClient`,
`Mailer`) the service depends on.

### Consequences

* Good, because the entire `service/` package is unit-tested with in-memory
  mocks (see `internal/service/scanner_test.go`). Zero database, zero network.
* Good, because Redis caching was added as a decorator (`CachedClient` wraps
  `*Client`, both implementing `GitHubClient`) without touching service logic
  — see [ADR 0004](0004-redis-cache-aside-as-decorator.md).
* Good, because the repository layer has its own integration tests using
  `testcontainers-go`, isolated from HTTP concerns.
* Bad, because every new contract requires an interface declaration even when
  there's only one implementation today (mild boilerplate).
* Bad, because for trivial CRUD the indirection adds friction with no
  immediate payoff.

## Pros and Cons of the Options

### Layered + dependency inversion

* + Testability without infrastructure.
* + Decorator pattern works naturally (caching, retries, metrics).
* + Idiomatic Go — interfaces declared by consumers, not producers.
* − Slight boilerplate for single-implementation contracts.

### Direct calls (no interfaces)

* + Fastest to write initially.
* + Less indirection when reading code.
* − Service tests require a real DB or full mocks of `*sql.DB`, neither
  pleasant.
* − Adding caching requires modifying the call sites.

### Full Hexagonal (`ports/`, `adapters/`)

* + Explicit naming makes intent very clear in large codebases.
* − For a single-author project this is ceremony without payoff.
* − Doubles the package count.

### DDD aggregates

* + Excellent for complex domains with invariants spanning many entities.
* − This domain has two entities and trivial invariants. Overkill.
