# ADR 0009: Consumer-Side Interface Placement

Date: 2026-05-12
Status: Accepted
Deciders: Project Author

## Context

[ADR-0001](0001-clean-architecture-with-dependency-inversion.md) commits to
"interfaces declared by consumers, not producers" in one line. The first hw4
pass violated this in three places (a `service.HealthChecker` that `rest/`
imported, a single `service/interfaces.go` stacking every contract, an
exported `SubscriptionUseCase` only `rest/` consumed). Coach review pointed at
[Go Code Review Comments §Interfaces](https://go.dev/wiki/CodeReviewComments#interfaces)
and asked for strict application of the rule.

Without a single rule, two patterns lived in the same codebase and future
contributors had no way to predict where a *new* interface should go.

## Decision

> An interface belongs in the package that **calls** its methods.
> When the consumer is a package containing several types that share the same
> dependency (e.g., multiple types in `release/` use the same
> GitHub client), the package as a whole is the consumer and the interface
> lives at package scope. When the consumer is a single
> wrapper/adapter (e.g., `CachedClient` decorating a GitHub client), the
> adapter declares its own local interface and does not import the upstream
> package.

Every consumer-side interface is **unexported**. Go's structural typing lets
the composition root pass a concrete value across package boundaries without
exporting the interface that consumes it.

Current placement after the package split:

| Interface | Lives in | Consumer |
|---|---|---|
| `subscriptionStore`, `repoUpserter`, `githubChecker`, `confirmationSender`, `tokenGen` | `internal/subscription/interfaces.go` | `subscription.Service` |
| `repoScanReader`, `subscriberLister`, `githubReleaseClient`, `releaseNotifier` | `internal/release/interfaces.go` | `release.Poller` |
| `subscriptionService` | `internal/api/rest/subscription/handler.go` | `subscription.Handler` |
| `checker` | `internal/api/rest/health/handler.go` | `health.Handler` |
| `healthChecker` | `internal/api/rest/router.go` | `rest.NewRouter` |
| `baseClient` | `internal/client/github/cached.go` | `github.CachedClient` |
| `githubClient` | `internal/app/deps.go` | composition root |

The drift risk for adapter-local interfaces (e.g., `baseClient` vs the
composition root's `githubClient`) is bounded by `internal/app/deps.go`: both
`*github.Client` and `*github.CachedClient` flow through the same wiring, so
any signature divergence becomes a compile error at boot.

## Consequences

- One rule answers "where does my new interface go?" without re-reading the
  codebase or polling the author.
- Adapter packages (`client/github`, `client/mailer`, `storage`) stay leaves
  in the import graph — their only upstream imports are domain types
  (see [ADR-0012](0012-adapter-to-domain-sibling-imports.md)).
- Tests mock only what their target actually calls — ISP applies naturally.
- Cost: `baseClient` and `githubClient` are effectively duplicate two-method
  contracts. No static check keeps them in sync; the composition root does.
- Cost: the rule has a narrowing ("package as a whole vs single adapter")
  that requires reading this ADR to apply to edge cases.

## Relation to other ADRs

Refines, does not supersede, [ADR-0001](0001-clean-architecture-with-dependency-inversion.md).
