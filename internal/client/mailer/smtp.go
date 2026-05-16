package mailer

import (
	"context"
	"fmt"
	"github-release-notifier/internal/model"
	"mime"
	"net"
	"net/smtp"
	"strings"
)

// SMTPMailer is a thin transport over net/smtp. It does NOT know how to
// build email content — that responsibility belongs to TemplateBuilder.
// SMTPMailer composes a builder and only adds delivery semantics: dialing,
// auth, MIME envelope, header sanitization.
type SMTPMailer struct {
	host      string
	port      int
	user      string
	password  string
	from      string
	templates *TemplateBuilder
}

// NewSMTPMailer wires a transport with the templates it should render.
// baseURL no longer leaks into the transport — it lives where it semantically
// belongs (the template builder, since URLs are content, not transport).
func NewSMTPMailer(
	host string, port int, user, password, from string, templates *TemplateBuilder,
) *SMTPMailer {
	return &SMTPMailer{
		host:      host,
		port:      port,
		user:      user,
		password:  password,
		from:      from,
		templates: templates,
	}
}

func (m *SMTPMailer) SendConfirmation(ctx context.Context, email, token, repo string) error {
	return m.deliver(ctx, m.templates.Confirmation(email, token, repo))
}

func (m *SMTPMailer) SendReleaseNotification(
	ctx context.Context, email, repo string, release *model.Release,
) error {
	return m.deliver(ctx, m.templates.ReleaseNotification(email, repo, release))
}

func sanitizeHeader(value string) string {
	r := strings.NewReplacer("\r", "", "\n", "")
	return r.Replace(value)
}

// deliver sends a pre-built Message via SMTP. It is the only place in the
// package that talks to the wire.
//
// Unlike smtp.SendMail, this implementation respects context cancellation at
// the dial phase (via net.DialContext) and at the send phase (via connection
// deadline), preventing goroutine leaks when the caller's context is canceled.
func (m *SMTPMailer) deliver(ctx context.Context, msg Message) error {
	sanitizedTo := sanitizeHeader(msg.To)
	sanitizedFrom := sanitizeHeader(m.from)
	// Strip CR/LF from Subject before MIME-encoding. mime.QEncoding already
	// escapes raw newlines, but pre-sanitizing keeps every header field
	// flowing through the same defense (consistency with To/From).
	encodedSubject := mime.QEncoding.Encode("utf-8", sanitizeHeader(msg.Subject))

	const envelopeTemplate = "From: %s\r\nTo: %s\r\nSubject: %s\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=\"utf-8\"\r\n\r\n%s"
	envelope := fmt.Sprintf(envelopeTemplate, sanitizedFrom, sanitizedTo, encodedSubject, msg.Body)

	addr := fmt.Sprintf("%s:%d", m.host, m.port)

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connecting to SMTP server: %w", err)
	}
	defer conn.Close() //nolint:errcheck // TCP conn close error is safe to ignore

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline) //nolint:errcheck // deadline is best-effort
	}

	client, err := smtp.NewClient(conn, m.host)
	if err != nil {
		return fmt.Errorf("creating SMTP client: %w", err)
	}
	defer client.Close() //nolint:errcheck // SMTP client close error is safe to ignore

	if m.user != "" {
		if err := client.Auth(smtp.PlainAuth("", m.user, m.password, m.host)); err != nil {
			return fmt.Errorf("SMTP auth: %w", err)
		}
	}

	if err := client.Mail(sanitizedFrom); err != nil {
		return fmt.Errorf("SMTP MAIL FROM: %w", err)
	}
	if err := client.Rcpt(sanitizedTo); err != nil {
		return fmt.Errorf("SMTP RCPT TO: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := w.Write([]byte(envelope)); err != nil {
		return fmt.Errorf("writing email body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("closing email body: %w", err)
	}

	if err := client.Quit(); err != nil {
		return fmt.Errorf("SMTP QUIT: %w", err)
	}
	return nil
}
