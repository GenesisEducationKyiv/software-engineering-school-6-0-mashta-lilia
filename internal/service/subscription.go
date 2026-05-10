package service

import (
	"context"
	"errors"
	"fmt"
	"github-release-notifier/internal/apperror"
	"github-release-notifier/internal/model"
	"log/slog"
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

// subscriptionStore is the narrow composition SubscriptionService actually
// uses — write + read access, but NOT the SubscriberLister role that belongs
// to Scanner. Per ISP, the field type advertises only the methods we depend
// on, not the full SubscriptionRepo union.
type subscriptionStore interface {
	SubscriptionWriter
	SubscriptionReader
}

type SubscriptionService struct {
	subs   subscriptionStore
	repos  RepoUpserter
	github GitHubClient
	mailer ConfirmationSender
	tokens TokenGenerator
}

func NewSubscriptionService(
	subs subscriptionStore,
	repos RepoUpserter,
	gh GitHubClient,
	m ConfirmationSender,
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
func (s *SubscriptionService) Subscribe(ctx context.Context, rawEmail, rawRepo string) error {
	email, repo, err := parseSubscribeInput(rawEmail, rawRepo)
	if err != nil {
		return err
	}

	if err := s.ensureRepoExistsOnGitHub(ctx, repo); err != nil {
		return err
	}

	if err := s.ensureNoActiveSubscription(ctx, email, repo); err != nil {
		return err
	}

	sub, err := s.createPendingSubscription(ctx, email, repo)
	if err != nil {
		return err
	}

	return s.sendConfirmationOrRollback(ctx, sub, repo)
}

// parseSubscribeInput validates and normalizes raw user input into domain
// value objects. The model package owns "what is a valid email/repo" —
// service only translates parse failures into its own error sentinels.
func parseSubscribeInput(rawEmail, rawRepo string) (model.Email, model.RepoRef, error) {
	email, err := model.NewEmail(rawEmail)
	if err != nil {
		return model.Email{}, model.RepoRef{}, ErrInvalidEmail
	}
	repo, err := model.ParseRepoRef(rawRepo)
	if err != nil {
		return model.Email{}, model.RepoRef{}, ErrInvalidRepo
	}
	return email, repo, nil
}

// ensureRepoExistsOnGitHub returns ErrRepoNotFound if the repo does not exist
// on GitHub, or wraps the upstream error otherwise.
func (s *SubscriptionService) ensureRepoExistsOnGitHub(ctx context.Context, repo model.RepoRef) error {
	exists, err := s.github.RepoExists(ctx, repo.Owner, repo.Name)
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
	ctx context.Context, email model.Email, repo model.RepoRef,
) error {
	already, err := s.subs.Exists(ctx, email.String(), repo.Owner, repo.Name)
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
	ctx context.Context, email model.Email, repo model.RepoRef,
) (*model.Subscription, error) {
	token, err := s.tokens.Generate()
	if err != nil {
		return nil, fmt.Errorf("generating token: %w", err)
	}

	if _, err := s.repos.Upsert(ctx, repo.Owner, repo.Name); err != nil {
		return nil, fmt.Errorf("upserting tracked repo: %w", err)
	}

	sub := &model.Subscription{
		Email:     email.String(),
		RepoOwner: repo.Owner,
		RepoName:  repo.Name,
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
	ctx context.Context, sub *model.Subscription, repo model.RepoRef,
) error {
	if err := s.mailer.SendConfirmation(ctx, sub.Email, sub.Token, repo.String()); err != nil {
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
	ctx context.Context, rawEmail string,
) ([]model.Subscription, error) {
	email, err := model.NewEmail(rawEmail)
	if err != nil {
		return nil, ErrInvalidEmail
	}
	return s.subs.GetActiveByEmail(ctx, email.String())
}
