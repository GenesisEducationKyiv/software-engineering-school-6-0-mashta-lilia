package repository

import (
	"context"
	"database/sql"
	"fmt"
	"github-release-notifier/internal/model"
)

type TrackedRepoStore struct {
	db *sql.DB
}

func NewTrackedRepoStore(db *sql.DB) *TrackedRepoStore {
	return &TrackedRepoStore{db: db}
}

func (r *TrackedRepoStore) Upsert(ctx context.Context, owner, name string) (*model.TrackedRepository, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback() //nolint:errcheck // rollback error safe to ignore
	}()

	// INSERT if not exists, then always SELECT — avoids unnecessary row versioning.
	// Wrapped in a transaction so the row cannot be deleted between INSERT and SELECT.
	insertQuery := `
		INSERT INTO tracked_repositories (owner, name)
		VALUES ($1, $2)
		ON CONFLICT (owner, name) DO NOTHING`

	if _, err := tx.ExecContext(ctx, insertQuery, owner, name); err != nil {
		return nil, fmt.Errorf("inserting tracked repository: %w", err)
	}

	selectQuery := `
		SELECT id, owner, name, last_seen_tag, last_checked_at, created_at
		FROM tracked_repositories
		WHERE owner = $1 AND name = $2`

	repo := &model.TrackedRepository{}
	if err := tx.QueryRowContext(ctx, selectQuery, owner, name).Scan(
		&repo.ID, &repo.Owner, &repo.Name, &repo.LastSeenTag, &repo.LastCheckedAt, &repo.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("selecting tracked repository: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}
	return repo, nil
}

func (r *TrackedRepoStore) GetAll(ctx context.Context) ([]model.TrackedRepository, error) {
	query := `SELECT id, owner, name, last_seen_tag, last_checked_at, created_at FROM tracked_repositories`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying tracked repositories: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows close error is safe to ignore

	repos := make([]model.TrackedRepository, 0)
	for rows.Next() {
		var repo model.TrackedRepository
		if err := rows.Scan(
			&repo.ID, &repo.Owner, &repo.Name, &repo.LastSeenTag, &repo.LastCheckedAt, &repo.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning tracked repository row: %w", err)
		}
		repos = append(repos, repo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating tracked repository rows: %w", err)
	}
	return repos, nil
}

func (r *TrackedRepoStore) UpdateLastSeen(ctx context.Context, id int64, tag string) error {
	query := `UPDATE tracked_repositories SET last_seen_tag = $1, last_checked_at = NOW() WHERE id = $2`
	if _, err := r.db.ExecContext(ctx, query, tag, id); err != nil {
		return fmt.Errorf("updating last seen tag: %w", err)
	}
	return nil
}

func (r *TrackedRepoStore) UpdateLastChecked(ctx context.Context, id int64) error {
	query := `UPDATE tracked_repositories SET last_checked_at = NOW() WHERE id = $1`
	if _, err := r.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("updating last checked timestamp: %w", err)
	}
	return nil
}
