package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
)

const (
	upsertQuery = `
		INSERT INTO tracked_repositories (owner, name)
		VALUES ($1, $2)
		ON CONFLICT (owner, name) DO NOTHING`

	getAllQuery = `
		SELECT id, owner, name, last_seen_tag, last_checked_at, created_at
		FROM tracked_repositories
		ORDER BY id`

	updateLastSeenQuery = `
		UPDATE tracked_repositories SET last_seen_tag = $1, last_checked_at = NOW() WHERE id = $2`

	updateLastCheckedQuery = `
		UPDATE tracked_repositories SET last_checked_at = NOW() WHERE id = $1`
)

type Store struct {
	db                    *sql.DB
	prepareMu             sync.Mutex
	prepared              bool
	stmtUpsert            *sql.Stmt
	stmtGetAll            *sql.Stmt
	stmtUpdateLastSeen    *sql.Stmt
	stmtUpdateLastChecked *sql.Stmt
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func NewStoreWithContext(ctx context.Context, db *sql.DB) (*Store, error) {
	if ctx == nil {
		return nil, errors.New("repository store: nil context")
	}
	if db == nil {
		return nil, errors.New("repository store: nil db")
	}

	store := &Store{db: db}
	if err := store.ensurePrepared(ctx); err != nil {
		return nil, errors.Join(err, store.Close())
	}
	return store, nil
}

func (s *Store) ensurePrepared(ctx context.Context) error {
	if s == nil {
		return errors.New("repository store: nil receiver")
	}
	if ctx == nil {
		return errors.New("repository store: nil context")
	}
	if s.db == nil {
		return errors.New("repository store: nil db")
	}

	s.prepareMu.Lock()
	defer s.prepareMu.Unlock()
	if s.prepared {
		return nil
	}
	if err := s.prepare(ctx); err != nil {
		return err
	}
	s.prepared = true
	return nil
}

func (s *Store) prepare(ctx context.Context) error {
	var err error
	if s.stmtUpsert, err = s.db.PrepareContext(ctx, upsertQuery); err != nil {
		return fmt.Errorf("preparing tracked repo upsert: %w", err)
	}
	if s.stmtGetAll, err = s.db.PrepareContext(ctx, getAllQuery); err != nil {
		return fmt.Errorf("preparing tracked repo get all: %w", err)
	}
	if s.stmtUpdateLastSeen, err = s.db.PrepareContext(ctx, updateLastSeenQuery); err != nil {
		return fmt.Errorf("preparing tracked repo update last seen: %w", err)
	}
	s.stmtUpdateLastChecked, err = s.db.PrepareContext(ctx, updateLastCheckedQuery)
	if err != nil {
		return fmt.Errorf("preparing tracked repo update last checked: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	return errors.Join(
		closeStmt("tracked repo upsert", s.stmtUpsert),
		closeStmt("tracked repo get all", s.stmtGetAll),
		closeStmt("tracked repo update last seen", s.stmtUpdateLastSeen),
		closeStmt("tracked repo update last checked", s.stmtUpdateLastChecked),
	)
}

func (s *Store) Upsert(ctx context.Context, owner, name string) error {
	if err := s.ensurePrepared(ctx); err != nil {
		return err
	}
	if _, err := s.stmtUpsert.ExecContext(ctx, owner, name); err != nil {
		return fmt.Errorf("inserting tracked repository: %w", err)
	}
	return nil
}

func (s *Store) GetAll(ctx context.Context) ([]Repository, error) {
	if err := s.ensurePrepared(ctx); err != nil {
		return nil, err
	}
	rows, err := s.stmtGetAll.QueryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("querying tracked repositories: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows close error is safe to ignore

	repos := make([]Repository, 0)
	for rows.Next() {
		var repo Repository
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

func (s *Store) UpdateLastSeen(ctx context.Context, id int64, tag string) error {
	if err := s.ensurePrepared(ctx); err != nil {
		return err
	}
	result, err := s.stmtUpdateLastSeen.ExecContext(ctx, tag, id)
	if err != nil {
		return fmt.Errorf("updating last seen tag: %w", err)
	}
	return requireRowsUpdated(result, "updating last seen tag", id)
}

func (s *Store) UpdateLastChecked(ctx context.Context, id int64) error {
	if err := s.ensurePrepared(ctx); err != nil {
		return err
	}
	result, err := s.stmtUpdateLastChecked.ExecContext(ctx, id)
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
