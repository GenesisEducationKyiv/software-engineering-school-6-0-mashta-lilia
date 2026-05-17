# ADR 0011: Foundational packages return root-cause errors; wrapping happens one layer up

Date: 2026-05-17
Status: Accepted
Deciders: Project Author

## Context

`internal/platform/` packages (`logger`, `token`, `postgres`, `health`) are
foundational: they wrap stdlib / driver mechanics and have no business context
to add. Per the rulebook, they must not call `slog.*`, must not `panic`, and
must not wrap with `fmt.Errorf("...: %w", err)` — they have nothing to say that
the original error doesn't already say.

The caller, which knows *why* it was opening a database or generating a token,
is the right place to add context.

## Decision

`internal/platform/*` returns root-cause errors only. Application packages
(`internal/app/`, `internal/subscription/`, etc.) wrap when they propagate.

Concrete pattern in `internal/platform/postgres/driver.go::New` — if `PingContext`
fails after `sql.Open` succeeded, the close error must not be swallowed by a
platform-side `slog.Error`:

```go
if err := db.PingContext(ctx); err != nil {
    return nil, errors.Join(err, db.Close())
}
```

Wrap context (`"opening database: %w"`, `"running migrations: %w"`,
`"generating token: %w"`) lives in `internal/app/db.go` and
`internal/subscription/service.go`.

## Consequences

- Platform stays as a leaf in the import graph and as a "no surprises" layer:
  readers know it logs nothing and panics nowhere.
- Stack traces / error chains stay clean — no double-wrapping like
  `opening database: connecting: dial tcp: ...`.
- A future contributor seeing `errors.Join(err, db.Close())` will reasonably
  ask *"why not `defer db.Close()` and log?"*. The answer is here: platform
  may not log; the close failure has to travel back as an error so the
  composition root can decide what to do with it.
- The rule has to be enforced by review — there is no linter for "no `%w` in
  this subtree."
