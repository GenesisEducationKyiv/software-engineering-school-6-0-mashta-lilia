package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github-release-notifier/internal/subscription"

	"github.com/lib/pq"
)

// Postgres SQLSTATE for unique/partial-unique constraint violation.
const pgUniqueViolation = "23505"

// Name of the partial unique index that enforces "no duplicate active
// subscription for (email, repo)". Matches migrations/000001_init_schema.up.sql.
// Only this constraint should map to ErrAlreadyExists; other 23505s
// (e.g. the token UNIQUE column) indicate a different bug and must surface.
const subscriptionEmailRepoActiveIndex = "idx_subscriptions_email_repo_active"

type SubscriptionRepo struct {
	db *sql.DB
}

func NewSubscriptionRepo(db *sql.DB) *SubscriptionRepo {
	return &SubscriptionRepo{db: db}
}

func (r *SubscriptionRepo) Create(ctx context.Context, sub *subscription.Subscription) error {
	query := `
		INSERT INTO subscriptions (email, repo_owner, repo_name, token, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`

	if err := r.db.QueryRowContext(ctx, query,
		sub.Email, sub.RepoOwner, sub.RepoName, sub.Token, sub.Status,
	).Scan(&sub.ID, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
		// Partial-unique-index violation -> domain sentinel so concurrent
		// duplicate subscribes return 409, not 500. The service pre-check
		// is a soft optimization; this is the authoritative guard. Filter
		// by constraint name so a future unique index (e.g. on token)
		// doesn't get mis-classified as "duplicate subscription".
		var pqErr *pq.Error
		if errors.As(err, &pqErr) &&
			pqErr.Code == pgUniqueViolation &&
			pqErr.Constraint == subscriptionEmailRepoActiveIndex {
			return subscription.ErrAlreadyExists
		}
		return fmt.Errorf("creating subscription: %w", err)
	}
	return nil
}

func (r *SubscriptionRepo) GetByToken(ctx context.Context, token string) (*subscription.Subscription, error) {
	query := `
		SELECT id, email, repo_owner, repo_name, token, status, created_at, updated_at
		FROM subscriptions WHERE token = $1`

	sub := &subscription.Subscription{}
	err := r.db.QueryRowContext(ctx, query, token).Scan(
		&sub.ID, &sub.Email, &sub.RepoOwner, &sub.RepoName,
		&sub.Token, &sub.Status, &sub.CreatedAt, &sub.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, subscription.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying subscription by token: %w", err)
	}
	return sub, nil
}

func (r *SubscriptionRepo) GetActiveByEmail(
	ctx context.Context, email string,
) ([]subscription.Subscription, error) {
	query := `
		SELECT id, email, repo_owner, repo_name, token, status, created_at, updated_at
		FROM subscriptions WHERE email = $1 AND status = $2`

	return r.scanSubscriptions(ctx, query, email, subscription.StatusActive)
}

func (r *SubscriptionRepo) GetEmailsByRepo(ctx context.Context, owner, name string) ([]string, error) {
	query := `SELECT email FROM subscriptions WHERE repo_owner = $1 AND repo_name = $2 AND status = $3`

	rows, err := r.db.QueryContext(ctx, query, owner, name, subscription.StatusActive)
	if err != nil {
		return nil, fmt.Errorf("querying subscriber emails: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows close error is safe to ignore

	emails := make([]string, 0)
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, fmt.Errorf("scanning subscriber email: %w", err)
		}
		emails = append(emails, email)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating subscriber emails: %w", err)
	}
	return emails, nil
}

func (r *SubscriptionRepo) UpdateStatus(
	ctx context.Context, id int64, status subscription.Status,
) error {
	query := `UPDATE subscriptions SET status = $1 WHERE id = $2`
	result, err := r.db.ExecContext(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("updating subscription status: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("getting rows affected: %w", err)
	}
	if n == 0 {
		return subscription.ErrNotFound
	}
	return nil
}

func (r *SubscriptionRepo) Exists(ctx context.Context, email, owner, name string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM subscriptions
			WHERE email = $1 AND repo_owner = $2 AND repo_name = $3 AND status != $4
		)`

	var exists bool
	if err := r.db.QueryRowContext(
		ctx, query, email, owner, name, subscription.StatusUnsubscribed,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("checking subscription existence: %w", err)
	}
	return exists, nil
}

func (r *SubscriptionRepo) scanSubscriptions(
	ctx context.Context, query string, args ...interface{},
) ([]subscription.Subscription, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying subscriptions: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows close error is safe to ignore

	subs := make([]subscription.Subscription, 0)
	for rows.Next() {
		var sub subscription.Subscription
		if err := rows.Scan(
			&sub.ID, &sub.Email, &sub.RepoOwner, &sub.RepoName,
			&sub.Token, &sub.Status, &sub.CreatedAt, &sub.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning subscription row: %w", err)
		}
		subs = append(subs, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating subscription rows: %w", err)
	}
	return subs, nil
}
