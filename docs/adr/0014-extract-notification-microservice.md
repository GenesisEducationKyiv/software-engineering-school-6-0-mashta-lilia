# ADR 0014: Extract Notification Microservice

Date: 2026-06-10
Status: Accepted
Deciders: Project Author

## Context and Problem Statement

HW7 requires sharpening modular boundaries and moving one domain into a
separate microservice over gRPC with its own database. The current monolith
already has clean consumer-side interfaces for sending confirmation and release
emails: `internal/subscription` consumes `confirmationSender`, and
`internal/release` consumes `releaseNotifier`.

The candidate domains are subscription, release polling, repository tracking,
GitHub integration, and notification. Subscription and release own core
business workflows and share the monolith's primary data model. Notification is
a leaf concern: it composes and sends email, and both callers already depend on
it through interfaces.

## Considered Options

* Extract notification into `services/notification`.
* Extract release polling.
* Extract subscription management.
* Keep a monolith and only re-folder packages.

## Decision Outcome

Extract **notification** as a separate deployable in the same Go module. The
monolith keeps the API, subscription lifecycle, repository store, and release
poller. The new service owns email composition, SMTP delivery, and a
`sent_notifications` ledger in its own Postgres database.

The notifier is structured as a DDD module:

* `model`: pure notification value types.
* `app`: orchestration and dedup policy.
* `inbound/grpcserver`: gRPC transport mapping.
* `outbound/smtp` and `outbound/store`: external adapters.

The monolith now calls `internal/outbound/notification`, a gRPC adapter that
satisfies the existing `subscription` and `release` interfaces. Those domain
packages remain unchanged, which is the boundary proof we wanted.

### Consequences

* Good, because notification is independently deployable and owns its own data.
* Good, because no broad re-foldering of subscription, release, or repository
  code is required.
* Good, because the assignment's DDD layering rule is demonstrated end to end
  in one module.
* Bad, because subscribe and poller fan-out now depend on a synchronous network
  hop.
* Bad, because local development needs an extra service and database.
