package subscription

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github-release-notifier/internal/platform/logger"
	"sync"

	"github.com/lib/pq"
)

const pgUniqueViolation = "23505"

// Filter on this index so other 23505s (e.g. token UNIQUE) don't get mis-mapped to ErrAlreadyExists.
const emailRepoActiveIndex = "idx_subscriptions_email_repo_active"

const (
	createQuery = `
		INSERT INTO subscriptions (email, repo_owner, repo_name, token, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`

	// #nosec G101 -- SQL column name, not a credential.
	getByTokenQuery = `
		SELECT id, email, repo_owner, repo_name, token, status, created_at, updated_at
		FROM subscriptions WHERE token = $1`

	getActiveByEmailQuery = `
		SELECT id, email, repo_owner, repo_name, token, status, created_at, updated_at
		FROM subscriptions WHERE email = $1 AND status = $2`

	getEmailsByRepoQuery = `
		SELECT email FROM subscriptions WHERE repo_owner = $1 AND repo_name = $2 AND status = $3`

	updateStatusQuery = `
		UPDATE subscriptions SET status = $1 WHERE id = $2`

	existsQuery = `
		SELECT EXISTS(
			SELECT 1 FROM subscriptions
			WHERE email = $1 AND repo_owner = $2 AND repo_name = $3 AND status != $4
		)`
)

type Repo struct {
	db                   *sql.DB
	prepareMu            sync.Mutex
	prepared             bool
	stmtCreate           *sql.Stmt
	stmtGetByToken       *sql.Stmt
	stmtGetActiveByEmail *sql.Stmt
	stmtGetEmailsByRepo  *sql.Stmt
	stmtUpdateStatus     *sql.Stmt
	stmtExists           *sql.Stmt
	log                  *logger.Logger
}

func NewRepo(db *sql.DB, log *logger.Logger) *Repo {
	if log == nil {
		log = logger.Nop()
	}
	return &Repo{db: db, log: log}
}

func NewRepoWithContext(ctx context.Context, db *sql.DB, log *logger.Logger) (*Repo, error) {
	if ctx == nil {
		return nil, errors.New("subscription repo: nil context")
	}
	if db == nil {
		return nil, errors.New("subscription repo: nil db")
	}
	if log == nil {
		log = logger.Nop()
	}

	r := &Repo{db: db, log: log}
	if err := r.ensurePrepared(ctx); err != nil {
		return nil, errors.Join(err, r.Close())
	}
	return r, nil
}

func (r *Repo) ensurePrepared(ctx context.Context) error {
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

func (r *Repo) prepare(ctx context.Context) error {
	var err error
	if r.stmtCreate, err = r.db.PrepareContext(ctx, createQuery); err != nil {
		r.log.Error(ctx, "subscription_repo_prepare_failed", "statement", "create", "err", err)
		return fmt.Errorf("preparing subscription create: %w", err)
	}
	if r.stmtGetByToken, err = r.db.PrepareContext(ctx, getByTokenQuery); err != nil {
		r.log.Error(ctx, "subscription_repo_prepare_failed", "statement", "get_by_token", "err", err)
		return fmt.Errorf("preparing subscription get by token: %w", err)
	}
	if r.stmtGetActiveByEmail, err = r.db.PrepareContext(ctx, getActiveByEmailQuery); err != nil {
		r.log.Error(ctx, "subscription_repo_prepare_failed", "statement", "get_active_by_email", "err", err)
		return fmt.Errorf("preparing subscription get active by email: %w", err)
	}
	if r.stmtGetEmailsByRepo, err = r.db.PrepareContext(ctx, getEmailsByRepoQuery); err != nil {
		r.log.Error(ctx, "subscription_repo_prepare_failed", "statement", "get_emails_by_repo", "err", err)
		return fmt.Errorf("preparing subscription get emails by repo: %w", err)
	}
	if r.stmtUpdateStatus, err = r.db.PrepareContext(ctx, updateStatusQuery); err != nil {
		r.log.Error(ctx, "subscription_repo_prepare_failed", "statement", "update_status", "err", err)
		return fmt.Errorf("preparing subscription update status: %w", err)
	}
	if r.stmtExists, err = r.db.PrepareContext(ctx, existsQuery); err != nil {
		r.log.Error(ctx, "subscription_repo_prepare_failed", "statement", "exists", "err", err)
		return fmt.Errorf("preparing subscription exists: %w", err)
	}
	return nil
}

func (r *Repo) Close() error {
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

func (r *Repo) Create(ctx context.Context, sub *Subscription) error {
	if sub == nil {
		return errors.New("subscription repo: nil subscription")
	}
	if err := r.ensurePrepared(ctx); err != nil {
		return err
	}
	if err := r.stmtCreate.QueryRowContext(
		ctx,
		sub.Email, sub.RepoOwner, sub.RepoName, sub.Token, sub.Status,
	).Scan(&sub.ID, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
		// Authoritative guard for concurrent dupes (the service pre-check is a soft optimization).
		var pqErr *pq.Error
		if errors.As(err, &pqErr) &&
			pqErr.Code == pgUniqueViolation &&
			pqErr.Constraint == emailRepoActiveIndex {
			return ErrAlreadyExists
		}
		return fmt.Errorf("creating subscription owner=%s name=%s: %w", sub.RepoOwner, sub.RepoName, err)
	}
	return nil
}

func (r *Repo) GetByToken(ctx context.Context, token string) (*Subscription, error) {
	if err := r.ensurePrepared(ctx); err != nil {
		return nil, err
	}
	sub := &Subscription{}
	err := r.stmtGetByToken.QueryRowContext(ctx, token).Scan(
		&sub.ID, &sub.Email, &sub.RepoOwner, &sub.RepoName,
		&sub.Token, &sub.Status, &sub.CreatedAt, &sub.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying subscription by token: %w", err)
	}
	return sub, nil
}

func (r *Repo) GetActiveByEmail(ctx context.Context, email string) ([]Subscription, error) {
	if err := r.ensurePrepared(ctx); err != nil {
		return nil, err
	}
	return r.scan(ctx, r.stmtGetActiveByEmail, email, StatusActive)
}

func (r *Repo) GetEmailsByRepo(ctx context.Context, owner, name string) ([]string, error) {
	if err := r.ensurePrepared(ctx); err != nil {
		return nil, err
	}
	rows, err := r.stmtGetEmailsByRepo.QueryContext(ctx, owner, name, StatusActive)
	if err != nil {
		return nil, fmt.Errorf("querying subscriber emails owner=%s name=%s: %w", owner, name, err)
	}
	defer rows.Close() //nolint:errcheck // rows close error is safe to ignore

	emails := make([]string, 0)
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, fmt.Errorf("scanning subscriber email owner=%s name=%s: %w", owner, name, err)
		}
		emails = append(emails, email)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating subscriber emails owner=%s name=%s: %w", owner, name, err)
	}
	return emails, nil
}

func (r *Repo) UpdateStatus(ctx context.Context, id int64, status Status) error {
	if err := r.ensurePrepared(ctx); err != nil {
		return err
	}
	result, err := r.stmtUpdateStatus.ExecContext(ctx, status, id)
	if err != nil {
		return fmt.Errorf("updating subscription status id=%d status=%s: %w", id, status, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("getting rows affected id=%d status=%s: %w", id, status, err)
	}
	if n == 0 {
		r.log.Warn(ctx, "subscription_update_no_rows", "id", id, "status", status)
		return ErrNotFound
	}
	return nil
}

func (r *Repo) Exists(ctx context.Context, email, owner, name string) (bool, error) {
	if err := r.ensurePrepared(ctx); err != nil {
		return false, err
	}
	var exists bool
	if err := r.stmtExists.QueryRowContext(
		ctx, email, owner, name, StatusUnsubscribed,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("checking subscription existence owner=%s name=%s: %w", owner, name, err)
	}
	return exists, nil
}

func (r *Repo) scan(
	ctx context.Context, stmt *sql.Stmt, args ...interface{},
) ([]Subscription, error) {
	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("querying subscriptions: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows close error is safe to ignore

	subs := make([]Subscription, 0)
	for rows.Next() {
		var sub Subscription
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
	_ = ctx
	return subs, nil
}

func closeStmt(name string, stmt *sql.Stmt) error {
	if stmt == nil {
		return nil
	}
	if err := stmt.Close(); err != nil {
		return fmt.Errorf("closing %s statement: %w", name, err)
	}
	return nil
}
