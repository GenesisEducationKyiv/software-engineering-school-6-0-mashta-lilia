package subscription

import (
	"context"
	"errors"
	"fmt"
	"github-release-notifier/internal/email"
	"github-release-notifier/internal/repository"
	"time"
)

const rollbackTimeout = 5 * time.Second

type Service struct {
	subs   subscriptionStore
	repos  repoUpserter
	github githubChecker
	mailer confirmationSender
	tokens tokenGen
}

// NewService panics if any collaborator is nil. Service is built once at
// startup by the composition root; a nil dep is a wiring bug that should
// crash boot rather than surface as a request-time nil-pointer panic.
func NewService(
	subs subscriptionStore,
	repos repoUpserter,
	gh githubChecker,
	m confirmationSender,
	tokens tokenGen,
) *Service {
	if subs == nil || repos == nil || gh == nil || m == nil || tokens == nil {
		panic("subscription.NewService: all dependencies must be non-nil")
	}
	return &Service{subs: subs, repos: repos, github: gh, mailer: m, tokens: tokens}
}

func (s *Service) Subscribe(ctx context.Context, rawEmail, rawRepo string) error {
	addr, ref, err := parseSubscribeInput(rawEmail, rawRepo)
	if err != nil {
		return err
	}
	if err := s.ensureRepoExistsOnGitHub(ctx, ref); err != nil {
		return err
	}
	if err := s.ensureNoActiveSubscription(ctx, addr, ref); err != nil {
		return err
	}
	sub, err := s.createPendingSubscription(ctx, addr, ref)
	if err != nil {
		return err
	}
	return s.sendConfirmationOrRollback(ctx, sub, ref)
}

func parseSubscribeInput(rawEmail, rawRepo string) (email.Address, repository.Ref, error) {
	addr, err := email.NewAddress(rawEmail)
	if err != nil {
		return email.Address{}, repository.Ref{}, ErrInvalidEmail
	}
	ref, err := repository.ParseRef(rawRepo)
	if err != nil {
		return email.Address{}, repository.Ref{}, ErrInvalidRepo
	}
	return addr, ref, nil
}

func (s *Service) ensureRepoExistsOnGitHub(ctx context.Context, ref repository.Ref) error {
	exists, err := s.github.RepoExists(ctx, ref.Owner, ref.Name)
	if err != nil {
		return fmt.Errorf("checking repo: %w", err)
	}
	if !exists {
		return ErrRepoNotFound
	}
	return nil
}

func (s *Service) ensureNoActiveSubscription(
	ctx context.Context, addr email.Address, ref repository.Ref,
) error {
	already, err := s.subs.Exists(ctx, addr.String(), ref.Owner, ref.Name)
	if err != nil {
		return fmt.Errorf("checking existing subscription: %w", err)
	}
	if already {
		return ErrAlreadyExists
	}
	return nil
}

func (s *Service) createPendingSubscription(
	ctx context.Context, addr email.Address, ref repository.Ref,
) (*Subscription, error) {
	token, err := s.tokens.Generate()
	if err != nil {
		return nil, fmt.Errorf("generating token: %w", err)
	}
	if err := s.repos.Upsert(ctx, ref.Owner, ref.Name); err != nil {
		return nil, fmt.Errorf("upserting tracked repo: %w", err)
	}
	sub := &Subscription{
		Email:     addr.String(),
		RepoOwner: ref.Owner,
		RepoName:  ref.Name,
		Token:     token,
		Status:    StatusPending,
	}
	if err := s.subs.Create(ctx, sub); err != nil {
		return nil, fmt.Errorf("creating subscription: %w", err)
	}
	return sub, nil
}

func (s *Service) sendConfirmationOrRollback(
	ctx context.Context, sub *Subscription, ref repository.Ref,
) error {
	if err := s.mailer.SendConfirmation(ctx, sub.Email, sub.Token, ref.String()); err != nil {
		// Detach cancellation so rollback survives an HTTP timeout / client
		// disconnect — a stuck pending row would block the partial unique
		// index from accepting a retry. WithoutCancel keeps the request's
		// trace IDs and logger; the timeout caps a hung DB.
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		if rbErr := s.subs.UpdateStatus(rollbackCtx, sub.ID, StatusUnsubscribed); rbErr != nil {
			return errors.Join(
				fmt.Errorf("%w: %w", ErrEmailSendFailed, err),
				fmt.Errorf("rollback after email failure: %w", rbErr),
			)
		}
		return fmt.Errorf("%w: %w", ErrEmailSendFailed, err)
	}
	return nil
}

func (s *Service) Confirm(ctx context.Context, token string) error {
	sub, err := s.subs.GetByToken(ctx, token)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrTokenNotFound
		}
		return fmt.Errorf("looking up token: %w", err)
	}
	if sub.Status == StatusActive {
		return nil // idempotent
	}
	if sub.Status != StatusPending {
		return ErrSubscriptionInactive
	}
	return s.subs.UpdateStatus(ctx, sub.ID, StatusActive)
}

func (s *Service) Unsubscribe(ctx context.Context, token string) error {
	sub, err := s.subs.GetByToken(ctx, token)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrTokenNotFound
		}
		return fmt.Errorf("looking up token: %w", err)
	}
	if sub.Status == StatusUnsubscribed {
		return nil // idempotent
	}
	return s.subs.UpdateStatus(ctx, sub.ID, StatusUnsubscribed)
}

func (s *Service) GetSubscriptions(ctx context.Context, rawEmail string) ([]Subscription, error) {
	addr, err := email.NewAddress(rawEmail)
	if err != nil {
		return nil, ErrInvalidEmail
	}
	return s.subs.GetActiveByEmail(ctx, addr.String())
}
