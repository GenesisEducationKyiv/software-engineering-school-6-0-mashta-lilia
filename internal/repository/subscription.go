package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github-release-notifier/internal/model"
)

var ErrNotFound = errors.New("not found")

type SubscriptionRepo struct {
	db *sql.DB
}

func NewSubscriptionRepo(db *sql.DB) *SubscriptionRepo {
	return &SubscriptionRepo{db: db}
}

func (r *SubscriptionRepo) Create(ctx context.Context, sub *model.Subscription) error {
	query := `
		INSERT INTO subscriptions (email, repo_owner, repo_name, token, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`

	return r.db.QueryRowContext(ctx, query,
		sub.Email, sub.RepoOwner, sub.RepoName, sub.Token, sub.Status,
	).Scan(&sub.ID, &sub.CreatedAt, &sub.UpdatedAt)
}

func (r *SubscriptionRepo) GetByToken(ctx context.Context, token string) (*model.Subscription, error) {
	query := `
		SELECT id, email, repo_owner, repo_name, token, status, created_at, updated_at
		FROM subscriptions WHERE token = $1`

	sub := &model.Subscription{}
	err := r.db.QueryRowContext(ctx, query, token).Scan(
		&sub.ID, &sub.Email, &sub.RepoOwner, &sub.RepoName,
		&sub.Token, &sub.Status, &sub.CreatedAt, &sub.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("subscription token: %w", ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("querying subscription by token: %w", err)
	}
	return sub, nil
}

func (r *SubscriptionRepo) GetActiveByEmail(ctx context.Context, email string) ([]model.Subscription, error) {
	query := `
		SELECT id, email, repo_owner, repo_name, token, status, created_at, updated_at
		FROM subscriptions WHERE email = $1 AND status = $2`

	return r.scanSubscriptions(ctx, query, email, model.StatusActive)
}

func (r *SubscriptionRepo) GetEmailsByRepo(ctx context.Context, owner, name string) ([]string, error) {
	query := `SELECT email FROM subscriptions WHERE repo_owner = $1 AND repo_name = $2 AND status = $3`

	rows, err := r.db.QueryContext(ctx, query, owner, name, model.StatusActive)
	if err != nil {
		return nil, fmt.Errorf("querying subscriber emails: %w", err)
	}
	defer rows.Close()

	emails := make([]string, 0)
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, fmt.Errorf("scanning subscriber email: %w", err)
		}
		emails = append(emails, email)
	}
	return emails, rows.Err()
}

func (r *SubscriptionRepo) UpdateStatus(ctx context.Context, id int64, status model.SubscriptionStatus) error {
	// updated_at is set by the database trigger
	query := `UPDATE subscriptions SET status = $1 WHERE id = $2`
	result, err := r.db.ExecContext(ctx, query, status, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
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
	err := r.db.QueryRowContext(ctx, query, email, owner, name, model.StatusUnsubscribed).Scan(&exists)
	return exists, err
}

// scanSubscriptions is a shared helper that eliminates the duplicated scan logic.
func (r *SubscriptionRepo) scanSubscriptions(ctx context.Context, query string, args ...interface{}) ([]model.Subscription, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying subscriptions: %w", err)
	}
	defer rows.Close()

	subs := make([]model.Subscription, 0)
	for rows.Next() {
		var sub model.Subscription
		if err := rows.Scan(
			&sub.ID, &sub.Email, &sub.RepoOwner, &sub.RepoName,
			&sub.Token, &sub.Status, &sub.CreatedAt, &sub.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning subscription row: %w", err)
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}
