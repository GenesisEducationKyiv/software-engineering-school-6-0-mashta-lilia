package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github-release-notifier/internal/platform/logger"
	"sync"
)

const reserveQuery = `
	INSERT INTO sent_notifications (kind, recipient, dedup_key)
	VALUES ($1, $2, $3)
	ON CONFLICT (dedup_key) DO NOTHING
	RETURNING id`

type Store struct {
	db          *sql.DB
	prepareMu   sync.Mutex
	prepared    bool
	stmtReserve *sql.Stmt
	log         *logger.Logger
}

func New(db *sql.DB, log *logger.Logger) *Store {
	if log == nil {
		log = logger.Nop()
	}
	return &Store{db: db, log: log}
}

func NewWithContext(ctx context.Context, db *sql.DB, log *logger.Logger) (*Store, error) {
	if ctx == nil {
		return nil, errors.New("notification store: nil context")
	}
	if db == nil {
		return nil, errors.New("notification store: nil db")
	}
	if log == nil {
		log = logger.Nop()
	}

	store := &Store{db: db, log: log}
	if err := store.ensurePrepared(ctx); err != nil {
		return nil, errors.Join(err, store.Close())
	}
	return store, nil
}

func (s *Store) Reserve(ctx context.Context, kind, recipient, dedupKey string) (bool, error) {
	if err := s.ensurePrepared(ctx); err != nil {
		return false, err
	}

	var id int64
	err := s.stmtReserve.QueryRowContext(ctx, kind, recipient, dedupKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reserving notification dedup_key=%q: %w", dedupKey, err)
	}
	return true, nil
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	return closeStmt("notification reserve", s.stmtReserve)
}

func (s *Store) ensurePrepared(ctx context.Context) error {
	if s == nil {
		return errors.New("notification store: nil receiver")
	}
	if ctx == nil {
		return errors.New("notification store: nil context")
	}
	if s.db == nil {
		return errors.New("notification store: nil db")
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
	if s.stmtReserve, err = s.db.PrepareContext(ctx, reserveQuery); err != nil {
		s.log.Error(ctx, "notification_store_prepare_failed", "statement", "reserve", "err", err)
		return fmt.Errorf("preparing notification reserve: %w", err)
	}
	return nil
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
