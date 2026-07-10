package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github-release-notifier/internal/platform/logger"
	"sync"
)

// A conflicting row is re-reserved (and the caller gets another send attempt)
// while it's still unconfirmed (sent_at IS NULL); once confirmed, the WHERE
// excludes it from the update so RETURNING yields no row, i.e. a true dedup.
const reserveQuery = `
	INSERT INTO sent_notifications (kind, dedup_key)
	VALUES ($1, $2)
	ON CONFLICT (dedup_key) DO UPDATE
		SET kind = EXCLUDED.kind
		WHERE sent_notifications.sent_at IS NULL
	RETURNING id`

const confirmQuery = `
	UPDATE sent_notifications SET sent_at = NOW() WHERE dedup_key = $1`

type Store struct {
	db          *sql.DB
	prepareMu   sync.Mutex
	prepared    bool
	stmtReserve *sql.Stmt
	stmtConfirm *sql.Stmt
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

func (s *Store) Reserve(ctx context.Context, kind, dedupKey string) (bool, error) {
	if err := s.ensurePrepared(ctx); err != nil {
		return false, err
	}

	var id int64
	err := s.stmtReserve.QueryRowContext(ctx, kind, dedupKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reserving notification dedup_key=%q: %w", dedupKey, err)
	}
	return true, nil
}

// Confirm marks a reserved dedup key as sent, so a later Reserve for the same
// key is treated as a true duplicate instead of a retryable failure.
func (s *Store) Confirm(ctx context.Context, dedupKey string) error {
	if err := s.ensurePrepared(ctx); err != nil {
		return err
	}
	if _, err := s.stmtConfirm.ExecContext(ctx, dedupKey); err != nil {
		return fmt.Errorf("confirming notification dedup_key=%q: %w", dedupKey, err)
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	return errors.Join(
		closeStmt("notification reserve", s.stmtReserve),
		closeStmt("notification confirm", s.stmtConfirm),
	)
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
	if s.stmtConfirm, err = s.db.PrepareContext(ctx, confirmQuery); err != nil {
		s.log.Error(ctx, "notification_store_prepare_failed", "statement", "confirm", "err", err)
		return fmt.Errorf("preparing notification confirm: %w", err)
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
