package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github-release-notifier/services/notification"
	"mime"
	"net"
	"net/smtp"
	"strings"
)

type SMTPMailer struct {
	host      string
	port      int
	user      string
	password  string
	from      string
	templates *TemplateBuilder
}

func NewSMTPMailer(
	host string, port int, user, password, from string, templates *TemplateBuilder,
) (*SMTPMailer, error) {
	if templates == nil {
		return nil, errors.New("smtp mailer: templates is nil")
	}
	return &SMTPMailer{
		host:      host,
		port:      port,
		user:      user,
		password:  password,
		from:      from,
		templates: templates,
	}, nil
}

func (m *SMTPMailer) SendConfirmation(ctx context.Context, confirmation notification.Confirmation) error {
	templates, err := m.templateBuilder()
	if err != nil {
		return err
	}
	return m.deliver(ctx, templates.Confirmation(
		confirmation.Email, confirmation.Token, confirmation.Repo,
	))
}

func (m *SMTPMailer) SendReleaseNotification(
	ctx context.Context, email, repo string, rel *notification.ReleaseInfo,
) error {
	templates, err := m.templateBuilder()
	if err != nil {
		return err
	}
	return m.deliver(ctx, templates.ReleaseNotification(email, repo, rel))
}

func (m *SMTPMailer) templateBuilder() (*TemplateBuilder, error) {
	if m == nil || m.templates == nil {
		return nil, errors.New("smtp mailer: templates is nil")
	}
	return m.templates, nil
}

func sanitizeHeader(value string) string {
	r := strings.NewReplacer("\r", "", "\n", "")
	return r.Replace(value)
}

func (m *SMTPMailer) deliver(ctx context.Context, msg Message) error {
	sanitizedTo := sanitizeHeader(msg.To)
	sanitizedFrom := sanitizeHeader(m.from)
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

	// Refuse to send PLAIN creds without STARTTLS; otherwise credentials leak on the wire.
	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsCfg := &tls.Config{ServerName: m.host, MinVersion: tls.VersionTLS12}
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("SMTP STARTTLS: %w", err)
		}
	} else if m.user != "" {
		return errors.New(
			"SMTP: server does not support STARTTLS; refusing to send credentials over plaintext")
	}

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
