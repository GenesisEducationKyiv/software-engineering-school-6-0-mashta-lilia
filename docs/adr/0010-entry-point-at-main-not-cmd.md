# ADR 0010: Entry point at `main/main.go`, not `cmd/<name>/`

Date: 2026-05-17
Status: Accepted
Deciders: Project Author

## Context

The "Go Package-Oriented Design" rulebook this project otherwise follows mandates
`cmd/<name>/main.go` (one folder per binary, daemon suffix `d`). PR-5 reviewer
@vladyslavpavlenko explicitly rejected that convention:

> "there's no need for `cmd/server`, or `cmd/app`, or anything similar. It was
> once a standard for Heroku, for example, but not anymore. Stick to
> `main/main.go`."
> — [PR-5 comment](https://github.com/GenesisEducationKyiv/software-engineering-school-6-0-mashta-lilia/pull/5#discussion_r3250570986)

Where rulebook and reviewer disagree, the reviewer wins (project lead's call).

## Decision

Entry point lives at `main/main.go`. The body does only:

```go
cfg, err := config.NewFromEnv()
if err != nil { panic(fmt.Errorf("config: %w", err)) }
l := logger.New(cfg.LogLevel)
app.New(cfg, l).Run(ctx)
```

No business logic. The "starve main" principle from the rulebook is honored via
layout, not via the rulebook's chosen folder name. The former `config.Must`
helper was inlined here so the panic visibly originates at the app entry rather
than inside a foundational package (see [ADR-0011](0011-platform-returns-root-cause-errors.md)).

## Consequences

- Single-binary projects stay flat: `go run ./main`, `go build ./main`.
- If a second binary ever lands (CLI tool, migrator, one-off job), Go's
  idiomatic `cmd/server/`, `cmd/migrate/` layout is closed off without
  reopening this ADR. Today there is only one binary; revisit then.
- Tooling that assumes `cmd/<name>/` (some scaffolders, some lint configs)
  needs manual paths.
