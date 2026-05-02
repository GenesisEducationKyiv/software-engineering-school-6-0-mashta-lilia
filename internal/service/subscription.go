package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/mail"
	"strings"

	"github-release-notifier/internal/model"
	"github-release-notifier/internal/repository"
)

var (
	ErrInvalidRepo          = errors.New("invalid repository format, expected owner/repo")
	ErrRepoNotFound         = errors.New("repository not found on GitHub")
	ErrAlreadyExists        = errors.New("subscription already exists")
	ErrTokenNotFound        = errors.New("subscription token not found")
	ErrSubscriptionInactive = errors.New("subscription is not active")
	ErrInvalidEmail         = errors.New("invalid email address")
	ErrEmailSendFailed      = errors.New("failed to send email")
)

type SubscriptionService struct {
	subs   SubscriptionRepo
	repos  RepoStore
	github GitHubClient
	mailer Mailer
}

func NewSubscriptionService(subs SubscriptionRepo, repos RepoStore, gh GitHubClient, m Mailer) *SubscriptionService {
	return &SubscriptionService{
		subs:   subs,
		repos:  repos,
		github: gh,
		mailer: m,
	}
}

func (s *SubscriptionService) Subscribe(ctx context.Context, email, repo string) error {
	normalized, err := normalizeEmail(email)
	if err != nil {
		return ErrInvalidEmail
	}
	email = normalized

	owner, name, err := parseRepo(repo)
	if err != nil {
		return ErrInvalidRepo
	}

	exists, err := s.github.RepoExists(ctx, owner, name)
	if err != nil {
		return fmt.Errorf("checking repo: %w", err)
	}
	if !exists {
		return ErrRepoNotFound
	}

	already, err := s.subs.Exists(ctx, email, owner, name)
	if err != nil {
		return fmt.Errorf("checking existing subscription: %w", err)
	}
	if already {
		return ErrAlreadyExists
	}

	token, err := generateToken()
	if err != nil {
		return fmt.Errorf("generating token: %w", err)
	}

	// Upsert tracked repo BEFORE creating subscription (FK constraint)
	if _, err := s.repos.Upsert(ctx, owner, name); err != nil {
		return fmt.Errorf("upserting tracked repo: %w", err)
	}

	sub := &model.Subscription{
		Email:     email,
		RepoOwner: owner,
		RepoName:  name,
		Token:     token,
		Status:    model.StatusPending,
	}

	if err := s.subs.Create(ctx, sub); err != nil {
		return fmt.Errorf("creating subscription: %w", err)
	}

	if err := s.mailer.SendConfirmation(ctx, email, token, repo); err != nil {
		// Compensate: mark as unsubscribed so the partial unique index frees the
		// slot and the user can retry. Without this, the orphaned pending row
		// causes a permanent 409 Conflict on retry.
		if rollbackErr := s.subs.UpdateStatus(ctx, sub.ID, model.StatusUnsubscribed); rollbackErr != nil {
			log.Printf("failed to rollback subscription %d after email failure: %v", sub.ID, rollbackErr)
		}
		return fmt.Errorf("%w: %v", ErrEmailSendFailed, err)
	}

	return nil
}

func (s *SubscriptionService) Confirm(ctx context.Context, token string) error {
	sub, err := s.subs.GetByToken(ctx, token)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrTokenNotFound
		}
		return fmt.Errorf("looking up token: %w", err)
	}
	if sub.Status == model.StatusActive {
		return nil // idempotent: already confirmed
	}
	if sub.Status != model.StatusPending {
		return ErrSubscriptionInactive
	}
	return s.subs.UpdateStatus(ctx, sub.ID, model.StatusActive)
}

func (s *SubscriptionService) Unsubscribe(ctx context.Context, token string) error {
	sub, err := s.subs.GetByToken(ctx, token)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrTokenNotFound
		}
		return fmt.Errorf("looking up token: %w", err)
	}
	if sub.Status == model.StatusUnsubscribed {
		return nil // idempotent: already unsubscribed
	}
	return s.subs.UpdateStatus(ctx, sub.ID, model.StatusUnsubscribed)
}

func (s *SubscriptionService) GetSubscriptions(ctx context.Context, email string) ([]model.Subscription, error) {
	normalized, err := normalizeEmail(email)
	if err != nil {
		return nil, ErrInvalidEmail
	}
	return s.subs.GetActiveByEmail(ctx, normalized)
}

func normalizeEmail(raw string) (string, error) {
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return "", err
	}
	return strings.ToLower(addr.Address), nil
}

func parseRepo(repo string) (string, string, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", ErrInvalidRepo
	}
	return owner, name, nil
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
