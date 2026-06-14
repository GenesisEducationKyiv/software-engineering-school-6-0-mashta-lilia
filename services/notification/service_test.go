package notification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github-release-notifier/internal/platform/logger"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSender struct {
	confirmationCalls int
	releaseCalls      int
	err               error
}

func (f *fakeSender) SendConfirmation(_ context.Context, _ Confirmation) error {
	f.confirmationCalls++
	return f.err
}

func (f *fakeSender) SendReleaseNotification(
	_ context.Context, _ string, _ string, _ *ReleaseInfo,
) error {
	f.releaseCalls++
	return f.err
}

type fakeDedupStore struct {
	reserved bool
	err      error
	kind     string
	key      string
}

func (f *fakeDedupStore) Reserve(_ context.Context, kind, dedupKey string) (bool, error) {
	f.kind = kind
	f.key = dedupKey
	return f.reserved, f.err
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestService_SendConfirmation_ReservesThenSends(t *testing.T) {
	t.Parallel()
	sender := &fakeSender{}
	dedup := &fakeDedupStore{reserved: true}
	svc, err := NewService(sender, dedup, logger.Nop())
	require.NoError(t, err)

	delivered, err := svc.SendConfirmation(context.Background(), Confirmation{
		Email: "alice@example.com",
		Token: "tok-123",
		Repo:  "golang/go",
	})

	require.NoError(t, err)
	assert.True(t, delivered)
	assert.Equal(t, 1, sender.confirmationCalls)
	assert.Equal(t, kindConfirmation, dedup.kind)
	assert.Equal(t, sha256Hex("confirm:tok-123"), dedup.key)
}

func TestService_SendConfirmation_DedupConflictSkipsSend(t *testing.T) {
	t.Parallel()
	sender := &fakeSender{}
	dedup := &fakeDedupStore{reserved: false}
	svc, err := NewService(sender, dedup, logger.Nop())
	require.NoError(t, err)

	delivered, err := svc.SendConfirmation(context.Background(), Confirmation{
		Email: "alice@example.com",
		Token: "tok-123",
		Repo:  "golang/go",
	})

	require.NoError(t, err)
	assert.False(t, delivered)
	assert.Equal(t, 0, sender.confirmationCalls)
}

func TestService_SendReleaseNotification_UsesReleaseDedupKey(t *testing.T) {
	t.Parallel()
	sender := &fakeSender{}
	dedup := &fakeDedupStore{reserved: true}
	svc, err := NewService(sender, dedup, logger.Nop())
	require.NoError(t, err)

	delivered, err := svc.SendReleaseNotification(
		context.Background(),
		"alice@example.com",
		"golang/go",
		&ReleaseInfo{TagName: "v1.22.0"},
	)

	require.NoError(t, err)
	assert.True(t, delivered)
	assert.Equal(t, 1, sender.releaseCalls)
	assert.Equal(t, kindRelease, dedup.kind)
	assert.Equal(t, sha256Hex("release:golang/go:v1.22.0:alice@example.com"), dedup.key)
}

func TestService_SendErrorIsReturnedAfterReservation(t *testing.T) {
	t.Parallel()
	sendErr := errors.New("smtp down")
	sender := &fakeSender{err: sendErr}
	dedup := &fakeDedupStore{reserved: true}
	svc, err := NewService(sender, dedup, logger.Nop())
	require.NoError(t, err)

	delivered, err := svc.SendReleaseNotification(
		context.Background(), "alice@example.com", "golang/go", &ReleaseInfo{TagName: "v1.22.0"},
	)

	assert.False(t, delivered)
	require.Error(t, err)
	assert.ErrorIs(t, err, sendErr)
	assert.Equal(t, 1, sender.releaseCalls)
}

func TestService_ReserveErrorSkipsSend(t *testing.T) {
	t.Parallel()
	reserveErr := errors.New("db down")
	sender := &fakeSender{}
	dedup := &fakeDedupStore{err: reserveErr}
	svc, err := NewService(sender, dedup, logger.Nop())
	require.NoError(t, err)

	delivered, err := svc.SendConfirmation(context.Background(), Confirmation{
		Email: "alice@example.com",
		Token: "tok-123",
		Repo:  "golang/go",
	})

	assert.False(t, delivered)
	require.Error(t, err)
	assert.ErrorIs(t, err, reserveErr)
	assert.Equal(t, 0, sender.confirmationCalls)
}
