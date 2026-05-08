# ADR 0000: Record Architecture Decisions

Date: 2026-05-08
Status: Accepted
Deciders: Project Author

## Context and Problem Statement

As `github-release-notifier` evolves across homework iterations, the rationale
behind technical choices (polling vs webhooks, SMTP vs SaaS, sync vs async
fan-out) is being lost in PR descriptions and git blame. New contributors —
including future-self — repeatedly ask "why was it done this way?" and the
answer is buried in chat history or no longer exists.

We need a lightweight, version-controlled mechanism to capture **why** a
decision was made at a specific point in time, with the constraints and
alternatives that existed then.

## Decision Drivers

* Decisions must travel with the code (in the repo, not Confluence/Notion).
* The format must be plain text and diffable in Pull Requests.
* The barrier to writing one must be low — otherwise nobody writes them.
* It must be possible to mark a decision as superseded without deleting history.

## Considered Options

* Option 1: **Architectural Decision Records (Nygard / MADR style)** in `docs/adr/`.
* Option 2: Wiki pages (Notion / Confluence).
* Option 3: A long-running `ARCHITECTURE.md` in the repo root.
* Option 4: Do nothing — rely on PR descriptions and git history.

## Decision Outcome

Chosen option: **Option 1 — ADRs as numbered Markdown files in `docs/adr/`**,
because it satisfies all decision drivers with the lowest tooling overhead.

We use the [MADR 3.0](https://adr.github.io/madr/) template (Status / Context /
Decision Drivers / Considered Options / Outcome / Consequences). Files are named
`NNNN-kebab-case-title.md` starting from `0001`. ADR `0000` is reserved for this
meta-decision.

### Consequences

* Good, because every architectural decision is reviewable in a PR with the
  same workflow as code changes.
* Good, because superseded decisions remain visible in git history — readers
  can understand both the past constraint and the reason it changed.
* Good, because the format is tool-agnostic (rendered by any Markdown viewer,
  including GitHub).
* Bad, because ADRs require discipline to keep up to date. Without enforcement,
  they will drift from reality.
* Bad, because there is no automatic linkage between an ADR and the code that
  implements it — refactors can leave ADRs orphaned.

## Pros and Cons of the Options

### ADRs in `docs/adr/`

* + Lives in the repo, versioned with the code.
* + Reviewable in PRs.
* + Plain Markdown — no vendor lock-in.
* − Requires discipline; nothing automatically detects a stale ADR.

### Wiki (Notion / Confluence)

* + Rich formatting, easy embedding of diagrams and comments.
* − Lives outside the repo — drifts from code immediately.
* − Permission management is a separate concern.
* − No diff history that ties to a specific commit.

### Single `ARCHITECTURE.md`

* + One file — easy to find.
* − Conflates many independent decisions; merge conflicts on every change.
* − No way to mark a single decision as superseded.

### Do nothing

* + Zero overhead.
* − The cost compounds: every six months we forget why something was decided.

## Process

1. Open a PR introducing a new ADR file under `docs/adr/`.
2. Status starts as `Proposed`.
3. After review and merge, the author updates the status to `Accepted` in a
   follow-up commit (or in the same PR if uncontested).
4. To revisit a decision: write a **new** ADR with a higher number, set its
   status to `Accepted`, and update the old ADR's status to
   `Superseded by ADR-XXXX`. Never edit the rationale of the old ADR.
