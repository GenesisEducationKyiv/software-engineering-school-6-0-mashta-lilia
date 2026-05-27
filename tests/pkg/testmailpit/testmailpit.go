// Package testmailpit boots an axllent/mailpit container for integration
// tests and exposes the subset of Mailpit's REST API the suites actually
// assert on (list, fetch body, reset, wait-for-message).
package testmailpit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Container wraps a running Mailpit, exposing the SMTP port (where the app
// sends mail) and the HTTP API URL (where tests query captured messages).
type Container struct {
	c        testcontainers.Container
	Host     string
	SMTPPort int
	HTTPURL  string
}

// New starts a Mailpit container and returns it together with a cleanup
// func that terminates the container.
func New(ctx context.Context) (*Container, func(), error) {
	req := testcontainers.ContainerRequest{
		Image:        "axllent/mailpit:v1.18",
		ExposedPorts: []string{"1025/tcp", "8025/tcp"},
		WaitingFor: wait.ForHTTP("/api/v1/info").
			WithPort("8025/tcp").
			WithStartupTimeout(60 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, func() {}, err
	}
	terminate := func() {
		if err := c.Terminate(context.Background()); err != nil {
			slog.Warn("terminate mailpit", "err", err)
		}
	}

	host, err := c.Host(ctx)
	if err != nil {
		return nil, terminate, err
	}
	smtpPort, err := c.MappedPort(ctx, "1025")
	if err != nil {
		return nil, terminate, err
	}
	httpPort, err := c.MappedPort(ctx, "8025")
	if err != nil {
		return nil, terminate, err
	}

	return &Container{
		c:        c,
		Host:     host,
		SMTPPort: smtpPort.Int(),
		HTTPURL:  fmt.Sprintf("http://%s:%d", host, httpPort.Int()),
	}, terminate, nil
}

// Message is a thin subset of Mailpit's REST API envelope — only the fields
// tests assert on.
type Message struct {
	ID      string `json:"ID"`
	From    Addr   `json:"From"`
	To      []Addr `json:"To"`
	Subject string `json:"Subject"`
}

type Addr struct {
	Address string `json:"Address"`
}

type messagesResponse struct {
	Total    int       `json:"total"`
	Messages []Message `json:"messages"`
}

// ListMessages returns all captured emails currently in Mailpit. Order is
// newest first per Mailpit's API contract.
func (mp *Container) ListMessages(ctx context.Context) ([]Message, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		mp.HTTPURL+"/api/v1/messages?limit=200", http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("mailpit list: %d %s", resp.StatusCode, string(body))
	}
	var out messagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Messages, nil
}

// MessageBody fetches the plaintext body of a captured message by ID.
func (mp *Container) MessageBody(ctx context.Context, id string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		mp.HTTPURL+"/api/v1/message/"+id, http.NoBody)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mailpit get message: %d", resp.StatusCode)
	}
	var msg struct {
		Text string `json:"Text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return "", err
	}
	return msg.Text, nil
}

// Reset deletes all stored messages so the next test starts clean.
func (mp *Container) Reset(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		mp.HTTPURL+"/api/v1/messages", http.NoBody)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("mailpit reset: %d", resp.StatusCode)
	}
	return nil
}

// WaitForMessage polls Mailpit for up to timeout, returning the first
// captured message that arrives. The poll loop honors ctx so a parent that
// cancels (e.g. via t.Context()) returns immediately instead of sleeping
// out the remaining budget.
func (mp *Container) WaitForMessage(ctx context.Context, timeout time.Duration) (Message, error) {
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		msgs, err := mp.ListMessages(pollCtx)
		if err == nil && len(msgs) > 0 {
			return msgs[0], nil
		}
		select {
		case <-pollCtx.Done():
			return Message{}, fmt.Errorf("no message received within %s: %w", timeout, pollCtx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}
