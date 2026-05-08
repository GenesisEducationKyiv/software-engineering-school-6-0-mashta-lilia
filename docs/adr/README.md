# Architecture Decision Records

This directory holds the Architecture Decision Records (ADRs) for
`github-release-notifier`. Each ADR captures a single significant technical
decision with its context, alternatives, and consequences as they were
understood at the time of writing.

The format follows [MADR 3.0](https://adr.github.io/madr/). The process for
proposing, accepting, and superseding ADRs is itself documented in
[ADR 0000](0000-record-architecture-decisions.md).

## Index

| #    | Status   | Title                                                                                  |
|------|----------|----------------------------------------------------------------------------------------|
| 0000 | Accepted | [Record Architecture Decisions](0000-record-architecture-decisions.md)                 |
| 0001 | Accepted | [Layered Architecture with Dependency Inversion](0001-clean-architecture-with-dependency-inversion.md) |
| 0002 | Accepted | [Poll GitHub for Releases Instead of Using Webhooks](0002-polling-over-webhooks-for-github.md) |
| 0003 | Accepted | [PostgreSQL as the Primary Datastore](0003-postgresql-as-primary-datastore.md)         |
| 0004 | Accepted | [Cache GitHub Responses with Redis Using a Decorator](0004-redis-cache-aside-as-decorator.md) |
| 0005 | Accepted | [Send Email via Direct SMTP, Not a Transactional API](0005-direct-smtp-over-transactional-api.md) |
| 0006 | Accepted | [Synchronous Email Fan-out from the Scanner Loop](0006-synchronous-email-fan-out.md)   |
| 0007 | Accepted | [Persist `last_seen_tag` Before Sending Notifications](0007-persist-before-notify-for-at-most-once.md) |
| 0008 | Accepted | [Partial Unique Index to Allow Re-subscription](0008-partial-unique-index-for-resubscription.md) |

## How to Add a New ADR

1. Copy the most recent ADR file as a template.
2. Number it `NNNN` (next sequential, zero-padded to 4 digits).
3. Set `Status: Proposed`, fill in Context / Drivers / Options / Outcome.
4. Open a PR. After review, change `Status` to `Accepted` and update this
   index.
5. To revisit a decision: write a **new** ADR, mark the old one as
   `Superseded by ADR-NNNN`, and update both files in the same PR.
