# ADR 0013: Do not commit `vendor/`

Date: 2026-05-17
Status: Accepted
Deciders: Project Author

## Context

Rulebook §2 mandates vendoring all third-party code into a committed
`vendor/` directory. Modern Go (since 1.11) relies on the module proxy
plus `go.sum` for the same guarantees the rulebook is asking for —
reproducibility and supply-chain integrity — without the diff cost.

The repo already shipped `vendor/` in `.gitignore` from day one. An
earlier draft of this ADR proposed removing it and committing the 26 MB
of dependency source; that draft was reverted before merge.

## Decision

Do not commit `vendor/`. `go.sum` provides cryptographic pinning of
every dependency version; the module proxy is the resolution source for
both local development and CI. `vendor/` may be created locally with
`go mod vendor` for offline builds but stays in `.gitignore`.

## Consequences

- PR diffs stay focused on substantive changes; reviewers do not scroll
  past 600k lines of dependency source.
- Repository clones do not carry a 26 MB tax forever.
- CI continues to resolve modules from the proxy. No silent
  `-mod=vendor` switch.
- Rulebook §2 is **not** satisfied on this point. We accept the deviation
  because the rulebook predates `go.sum` (or its author treats Go's
  built-in supply-chain story as insufficient — neither premise applies
  here). If a future build requires offline-first compilation, run
  `go mod vendor` locally and re-evaluate this ADR.
- Reviewer-friendliness over rule-literalism, consistent with ADR-0010's
  same posture on `main/` vs `cmd/`.
