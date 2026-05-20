# Architecture Decision Records

This directory holds Architecture Decision Records (ADRs) for the small set of
**non-obvious structural choices** in `github-release-notifier`. We deliberately
do **not** record an ADR for every library or technology pick — those belong in
the [System Design](../system-design.md) document or in the code itself.

The format follows [MADR 3.0](https://adr.github.io/madr/).

## Index

| #    | Status   | Title                                                                                  | Why an ADR                                       |
|------|----------|----------------------------------------------------------------------------------------|--------------------------------------------------|
| 0001 | Accepted | [Layered Architecture with Dependency Inversion](0001-clean-architecture-with-dependency-inversion.md) | Sets the dependency rule for the whole codebase. |
| 0007 | Accepted | [Persist `last_seen_tag` Before Sending Notifications](0007-persist-before-notify-for-at-most-once.md) | Non-obvious correctness trade-off (at-most-once vs at-least-once). |
| 0008 | Accepted | [Partial Unique Index to Allow Re-subscription](0008-partial-unique-index-for-resubscription.md) | Non-obvious data-model encoding of a state-dependent rule. |
| 0009 | Accepted | [Consumer-Side Interface Placement](0009-consumer-side-interface-placement.md) | Refines ADR-0001 with a precise rule for adapters and cross-layer contracts. |
| 0010 | Accepted | [Entry point at `main/main.go`, not `cmd/<name>/`](0010-entry-point-at-main-not-cmd.md) | Overrides the rulebook's `cmd/` mandate per PR-5 reviewer; closes off the idiomatic multi-binary layout. |
| 0011 | Accepted | [Foundational Packages Return Root-Cause Errors](0011-platform-returns-root-cause-errors.md) | Locks the `internal/platform/` no-`%w`, no-`slog`, no-`panic` contract and the `errors.Join` close-failure pattern. |
| 0012 | Superseded | [Adapter→Domain Sibling Imports Permitted](0012-adapter-to-domain-sibling-imports.md) | Historical carve-out from rulebook §5; superseded by vertical slicing (2026-05-19). |
| 0013 | Accepted | [Do not commit `vendor/`](0013-vendor-third-party-dependencies.md) | Deviates from rulebook §2; `go.sum` provides the same supply-chain integrity without the diff cost. |

## When to Add a New ADR

Reserve ADRs for decisions that future readers would otherwise be unable to
explain by reading the code, in particular:

- A genuinely **structural** change (e.g., introducing a strangler in front of
  an old service, splitting a service in two, switching the persistence shape).
- A non-obvious **correctness** trade-off where the alternative is defensible
  and a reader would reasonably ask "why did they pick this side?".
- A **data-model** decision that encodes business rules in schema where the
  intent is not visible from the schema alone.

Do **not** write an ADR for:

- Library / framework picks (Chi vs Echo, `lib/pq` vs `pgx`, MailHog vs Mailtrap).
- Adding a cache, a metric, or a retry — these belong in the System Design.
- Tactical tuning (timeouts, pool sizes, batch sizes).

## Workflow

1. Open a PR introducing a new ADR file under `docs/adr/`.
2. Status starts as `Proposed`.
3. After review and merge, set status to `Accepted` and update the index above.
4. To revisit a decision: write a **new** ADR with a higher number, set its
   status to `Accepted`, and mark the old ADR's status `Superseded by ADR-NNNN`.
   Never edit the rationale of the old ADR.
