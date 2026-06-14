package notification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github-release-notifier/internal/platform/logger"
)

const (
	kindConfirmation = "confirmation"
	kindRelease      = "release"
)

type sender interface {
	SendConfirmation(ctx context.Context, confirmation Confirmation) error
	SendReleaseNotification(ctx context.Context, email, repo string, rel *ReleaseInfo) error
}

type dedupStore interface {
	Reserve(ctx context.Context, kind, dedupKey string) (bool, error)
}

type Service struct {
	sender sender
	dedup  dedupStore
	log    *logger.Logger
}

func NewService(sender sender, dedup dedupStore, log *logger.Logger) (*Service, error) {
	if sender == nil {
		return nil, errors.New("notification service: sender is nil")
	}
	if dedup == nil {
		return nil, errors.New("notification service: dedup store is nil")
	}
	if log == nil {
		log = logger.Nop()
	}
	return &Service{sender: sender, dedup: dedup, log: log}, nil
}

func (s *Service) SendConfirmation(
	ctx context.Context, confirmation Confirmation,
) (bool, error) {
	dedupKey := hashDedupKey("confirm:" + confirmation.Token)
	return s.reserveAndSend(ctx, kindConfirmation, dedupKey, func() error {
		return s.sender.SendConfirmation(ctx, confirmation)
	})
}

func (s *Service) SendReleaseNotification(
	ctx context.Context, email, repo string, rel *ReleaseInfo,
) (bool, error) {
	tag := ""
	if rel != nil {
		tag = rel.TagName
	}
	dedupKey := hashDedupKey(fmt.Sprintf("release:%s:%s:%s", repo, tag, email))
	return s.reserveAndSend(ctx, kindRelease, dedupKey, func() error {
		return s.sender.SendReleaseNotification(ctx, email, repo, rel)
	})
}

// Hashed so the key is fixed-width and keeps tokens and emails out of the DB, logs, and errors.
func hashDedupKey(logical string) string {
	sum := sha256.Sum256([]byte(logical))
	return hex.EncodeToString(sum[:])
}

func (s *Service) reserveAndSend(
	ctx context.Context, kind, dedupKey string, send func() error,
) (bool, error) {
	reserved, err := s.dedup.Reserve(ctx, kind, dedupKey)
	if err != nil {
		return false, fmt.Errorf("reserving notification: %w", err)
	}
	if !reserved {
		s.log.Info(ctx, "notification_deduped", "kind", kind, "dedup_key", dedupKey)
		return false, nil
	}
	if err := send(); err != nil {
		return false, fmt.Errorf("sending notification kind=%s: %w", kind, err)
	}
	return true, nil
}
