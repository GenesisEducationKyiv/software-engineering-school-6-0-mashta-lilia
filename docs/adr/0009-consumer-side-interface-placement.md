# ADR 0009: Consumer-Side Interface Placement

Date: 2026-05-12
Status: Accepted
Deciders: Project Author

## Context and Problem Statement

ADR 0001 commits the project to "interfaces declared by consumers, not
producers" as a one-line consequence of layered architecture. In practice
this rule has subtleties that left the codebase inconsistent until a
post-review pass:

1. `SubscriptionService` (in `service/`) consumes a database via
   `SubscriptionRepo`. The interface lives in `service/`. Consumer-side ✓.
2. `Handler` (in `rest/`) consumes business logic via `SubscriptionUseCase`.
   The interface lives in `rest/`. Consumer-side ✓.
3. `Router` (in `rest/`) consumes a liveness probe via `HealthChecker`.
   The interface initially lived in `service/` — **provider-side**, breaking
   the rule for no documented reason.
4. `CachedClient` (in `client/github/`) consumes a GitHub-shaped client to
   delegate cache misses. The wrapped contract was typed as
   `service.GitHubClient` — an upstream import from an adapter package, again
   breaking the rule.

Two competing patterns in the same codebase left future contributors with
no way to predict where a *new* interface should live. The choice
between sharing a single canonical interface (DRY) and keeping local
interfaces per consumer (decoupling) is a real trade-off and worth being
explicit about.

## Decision Drivers

* **Predictability.** A single rule a future contributor can apply without
  re-reading the codebase or polling the author.
* **Package autonomy.** Leaf adapter packages (`client/github`,
  `client/mailer`, `repository`) should not import business packages just
  to name an interface type. They are the bottom of the dependency graph
  and should stay there.
* **Drift prevention.** When the same contract is declared in two places
  with no compile-time link, signatures can silently fall out of sync.
* **Compatibility with Go's structural typing.** Choices should not require
  explicit `implements` declarations or registries.

## Considered Options

* Option 1: **Strict consumer-side, including inside adapters.** Every
  package that *calls* methods declares the interface locally, even when
  another package has an equivalent interface. Decorators like `CachedClient`
  define their own `baseClient` rather than importing
  `service.GitHubClient`.
* Option 2: **Provider-side for cross-layer contracts.** Interfaces consumed
  across layers live in the lowest layer that anyone might consume from.
  `service.HealthChecker`, `service.GitHubClient`, `service.SubscriptionRepo`
  all in `service/`, regardless of who imports them.
* Option 3: **Mixed by judgment** (the state this ADR is replacing).
* Option 4: **Central `ports/` package.** All cross-package interfaces in
  `internal/ports/`. Neither package has to declare or import contracts.

## Decision Outcome

Chosen option: **Option 1 — strict consumer-side**, with one explicit
narrowing for clarity:

> An interface belongs in the package that **calls** its methods.
> When the consumer is itself a Go package containing multiple types that
> share the same dependency (e.g., both `SubscriptionService` and `Scanner`
> in `service/` consume `GitHubClient`), the package as a whole is the
> consumer and the interface lives there. When the consumer is a single
> wrapper/adapter (e.g., `CachedClient` decorating a GitHub client), the
> adapter declares its own local interface and does not import the
> upstream package.

Concretely after this ADR:

| Interface | Lives in | Consumer |
|---|---|---|
| `SubscriptionRepo`, `RepoStore`, `GitHubClient`, `Mailer` (and ISP role splits) | `service/` | `service` package types |
| `SubscriptionUseCase` | `rest/` | `rest.Handler` |
| `HealthChecker` | `rest/` | `rest.healthHandler` |
| `baseClient` (in `cached.go`) | `client/github/` | `github.CachedClient` |
| `TokenGenerator` | `service/` | `service.SubscriptionService` |
| `RetryStrategy` | `client/github/` | `github.Client` |

The drift risk for the adapter case (`baseClient` vs `service.GitHubClient`)
is bounded by the composition root in `cmd/server/main.go`: both
`*github.Client` and `*github.CachedClient` are assigned to a
`service.GitHubClient`-typed variable, so any signature divergence between
the two interfaces becomes a compile error at wiring time, not a silent
runtime gap.

### Consequences

* Good, because the rule answers "where does my new interface go?"
  unambiguously.
* Good, because adapter packages (`client/github`, `client/mailer`,
  `repository`) have no upstream imports beyond `model/`. They are leaf
  packages in the dependency graph and stay extractable.
* Good, because each test depends on the narrowest interface its target
  actually uses — mocks stay focused, ISP applies naturally.
* Good, because reading any file makes its dependencies explicit: every
  interface in a file is something *that file* uses.
* Bad, because `CachedClient.baseClient` and `service.GitHubClient` are
  effectively duplicates of the same two-method contract. A linter cannot
  enforce them in sync.
* Bad, because the rule has a narrowing ("package as a whole vs single
  adapter") that requires reading this ADR to apply correctly to edge
  cases.

## Pros and Cons of the Options

### Option 1 — strict consumer-side

* + Predictable: one rule, one place per interface.
* + Adapter packages stay leaves in the import graph.
* + Idiomatic Go.
* + Tests in each package only need to mock methods that package actually
  uses.
* − Two-method drift between `baseClient` and `service.GitHubClient` is
  technically possible (caught at wiring time, not statically).

### Option 2 — provider-side for cross-layer

* + No drift: single source of truth for each contract.
* + Conventional in classical "ports & adapters" terminology.
* − `rest/` imports `service/` solely to name `HealthChecker`. `client/github/`
  imports `service/` solely to name `GitHubClient`. Adapter packages
  acquire upstream business-package dependencies they don't need.
* − Inverts the Go consumer-side idiom partially — readers see two
  competing patterns and can't predict which one applies next.

### Option 3 — mixed by judgment

* + Lowest friction per individual decision.
* − No rule means new contributors guess, miss, and produce more drift
  than Option 1.
* − This is the state this ADR replaces.

### Option 4 — central `ports/` package

* + No coupling, no drift, no judgment calls.
* − One extra package for ~10 interfaces in a project this size.
* − Conflicts with Go's "package = boundary of related code" idiom; the
  `ports/` package becomes a kitchen sink.
* − Reading any consumer requires a second hop to `ports/` to see what it
  depends on.

## Relation to Other ADRs

This ADR refines, but does not supersede, **[ADR 0001](0001-clean-architecture-with-dependency-inversion.md)**.
ADR 0001 establishes the layered structure and the "interfaces consumer-side"
principle in one line; this ADR documents how the principle applies to
adapters and cross-layer contracts so the codebase is internally consistent.
