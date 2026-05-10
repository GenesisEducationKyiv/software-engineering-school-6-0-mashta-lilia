package service

import (
	"context"
	"errors"
	"fmt"
	"github-release-notifier/internal/apperror"
	"github-release-notifier/internal/model"
	"log/slog"
	"net/mail"
	"strings"
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
	tokens TokenGenerator
}

func NewSubscriptionService(
	subs SubscriptionRepo,
	repos RepoStore,
	gh GitHubClient,
	m Mailer,
	tokens TokenGenerator,
) *SubscriptionService {
	return &SubscriptionService{
		subs:   subs,
		repos:  repos,
		github: gh,
		mailer: m,
		tokens: tokens,
	}
}

// Subscribe orchestrates the subscribe lifecycle. Each step is delegated to a
// focused helper so this method stays a readable outline of the flow.
func (s *SubscriptionService) Subscribe(ctx context.Context, email, repo string) error {
	email, owner, name, err := parseSubscribeInput(email, repo)
	if err != nil {
		return err
	}

	if err := s.ensureRepoExistsOnGitHub(ctx, owner, name); err != nil {
		return err
	}

	if err := s.ensureNoActiveSubscription(ctx, email, owner, name); err != nil {
		return err
	}

	sub, err := s.createPendingSubscription(ctx, email, owner, name)
	if err != nil {
		return err
	}

	return s.sendConfirmationOrRollback(ctx, sub, repo)
}

// parseSubscribeInput validates and normalizes the raw user input.
func parseSubscribeInput(email, repo string) (normalizedEmail, owner, name string, err error) {
	normalizedEmail, err = normalizeEmail(email)
	if err != nil {
		return "", "", "", ErrInvalidEmail
	}
	owner, name, err = parseRepo(repo)
	if err != nil {
		return "", "", "", ErrInvalidRepo
	}
	return normalizedEmail, owner, name, nil
}

// ensureRepoExistsOnGitHub returns ErrRepoNotFound if the repo does not exist
// on GitHub, or wraps the upstream error otherwise.
func (s *SubscriptionService) ensureRepoExistsOnGitHub(ctx context.Context, owner, name string) error {
	exists, err := s.github.RepoExists(ctx, owner, name)
	if err != nil {
		return fmt.Errorf("checking repo: %w", err)
	}
	if !exists {
		return ErrRepoNotFound
	}
	return nil
}

// ensureNoActiveSubscription returns ErrAlreadyExists if the email is already
// subscribed (pending or active) to the same repo.
func (s *SubscriptionService) ensureNoActiveSubscription(
	ctx context.Context, email, owner, name string,
) error {
	already, err := s.subs.Exists(ctx, email, owner, name)
	if err != nil {
		return fmt.Errorf("checking existing subscription: %w", err)
	}
	if already {
		return ErrAlreadyExists
	}
	return nil
}

// createPendingSubscription upserts the FK target (tracked_repositories) and
// then creates a pending subscription row. The order matters: the FK
// constraint requires the tracked repo to exist first.
func (s *SubscriptionService) createPendingSubscription(
	ctx context.Context, email, owner, name string,
) (*model.Subscription, error) {
	token, err := s.tokens.Generate()
	if err != nil {
		return nil, fmt.Errorf("generating token: %w", err)
	}

	if _, err := s.repos.Upsert(ctx, owner, name); err != nil {
		return nil, fmt.Errorf("upserting tracked repo: %w", err)
	}

	sub := &model.Subscription{
		Email:     email,
		RepoOwner: owner,
		RepoName:  name,
		Token:     token,
		Status:    model.StatusPending,
	}
	if err := s.subs.Create(ctx, sub); err != nil {
		return nil, fmt.Errorf("creating subscription: %w", err)
	}
	return sub, nil
}

// sendConfirmationOrRollback sends the confirmation email and, on failure,
// rolls the subscription back to "unsubscribed" so the partial unique index
// frees the slot and the user can retry without a permanent 409 Conflict.
func (s *SubscriptionService) sendConfirmationOrRollback(
	ctx context.Context, sub *model.Subscription, repo string,
) error {
	if err := s.mailer.SendConfirmation(ctx, sub.Email, sub.Token, repo); err != nil {
		if rollbackErr := s.subs.UpdateStatus(ctx, sub.ID, model.StatusUnsubscribed); rollbackErr != nil {
			slog.Error("failed to rollback subscription after email failure",
				"id", sub.ID, "error", rollbackErr)
		}
		return fmt.Errorf("%w: %w", ErrEmailSendFailed, err)
	}
	return nil
}

func (s *SubscriptionService) Confirm(ctx context.Context, token string) error {
	sub, err := s.subs.GetByToken(ctx, token)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
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
		if errors.Is(err, apperror.ErrNotFound) {
			return ErrTokenNotFound
		}
		return fmt.Errorf("looking up token: %w", err)
	}
	if sub.Status == model.StatusUnsubscribed {
		return nil // idempotent: already unsubscribed
	}
	return s.subs.UpdateStatus(ctx, sub.ID, model.StatusUnsubscribed)
}

func (s *SubscriptionService) GetSubscriptions(
	ctx context.Context, email string,
) ([]model.Subscription, error) {
	normalized, err := normalizeEmail(email)
	if err != nil {
		return nil, ErrInvalidEmail
	}
	return s.subs.GetActiveByEmail(ctx, normalized)
}

func normalizeEmail(raw string) (string, error) {
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return "", fmt.Errorf("parsing email address: %w", err)
	}
	return strings.ToLower(addr.Address), nil
}

func parseRepo(repo string) (owner, name string, err error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", ErrInvalidRepo
	}
	return owner, name, nil
}

