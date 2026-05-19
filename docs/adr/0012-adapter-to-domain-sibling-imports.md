# ADR 0012: Adapter→domain sibling imports permitted (carve-out from rulebook §5)

Date: 2026-05-17
Status: Superseded by vertical slicing (2026-05-19)
Deciders: Project Author

> **2026-05-19 update.** PR #5 review pushed back on the mechanism-grouped
> `internal/storage/` package: the reviewer asked that repos live inside
> their domain packages instead. We adopted that:
> `internal/storage/subscription.go` became `internal/subscription/repo.go`;
> `internal/storage/tracked_repo.go` became `internal/repository/store.go`
> alongside `repository.Repository` (was `release.TrackedRepository`) and
> `repository.Ref` (was `repo.Ref`). The clients still import release for
> `release.Release` — that one cross-sibling type signature import is
> the only remnant of the carve-out below.

## Context

Rulebook §5: *"Packages at the same directory level must not import each other."*
A strict reading forbids these adapter→domain imports, each of which exists
solely to name a type for DB-row / JSON marshaling:

- `internal/storage/subscription.go` → `internal/subscription` (`*subscription.Subscription`)
- `internal/storage/tracked_repo.go` → `internal/release` (`*release.TrackedRepository`)
- `internal/client/github/*.go` → `internal/release` (`release.Release`)
- `internal/client/mailer/*.go` → `internal/release` (`release.Release`)

The only alternative that fully satisfies §5 is vertical (DDD-style) slicing:
dissolve `internal/storage/` into `internal/subscription/storage.go` and
`internal/release/storage.go`; same for the clients. This restructure was
explicitly rejected — the project keeps adapters grouped by mechanism
(`storage/`, `client/github/`, `client/mailer/`), not by domain.

## Decision

Adapter packages may import a sibling domain package **for type signatures
and row/JSON marshaling only**. Domain packages must **never** import their
adapters — the dependency arrow stays one-way and no cycle is possible.

The one sibling import that did *not* survive is `internal/subscription` →
`internal/release`: it existed for a return type that the caller discarded with
`_, err := ...`. That value is now `error`, and the import is gone.

## Consequences

- `internal/storage/` and `internal/client/{github,mailer}/` keep their flat,
  mechanism-grouped layout. New adapters land next to existing adapters.
- Marshaling code reads naturally — fields are named, typed values, not
  `map[string]any` blobs at the storage boundary.
- Rulebook §5 has a one-line exception. A future contributor must read this
  ADR to know it. The check is "is the importer an adapter, is the import
  used only for type signatures?" — by review, not by lint.
- If the project ever splits along bounded contexts (subscriptions service vs
  release-poller service), vertical slicing becomes the natural restructure
  and this carve-out goes away.

## Relation to other ADRs

Refines [ADR-0001](0001-clean-architecture-with-dependency-inversion.md)
(layered + adapter pattern) and [ADR-0009](0009-consumer-side-interface-placement.md)
(consumer-side interfaces) — neither said anything about sibling type imports.
