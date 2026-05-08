# ADR 0005: Send Email via Direct SMTP, Not a Transactional Email API

Date: 2026-05-08
Status: Accepted
Deciders: Project Author

## Context and Problem Statement

The service sends two kinds of email: subscription confirmations (one per
subscribe) and release notifications (fan-out to N subscribers per release).
We must choose between calling SMTP servers directly via `net/smtp` or
integrating a transactional email API such as SendGrid, Mailgun, AWS SES, or
Postmark.

The choice affects deliverability, deployment dependencies, cost, dev-loop
ergonomics, and the surface area of email-specific bugs (header injection,
encoding, bounce handling).

## Decision Drivers

* The project is an academic / pet-project deployment; cost must be zero or
  near-zero at low volume.
* Local development and CI must not require signing up for a SaaS account
  or storing API keys for a third-party provider.
* The volume is low — tens of emails per day in normal operation.
* Reliable deliverability to common providers (Gmail, Outlook) is desirable
  but not the primary driver at this scope.

## Considered Options

* Option 1: **Direct SMTP via `net/smtp`**, configured per environment
  (e.g., MailHog locally, Gmail/Mailtrap in CI, real SMTP in prod).
* Option 2: SendGrid / Mailgun / Postmark / AWS SES API client.
* Option 3: Hybrid — abstract through the `Mailer` interface and provide both
  implementations, switchable by config.

## Decision Outcome

Chosen option: **Option 1 — direct SMTP**, because the volume does not
justify a third-party dependency, and the abstraction (the `Mailer`
interface) already isolates the implementation detail.

Implementation (`internal/client/mailer/`):
* `SMTPMailer` struct holds host, port, username, password, "from" address.
* Uses `smtp.PlainAuth` and `smtp.SendMail`.
* Strips `\r` and `\n` from all header values to prevent **header
  injection** — without this, a crafted email with `\nBcc: ...` could
  silently CC an attacker on every notification.
* MIME Q-encodes the Subject (`=?UTF-8?Q?...?=`) so non-ASCII characters
  in repo names render correctly.

Local development uses MailHog (in `docker-compose.yml`). It exposes a web
UI at `:8025` that captures all outgoing mail without forwarding,
eliminating the risk of accidentally emailing real users from a development
environment.

### Consequences

* Good, because zero third-party signup, zero API keys to manage in CI,
  zero monthly cost.
* Good, because the `Mailer` interface remains the seam — switching to
  SendGrid later is a single new struct that implements the interface
  (Option 3 always remains available).
* Good, because the security mitigation (header injection scrubbing,
  Q-encoding) lives in one place; a SaaS API client could hide such bugs
  behind an opaque library.
* Bad, because deliverability to Gmail/Outlook is provider-dependent and
  may require SPF / DKIM / DMARC configuration on the sending domain.
  At low volume from a non-reputation-stable IP, mail can land in spam.
* Bad, because SMTP failures are synchronous — a slow SMTP server slows
  down the scanner's notification fan-out (see
  [ADR 0006](0006-synchronous-email-fan-out.md)).
* Bad, because there is no built-in bounce or complaint feedback loop.
  A hard-bounced email keeps receiving notifications until the user
  unsubscribes.

## Pros and Cons of the Options

### Direct SMTP

* + Free at any volume.
* + No third-party dependency to onboard or replace.
* + Local dev is trivial (MailHog).
* − Deliverability tuning is on us.
* − No bounce / complaint pipeline.

### Transactional API (SendGrid / Mailgun / SES)

* + Excellent deliverability, reputation management.
* + Bounce / complaint webhooks.
* + Templating, tracking, analytics.
* − Cost — free tiers exist but require account signup, billing setup.
* − Vendor lock-in unless the `Mailer` interface is preserved.
* − API keys in CI / production secrets management.

### Hybrid (both implementations behind `Mailer`)

* + Maximum flexibility.
* − Twice the code surface to test.
* − No payoff today; YAGNI.

## Future Direction

When this service is run in a production setting where deliverability matters
(non-academic), revisit this ADR and consider Option 2. The migration cost is
exactly one new file implementing `Mailer` plus a config switch.
