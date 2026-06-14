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

// fakeDedupStore tracks reserved keys in a map so repeated keys behave like the
// real INSERT ... ON CONFLICT DO NOTHING ledger: the first reserve of a key wins
// and later ones report already-reserved.
type fakeDedupStore struct {
	reserved map[string]bool
	err      error
	calls    int
	lastKind string
	lastKey  string
}

func newFakeDedupStore() *fakeDedupStore {
	return &fakeDedupStore{reserved: make(map[string]bool)}
}

func (f *fakeDedupStore) Reserve(_ context.Context, kind, dedupKey string) (bool, error) {
	f.calls++
	f.lastKind = kind
	f.lastKey = dedupKey
	if f.err != nil {
		return false, f.err
	}
	if f.reserved[dedupKey] {
		return false, nil
	}
	f.reserved[dedupKey] = true
	return true, nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestService_SendConfirmation_ReservesThenSends(t *testing.T) {
	t.Parallel()
	sender := &fakeSender{}
	dedup := newFakeDedupStore()
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
	assert.Equal(t, kindConfirmation, dedup.lastKind)
	assert.Equal(t, sha256Hex("confirm:tok-123"), dedup.lastKey)
}

func TestService_SendConfirmation_DedupConflictSkipsSend(t *testing.T) {
	t.Parallel()
	sender := &fakeSender{}
	dedup := newFakeDedupStore()
	dedup.reserved[sha256Hex("confirm:tok-123")] = true // already delivered earlier
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
	dedup := newFakeDedupStore()
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
	assert.Equal(t, kindRelease, dedup.lastKind)
	assert.Equal(t, sha256Hex("release:golang/go:v1.22.0:alice@example.com"), dedup.lastKey)
}

func TestService_SendErrorIsReturnedAfterReservation(t *testing.T) {
	t.Parallel()
	sendErr := errors.New("smtp down")
	sender := &fakeSender{err: sendErr}
	dedup := newFakeDedupStore()
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

// ADR-0015 failure window: Reserve succeeds, the send fails, and a later retry
// finds the key already reserved -> it returns delivered=false WITHOUT calling
// SMTP again. Guards against a refactor silently re-sending after a failure.
func TestService_FailedSendIsNotResentOnRetry(t *testing.T) {
	t.Parallel()
	sender := &fakeSender{err: errors.New("smtp down")}
	dedup := newFakeDedupStore()
	svc, err := NewService(sender, dedup, logger.Nop())
	require.NoError(t, err)

	rel := &ReleaseInfo{TagName: "v1.22.0"}

	delivered, err := svc.SendReleaseNotification(context.Background(), "alice@example.com", "golang/go", rel)
	require.Error(t, err)
	assert.False(t, delivered)
	require.Equal(t, 1, sender.releaseCalls)

	delivered, err = svc.SendReleaseNotification(context.Background(), "alice@example.com", "golang/go", rel)
	require.NoError(t, err)
	assert.False(t, delivered)
	assert.Equal(t, 1, sender.releaseCalls, "SMTP must not be retried once the row is reserved")
	assert.Equal(t, 2, dedup.calls, "the retry still consults the ledger")
}

func TestService_ReserveErrorSkipsSend(t *testing.T) {
	t.Parallel()
	reserveErr := errors.New("db down")
	sender := &fakeSender{}
	dedup := newFakeDedupStore()
	dedup.err = reserveErr
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
