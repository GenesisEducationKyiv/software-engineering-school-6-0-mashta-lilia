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

type SMTPMailer struct {
	host     string
	port     int
	user     string
	password string
	from     string
	baseURL  string
}

func NewSMTPMailer(host string, port int, user, password, from, baseURL string) *SMTPMailer {
	return &SMTPMailer{
		host:     host,
		port:     port,
		user:     user,
		password: password,
		from:     from,
		baseURL:  baseURL,
	}
}

func (m *SMTPMailer) SendConfirmation(ctx context.Context, email, token, repo string) error {
	subject := fmt.Sprintf("Confirm your subscription to %s releases", repo)
	confirmURL := fmt.Sprintf("%s/api/confirm/%s", m.baseURL, token)
	body := fmt.Sprintf(
		"You have subscribed to release notifications for %s.\n\n"+
			"Please confirm your subscription by clicking the link below:\n%s\n\n"+
			"If you did not request this, you can ignore this email.",
		repo, confirmURL,
	)

	return m.sendWithContext(ctx, email, subject, body)
}

func (m *SMTPMailer) SendReleaseNotification(
	ctx context.Context, email, repo string, release *model.Release,
) error {
	subject := fmt.Sprintf("New release for %s: %s", repo, release.TagName)
	body := fmt.Sprintf(
		"A new release has been published for %s!\n\n"+
			"Version: %s\n"+
			"Name: %s\n"+
			"URL: %s\n",
		repo, release.TagName, release.Name, release.HTMLURL,
	)

	return m.sendWithContext(ctx, email, subject, body)
}

func sanitizeHeader(value string) string {
	r := strings.NewReplacer("\r", "", "\n", "")
	return r.Replace(value)
}

// sendWithContext sends an email using a context-aware TCP connection.
// Unlike smtp.SendMail, this implementation respects context cancellation at
// the dial phase (via net.DialContext) and at the send phase (via connection
// deadline), preventing goroutine leaks when the caller's context is canceled.
func (m *SMTPMailer) sendWithContext(ctx context.Context, to, subject, body string) error {
	sanitizedTo := sanitizeHeader(to)
	sanitizedFrom := sanitizeHeader(m.from)
	encodedSubject := mime.QEncoding.Encode("utf-8", subject)

	const msgTemplate = "From: %s\r\nTo: %s\r\nSubject: %s\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=\"utf-8\"\r\n\r\n%s"
	msg := fmt.Sprintf(msgTemplate, sanitizedFrom, sanitizedTo, encodedSubject, body)

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
	if _, err := w.Write([]byte(msg)); err != nil {
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
