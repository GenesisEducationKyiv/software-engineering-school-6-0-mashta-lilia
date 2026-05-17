package storage

import (
	"context"
	"database/sql"
	"fmt"
	"github-release-notifier/internal/release"
)

type TrackedRepoStore struct {
	db *sql.DB
}

func NewTrackedRepoStore(db *sql.DB) *TrackedRepoStore {
	return &TrackedRepoStore{db: db}
}

func (r *TrackedRepoStore) Upsert(ctx context.Context, owner, name string) error {
	query := `
		INSERT INTO tracked_repositories (owner, name)
		VALUES ($1, $2)
		ON CONFLICT (owner, name) DO NOTHING`

	if _, err := r.db.ExecContext(ctx, query, owner, name); err != nil {
		return fmt.Errorf("inserting tracked repository: %w", err)
	}
	return nil
}

func (r *TrackedRepoStore) GetAll(ctx context.Context) ([]release.TrackedRepository, error) {
	query := `
		SELECT id, owner, name, last_seen_tag, last_checked_at, created_at
		FROM tracked_repositories
		ORDER BY id`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying tracked repositories: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows close error is safe to ignore

	repos := make([]release.TrackedRepository, 0)
	for rows.Next() {
		var repo release.TrackedRepository
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
