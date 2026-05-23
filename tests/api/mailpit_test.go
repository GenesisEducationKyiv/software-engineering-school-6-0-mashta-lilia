package api_test

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

// mailpitContainer wraps an axllent/mailpit container exposing the SMTP
// port (the app sends mail here) and the HTTP API port (tests query it
// to assert on captured messages).
type mailpitContainer struct {
	container testcontainers.Container
	host      string
	smtpPort  int
	httpURL   string
}

func startMailpit(ctx context.Context) (*mailpitContainer, func(), error) {
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

	mp := &mailpitContainer{
		container: c,
		host:      host,
		smtpPort:  smtpPort.Int(),
		httpURL:   fmt.Sprintf("http://%s:%d", host, httpPort.Int()),
	}
	return mp, terminate, nil
}

// mailpitMessage is a thin subset of Mailpit's REST API message envelope —
// just the fields tests assert on.
type mailpitMessage struct {
	ID      string        `json:"ID"`
	From    mailpitAddr   `json:"From"`
	To      []mailpitAddr `json:"To"`
	Subject string        `json:"Subject"`
}

type mailpitAddr struct {
	Address string `json:"Address"`
}

type mailpitMessagesResponse struct {
	Total    int              `json:"total"`
	Messages []mailpitMessage `json:"messages"`
}

// listMessages returns all captured emails currently in Mailpit. Order is
// newest first per Mailpit's API contract.
func (mp *mailpitContainer) listMessages(ctx context.Context) ([]mailpitMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		mp.httpURL+"/api/v1/messages?limit=200", http.NoBody)
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
	var out mailpitMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Messages, nil
}

// messageBody fetches the plaintext body of a captured message by ID.
func (mp *mailpitContainer) messageBody(ctx context.Context, id string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		mp.httpURL+"/api/v1/message/"+id, http.NoBody)
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

// reset deletes all stored messages so the next test starts clean.
func (mp *mailpitContainer) reset(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		mp.httpURL+"/api/v1/messages", http.NoBody)
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

// waitForMessage polls Mailpit for up to timeout, returning the first
// captured message that arrives. Used after Subscribe to assert that the
// confirmation email actually went out. The poll loop honors ctx so a
// parent test that cancels (e.g. via t.Context()) returns immediately
// instead of sleeping out the remaining budget.
func (mp *mailpitContainer) waitForMessage(
	ctx context.Context, timeout time.Duration,
) (mailpitMessage, error) {
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		msgs, err := mp.listMessages(pollCtx)
		if err == nil && len(msgs) > 0 {
			return msgs[0], nil
		}
		select {
		case <-pollCtx.Done():
			return mailpitMessage{}, fmt.Errorf("no message received within %s: %w", timeout, pollCtx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}
