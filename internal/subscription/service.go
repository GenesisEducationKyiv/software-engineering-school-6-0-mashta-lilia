package subscription

import (
	"context"
	"errors"
	"fmt"
	"github-release-notifier/internal/email"
	"github-release-notifier/internal/repository"
	"github-release-notifier/internal/saga"
)

type Service struct {
	subs   subscriptionStore
	repos  repoUpserter
	github githubChecker
	saga   subscriptionSaga
	tokens tokenGen
	links  confirmationLinkBuilder
}

// Errors on any nil dep: built once at boot, so a wiring bug fails startup
// rather than surfacing on a request.
func NewService(
	subs subscriptionStore,
	repos repoUpserter,
	gh githubChecker,
	orch subscriptionSaga,
	tokens tokenGen,
	links confirmationLinkBuilder,
) (*Service, error) {
	if subs == nil || repos == nil || gh == nil || orch == nil || tokens == nil || links == nil {
		return nil, errors.New("subscription.NewService: all dependencies must be non-nil")
	}
	return &Service{subs: subs, repos: repos, github: gh, saga: orch, tokens: tokens, links: links}, nil
}

func (s *Service) Subscribe(ctx context.Context, rawEmail, rawRepo string) error {
	addr, ref, err := parseSubscribeInput(rawEmail, rawRepo)
	if err != nil {
		return err
	}
	if err := s.ensureRepoExistsOnGitHub(ctx, ref); err != nil {
		return err
	}
	sub, err := s.reserveSubscription(ctx, addr, ref)
	if err != nil {
		return err
	}
	// Hand off to the orchestrated saga: it dispatches the confirmation and
	// compensates (cancels the row) if delivery fails or times out.
	if err := s.saga.StartAndWait(ctx, saga.SubscriptionData{
		Email:          sub.Email,
		Repo:           ref.String(),
		Owner:          ref.Owner,
		Name:           ref.Name,
		Token:          sub.Token,
		ConfirmURL:     s.links.ConfirmURL(sub.Token),
		SubscriptionID: sub.ID,
	}); err != nil {
		return fmt.Errorf("%w: %w", ErrEmailSendFailed, err)
	}
	return nil
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

// reserveSubscription refreshes a still-pending row (so a never-delivered
// confirmation can be retried) instead of rejecting the re-subscribe.
func (s *Service) reserveSubscription(
	ctx context.Context, addr email.Address, ref repository.Ref,
) (*Subscription, error) {
	existing, err := s.subs.GetByEmailAndRepo(ctx, addr.String(), ref.Owner, ref.Name)
	switch {
	case err == nil:
		if existing.Status == StatusActive {
			return nil, ErrAlreadyExists
		}
		return s.refreshPendingSubscription(ctx, existing)
	case errors.Is(err, ErrNotFound):
		return s.createPendingSubscription(ctx, addr, ref)
	default:
		return nil, fmt.Errorf("checking existing subscription: %w", err)
	}
}

// refreshPendingSubscription re-issues the token on a still-pending row so the
// confirmation can be resent without tripping the partial unique index (ADR-0008).
// UpdateToken is a CAS on the old token: if a concurrent re-subscribe already
// refreshed this row, ours loses the race (ErrNotFound) and we report
// ErrAlreadyExists rather than emailing a confirm link for a token we never
// actually wrote.
func (s *Service) refreshPendingSubscription(
	ctx context.Context, sub *Subscription,
) (*Subscription, error) {
	token, err := s.tokens.Generate()
	if err != nil {
		return nil, fmt.Errorf("generating token: %w", err)
	}
	if err := s.subs.UpdateToken(ctx, sub.ID, sub.Token, token); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrAlreadyExists
		}
		return nil, fmt.Errorf("refreshing confirmation token: %w", err)
	}
	sub.Token = token
	return sub, nil
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

func (s *Service) Confirm(ctx context.Context, token string) error {
	sub, err := s.subs.GetByToken(ctx, token)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrTokenNotFound
		}
		return fmt.Errorf("looking up token: %w", err)
	}
	if sub.Status == StatusActive {
		return nil
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
		return nil
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
