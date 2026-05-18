package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github-release-notifier/internal/subscription"
	"sync"

	"github.com/lib/pq"
)

// Postgres SQLSTATE for unique/partial-unique constraint violation.
const pgUniqueViolation = "23505"

// Name of the partial unique index that enforces "no duplicate active
// subscription for (email, repo)". Matches migrations/000001_init_schema.up.sql.
// Only this constraint should map to ErrAlreadyExists; other 23505s
// (e.g. the token UNIQUE column) indicate a different bug and must surface.
const subscriptionEmailRepoActiveIndex = "idx_subscriptions_email_repo_active"

const (
	subscriptionCreateQuery = `
		INSERT INTO subscriptions (email, repo_owner, repo_name, token, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`

	// #nosec G101 -- SQL column name, not a credential.
	subscriptionGetByTokenQuery = `
		SELECT id, email, repo_owner, repo_name, token, status, created_at, updated_at
		FROM subscriptions WHERE token = $1`

	subscriptionGetActiveByEmailQuery = `
		SELECT id, email, repo_owner, repo_name, token, status, created_at, updated_at
		FROM subscriptions WHERE email = $1 AND status = $2`

	subscriptionGetEmailsByRepoQuery = `
		SELECT email FROM subscriptions WHERE repo_owner = $1 AND repo_name = $2 AND status = $3`

	subscriptionUpdateStatusQuery = `
		UPDATE subscriptions SET status = $1 WHERE id = $2`

	subscriptionExistsQuery = `
		SELECT EXISTS(
			SELECT 1 FROM subscriptions
			WHERE email = $1 AND repo_owner = $2 AND repo_name = $3 AND status != $4
		)`
)

type SubscriptionRepo struct {
	db                   *sql.DB
	prepareMu            sync.Mutex
	prepared             bool
	stmtCreate           *sql.Stmt
	stmtGetByToken       *sql.Stmt
	stmtGetActiveByEmail *sql.Stmt
	stmtGetEmailsByRepo  *sql.Stmt
	stmtUpdateStatus     *sql.Stmt
	stmtExists           *sql.Stmt
}

func NewSubscriptionRepo(db *sql.DB) *SubscriptionRepo {
	return &SubscriptionRepo{db: db}
}

func NewSubscriptionRepoWithContext(ctx context.Context, db *sql.DB) (*SubscriptionRepo, error) {
	if ctx == nil {
		return nil, errors.New("subscription repo: nil context")
	}
	if db == nil {
		return nil, errors.New("subscription repo: nil db")
	}

	repo := &SubscriptionRepo{db: db}
	if err := repo.ensurePrepared(ctx); err != nil {
		return nil, errors.Join(err, repo.Close())
	}
	return repo, nil
}

func (r *SubscriptionRepo) ensurePrepared(ctx context.Context) error {
	if r == nil {
		return errors.New("subscription repo: nil receiver")
	}
	if ctx == nil {
		return errors.New("subscription repo: nil context")
	}
	if r.db == nil {
		return errors.New("subscription repo: nil db")
	}

	r.prepareMu.Lock()
	defer r.prepareMu.Unlock()
	if r.prepared {
		return nil
	}
	if err := r.prepare(ctx); err != nil {
		return err
	}
	r.prepared = true
	return nil
}

func (r *SubscriptionRepo) prepare(ctx context.Context) error {
	var err error
	if r.stmtCreate, err = r.db.PrepareContext(ctx, subscriptionCreateQuery); err != nil {
		return fmt.Errorf("preparing subscription create: %w", err)
	}
	if r.stmtGetByToken, err = r.db.PrepareContext(ctx, subscriptionGetByTokenQuery); err != nil {
		return fmt.Errorf("preparing subscription get by token: %w", err)
	}
	if r.stmtGetActiveByEmail, err = r.db.PrepareContext(ctx, subscriptionGetActiveByEmailQuery); err != nil {
		return fmt.Errorf("preparing subscription get active by email: %w", err)
	}
	if r.stmtGetEmailsByRepo, err = r.db.PrepareContext(ctx, subscriptionGetEmailsByRepoQuery); err != nil {
		return fmt.Errorf("preparing subscription get emails by repo: %w", err)
	}
	if r.stmtUpdateStatus, err = r.db.PrepareContext(ctx, subscriptionUpdateStatusQuery); err != nil {
		return fmt.Errorf("preparing subscription update status: %w", err)
	}
	if r.stmtExists, err = r.db.PrepareContext(ctx, subscriptionExistsQuery); err != nil {
		return fmt.Errorf("preparing subscription exists: %w", err)
	}
	return nil
}

func (r *SubscriptionRepo) Close() error {
	if r == nil {
		return nil
	}
	return errors.Join(
		closeStmt("subscription create", r.stmtCreate),
		closeStmt("subscription get by token", r.stmtGetByToken),
		closeStmt("subscription get active by email", r.stmtGetActiveByEmail),
		closeStmt("subscription get emails by repo", r.stmtGetEmailsByRepo),
		closeStmt("subscription update status", r.stmtUpdateStatus),
		closeStmt("subscription exists", r.stmtExists),
	)
}

func (r *SubscriptionRepo) Create(ctx context.Context, sub *subscription.Subscription) error {
	if err := r.ensurePrepared(ctx); err != nil {
		return err
	}
	if err := r.stmtCreate.QueryRowContext(ctx,
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
	if err := r.ensurePrepared(ctx); err != nil {
		return nil, err
	}
	sub := &subscription.Subscription{}
	err := r.stmtGetByToken.QueryRowContext(ctx, token).Scan(
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
	if err := r.ensurePrepared(ctx); err != nil {
		return nil, err
	}
	return r.scanSubscriptions(ctx, r.stmtGetActiveByEmail, email, subscription.StatusActive)
}

func (r *SubscriptionRepo) GetEmailsByRepo(ctx context.Context, owner, name string) ([]string, error) {
	if err := r.ensurePrepared(ctx); err != nil {
		return nil, err
	}
	rows, err := r.stmtGetEmailsByRepo.QueryContext(ctx, owner, name, subscription.StatusActive)
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
	if err := r.ensurePrepared(ctx); err != nil {
		return err
	}
	result, err := r.stmtUpdateStatus.ExecContext(ctx, status, id)
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
	if err := r.ensurePrepared(ctx); err != nil {
		return false, err
	}
	var exists bool
	if err := r.stmtExists.QueryRowContext(
		ctx, email, owner, name, subscription.StatusUnsubscribed,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("checking subscription existence: %w", err)
	}
	return exists, nil
}

func (r *SubscriptionRepo) scanSubscriptions(
	ctx context.Context, stmt *sql.Stmt, args ...interface{},
) ([]subscription.Subscription, error) {
	rows, err := stmt.QueryContext(ctx, args...)
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
