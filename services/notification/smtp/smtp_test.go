package smtp_test

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/services/notification"
	"github-release-notifier/services/notification/smtp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A server that accepts the connection but never speaks SMTP must not hang the
// sender past its timeout — the consumer path's context carries no deadline.
func TestSMTPMailer_DeliverHonorsTimeout(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close() //nolint:errcheck // test listener

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close() //nolint:errcheck // held open to simulate a stalled server
		time.Sleep(2 * time.Second)
	}()

	host, portStr, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	const sendTimeout = 100 * time.Millisecond
	mailer, err := smtp.NewSMTPMailer(
		host, port, "", "", "noreply@test.local", sendTimeout, smtp.NewTemplateBuilder(), logger.Nop(),
	)
	require.NoError(t, err)

	start := time.Now()
	// context.Background() has no deadline, mirroring the broker consumer path.
	err = mailer.SendConfirmation(context.Background(), notification.Confirmation{
		Email:      "alice@example.com",
		ConfirmURL: "https://app.example/api/confirm/tok-123",
		Repo:       "golang/go",
	})
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, time.Second, "delivery must abort near the timeout, not hang on the stalled socket")
}
