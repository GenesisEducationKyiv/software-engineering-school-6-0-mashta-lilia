package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github-release-notifier/internal/release"
	"sync"
)

const (
	trackedRepoUpsertQuery = `
		INSERT INTO tracked_repositories (owner, name)
		VALUES ($1, $2)
		ON CONFLICT (owner, name) DO NOTHING`

	trackedRepoGetAllQuery = `
		SELECT id, owner, name, last_seen_tag, last_checked_at, created_at
		FROM tracked_repositories
		ORDER BY id`

	trackedRepoUpdateLastSeenQuery = `
		UPDATE tracked_repositories SET last_seen_tag = $1, last_checked_at = NOW() WHERE id = $2`

	trackedRepoUpdateLastCheckedQuery = `
		UPDATE tracked_repositories SET last_checked_at = NOW() WHERE id = $1`
)

type TrackedRepoStore struct {
	db                    *sql.DB
	prepareMu             sync.Mutex
	prepared              bool
	stmtUpsert            *sql.Stmt
	stmtGetAll            *sql.Stmt
	stmtUpdateLastSeen    *sql.Stmt
	stmtUpdateLastChecked *sql.Stmt
}

func NewTrackedRepoStore(db *sql.DB) *TrackedRepoStore {
	return &TrackedRepoStore{db: db}
}

func NewTrackedRepoStoreWithContext(ctx context.Context, db *sql.DB) (*TrackedRepoStore, error) {
	if ctx == nil {
		return nil, errors.New("tracked repo store: nil context")
	}
	if db == nil {
		return nil, errors.New("tracked repo store: nil db")
	}

	store := &TrackedRepoStore{db: db}
	if err := store.ensurePrepared(ctx); err != nil {
		return nil, errors.Join(err, store.Close())
	}
	return store, nil
}

func (r *TrackedRepoStore) ensurePrepared(ctx context.Context) error {
	if r == nil {
		return errors.New("tracked repo store: nil receiver")
	}
	if ctx == nil {
		return errors.New("tracked repo store: nil context")
	}
	if r.db == nil {
		return errors.New("tracked repo store: nil db")
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

func (r *TrackedRepoStore) prepare(ctx context.Context) error {
	var err error
	if r.stmtUpsert, err = r.db.PrepareContext(ctx, trackedRepoUpsertQuery); err != nil {
		return fmt.Errorf("preparing tracked repo upsert: %w", err)
	}
	if r.stmtGetAll, err = r.db.PrepareContext(ctx, trackedRepoGetAllQuery); err != nil {
		return fmt.Errorf("preparing tracked repo get all: %w", err)
	}
	if r.stmtUpdateLastSeen, err = r.db.PrepareContext(ctx, trackedRepoUpdateLastSeenQuery); err != nil {
		return fmt.Errorf("preparing tracked repo update last seen: %w", err)
	}
	r.stmtUpdateLastChecked, err = r.db.PrepareContext(ctx, trackedRepoUpdateLastCheckedQuery)
	if err != nil {
		return fmt.Errorf("preparing tracked repo update last checked: %w", err)
	}
	return nil
}

func (r *TrackedRepoStore) Close() error {
	if r == nil {
		return nil
	}
	return errors.Join(
		closeStmt("tracked repo upsert", r.stmtUpsert),
		closeStmt("tracked repo get all", r.stmtGetAll),
		closeStmt("tracked repo update last seen", r.stmtUpdateLastSeen),
		closeStmt("tracked repo update last checked", r.stmtUpdateLastChecked),
	)
}

func (r *TrackedRepoStore) Upsert(ctx context.Context, owner, name string) error {
	if err := r.ensurePrepared(ctx); err != nil {
		return err
	}
	if _, err := r.stmtUpsert.ExecContext(ctx, owner, name); err != nil {
		return fmt.Errorf("inserting tracked repository: %w", err)
	}
	return nil
}

func (r *TrackedRepoStore) GetAll(ctx context.Context) ([]release.TrackedRepository, error) {
	if err := r.ensurePrepared(ctx); err != nil {
		return nil, err
	}
	rows, err := r.stmtGetAll.QueryContext(ctx)
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
	if err := r.ensurePrepared(ctx); err != nil {
		return err
	}
	result, err := r.stmtUpdateLastSeen.ExecContext(ctx, tag, id)
	if err != nil {
		return fmt.Errorf("updating last seen tag: %w", err)
	}
	return requireRowsUpdated(result, "updating last seen tag", id)
}

func (r *TrackedRepoStore) UpdateLastChecked(ctx context.Context, id int64) error {
	if err := r.ensurePrepared(ctx); err != nil {
		return err
	}
	result, err := r.stmtUpdateLastChecked.ExecContext(ctx, id)
	if err != nil {
		return fmt.Errorf("updating last checked timestamp: %w", err)
	}
	return requireRowsUpdated(result, "updating last checked timestamp", id)
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

func requireRowsUpdated(result sql.Result, action string, id int64) error {
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: getting rows affected: %w", action, err)
	}
	if n == 0 {
		return fmt.Errorf("%s: tracked repository %d not found", action, id)
	}
	return nil
}
