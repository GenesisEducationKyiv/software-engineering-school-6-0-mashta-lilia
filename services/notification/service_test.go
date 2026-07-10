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

const testConfirmURL = "https://app.example/api/confirm/tok-123"

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

// fakeDedupStore mirrors the real upsert ledger: a dedup key can be reserved
// but unconfirmed (a prior send failed, so redelivery may retry it) or
// confirmed (a real duplicate, never re-reserved).
type fakeDedupStore struct {
	confirmed  map[string]bool
	err        error
	confirmErr error
	calls      int
	lastKind   string
	lastKey    string
}

func newFakeDedupStore() *fakeDedupStore {
	return &fakeDedupStore{confirmed: make(map[string]bool)}
}

func (f *fakeDedupStore) Reserve(_ context.Context, kind, dedupKey string) (bool, error) {
	f.calls++
	f.lastKind = kind
	f.lastKey = dedupKey
	if f.err != nil {
		return false, f.err
	}
	if f.confirmed[dedupKey] {
		return false, nil
	}
	return true, nil
}

func (f *fakeDedupStore) Confirm(_ context.Context, dedupKey string) error {
	if f.confirmErr != nil {
		return f.confirmErr
	}
	f.confirmed[dedupKey] = true
	return nil
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
		Email:      "alice@example.com",
		ConfirmURL: testConfirmURL,
		Repo:       "golang/go",
	})

	require.NoError(t, err)
	assert.True(t, delivered)
	assert.Equal(t, 1, sender.confirmationCalls)
	assert.Equal(t, kindConfirmation, dedup.lastKind)
	assert.Equal(t, sha256Hex("confirm:"+testConfirmURL), dedup.lastKey)
}

func TestService_SendConfirmation_DedupConflictSkipsSend(t *testing.T) {
	t.Parallel()
	sender := &fakeSender{}
	dedup := newFakeDedupStore()
	dedup.confirmed[sha256Hex("confirm:"+testConfirmURL)] = true // already delivered earlier
	svc, err := NewService(sender, dedup, logger.Nop())
	require.NoError(t, err)

	delivered, err := svc.SendConfirmation(context.Background(), Confirmation{
		Email:      "alice@example.com",
		ConfirmURL: testConfirmURL,
		Repo:       "golang/go",
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

// The ledger row stays unconfirmed until send() succeeds, so a redelivery
// after a failed send retries SMTP instead of silently dropping the email.
func TestService_FailedSendIsRetriedOnRedelivery(t *testing.T) {
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

	sender.err = nil // the transient failure clears before the broker redelivers
	delivered, err = svc.SendReleaseNotification(context.Background(), "alice@example.com", "golang/go", rel)
	require.NoError(t, err)
	assert.True(t, delivered)
	assert.Equal(t, 2, sender.releaseCalls, "an unconfirmed reservation must let redelivery retry SMTP")
	assert.Equal(t, 2, dedup.calls, "the retry still consults the ledger")
}

// Once a send is confirmed, a later redelivery of the same command is a true
// duplicate and must not reach SMTP again.
func TestService_ConfirmedSendIsNotResentOnRedelivery(t *testing.T) {
	t.Parallel()
	sender := &fakeSender{}
	dedup := newFakeDedupStore()
	svc, err := NewService(sender, dedup, logger.Nop())
	require.NoError(t, err)

	rel := &ReleaseInfo{TagName: "v1.22.0"}

	delivered, err := svc.SendReleaseNotification(context.Background(), "alice@example.com", "golang/go", rel)
	require.NoError(t, err)
	require.True(t, delivered)
	require.Equal(t, 1, sender.releaseCalls)

	delivered, err = svc.SendReleaseNotification(context.Background(), "alice@example.com", "golang/go", rel)
	require.NoError(t, err)
	assert.False(t, delivered, "a confirmed send is a true duplicate")
	assert.Equal(t, 1, sender.releaseCalls, "SMTP must not be retried once the send is confirmed")
}

// A failure to write the confirmation must not be reported as a send failure:
// the email already went out, so the caller should still see delivered=true.
func TestService_ConfirmErrorStillReportsDelivered(t *testing.T) {
	t.Parallel()
	sender := &fakeSender{}
	dedup := newFakeDedupStore()
	dedup.confirmErr = errors.New("db down")
	svc, err := NewService(sender, dedup, logger.Nop())
	require.NoError(t, err)

	delivered, err := svc.SendReleaseNotification(
		context.Background(), "alice@example.com", "golang/go", &ReleaseInfo{TagName: "v1.22.0"},
	)

	require.NoError(t, err)
	assert.True(t, delivered)
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
		Email:      "alice@example.com",
		ConfirmURL: testConfirmURL,
		Repo:       "golang/go",
	})

	assert.False(t, delivered)
	require.Error(t, err)
	assert.ErrorIs(t, err, reserveErr)
	assert.Equal(t, 0, sender.confirmationCalls)
}
