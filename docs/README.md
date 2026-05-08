# Docs

This directory holds the architecture documentation for `github-release-notifier`.

| Document                                  | Purpose                                                              |
|-------------------------------------------|----------------------------------------------------------------------|
| [system-design.md](system-design.md)      | Single-page system design — overview, NFRs, C4 diagrams, data model, failure modes, capacity. |
| [adr/](adr/)                              | Architecture Decision Records — one file per significant decision.    |
| [adr/README.md](adr/README.md)            | Index of ADRs and the workflow for proposing new ones.                |

## Reading Order

1. Start with **[system-design.md](system-design.md)** — it cross-references
   the ADRs at every decision point.
2. Drill into individual ADRs from there. The index at
   [adr/README.md](adr/README.md) is the launchpad.

## Conventions

- Diagrams use **Mermaid** so they render natively in GitHub and stay
  diff-able alongside the prose.
- ADRs follow [MADR 3.0](https://adr.github.io/madr/).
- Each ADR includes a "Status:" field after the title and date block,
  showing one of `Proposed`, `Accepted`, `Deprecated`, or `Superseded`.
  Superseded ADRs are kept in place — the history matters.
