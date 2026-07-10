package saga

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned when a saga instance does not exist.
var ErrNotFound = errors.New("saga: instance not found")

// Store persists saga instances directly against the pool; the saga is
// per-subscribe, not a hot path, so prepared statements add no value.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, in Instance) error {
	data, err := json.Marshal(in.Data)
	if err != nil {
		return fmt.Errorf("saga: marshal data: %w", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO saga_instances (id, saga_type, state, data, timeout_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		in.ID, in.Type, in.State, data, in.TimeoutAt,
	); err != nil {
		return fmt.Errorf("saga: create instance %s: %w", in.ID, err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, id string) (*Instance, error) {
	var (
		in      Instance
		rawData []byte
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, saga_type, state, data, timeout_at, created_at, updated_at
		 FROM saga_instances WHERE id = $1`, id,
	).Scan(&in.ID, &in.Type, &in.State, &rawData, &in.TimeoutAt, &in.CreatedAt, &in.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("saga: get instance %s: %w", id, err)
	}
	if err := json.Unmarshal(rawData, &in.Data); err != nil {
		return nil, fmt.Errorf("saga: unmarshal data %s: %w", id, err)
	}
	return &in, nil
}

// CompleteIfAwaiting moves awaiting_confirmation -> completed exactly once;
// false means the saga was already in another state (a duplicate/late reply).
func (s *Store) CompleteIfAwaiting(ctx context.Context, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE saga_instances SET state = $1 WHERE id = $2 AND state = $3`,
		StateCompleted, id, StateAwaitingConfirmation,
	)
	if err != nil {
		return false, fmt.Errorf("saga: complete %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("saga: complete rows %s: %w", id, err)
	}
	return n == 1, nil
}

// ClaimCompensation moves awaiting_confirmation -> compensating exactly once,
// returning the data so the single winner runs the cancel (claimed=false if lost).
func (s *Store) ClaimCompensation(ctx context.Context, id string) (SubscriptionData, bool, error) {
	var rawData []byte
	err := s.db.QueryRowContext(ctx,
		`UPDATE saga_instances SET state = $1 WHERE id = $2 AND state = $3 RETURNING data`,
		StateCompensating, id, StateAwaitingConfirmation,
	).Scan(&rawData)
	if errors.Is(err, sql.ErrNoRows) {
		return SubscriptionData{}, false, nil
	}
	if err != nil {
		return SubscriptionData{}, false, fmt.Errorf("saga: claim compensation %s: %w", id, err)
	}
	var data SubscriptionData
	if err := json.Unmarshal(rawData, &data); err != nil {
		return SubscriptionData{}, false, fmt.Errorf("saga: unmarshal data %s: %w", id, err)
	}
	return data, true, nil
}

// MarkCompensated finalizes a compensated saga: compensating -> failed.
func (s *Store) MarkCompensated(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE saga_instances SET state = $1, compensated_at = NOW() WHERE id = $2 AND state = $3`,
		StateFailed, id, StateCompensating,
	); err != nil {
		return fmt.Errorf("saga: mark compensated %s: %w", id, err)
	}
	return nil
}

// ListTimedOut returns ids of in-flight sagas past their deadline.
func (s *Store) ListTimedOut(ctx context.Context, now time.Time, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM saga_instances WHERE state = $1 AND timeout_at < $2 ORDER BY timeout_at LIMIT $3`,
		StateAwaitingConfirmation, now, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("saga: list timed out: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows close error is safe to ignore

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("saga: scan timed out: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("saga: iterate timed out: %w", err)
	}
	return ids, nil
}
