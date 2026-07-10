package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	_ "github.com/lib/pq"
)

const (
	dbMaxOpenConns    = 25
	dbMaxIdleConns    = 10
	dbConnMaxLifetime = 5 * time.Minute
	dbConnMaxIdleTime = 1 * time.Minute
	dbPingTimeout     = 5 * time.Second
)

func New(databaseURL string) (*sql.DB, error) {
	return NewWithContext(context.Background(), databaseURL)
}

func NewWithContext(ctx context.Context, databaseURL string) (*sql.DB, error) {
	if ctx == nil {
		return nil, errors.New("postgres: nil context")
	}

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, dbPingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		// Join so callers see the close error from the partially-open pool too.
		return nil, errors.Join(err, db.Close())
	}

	db.SetMaxOpenConns(dbMaxOpenConns)
	db.SetMaxIdleConns(dbMaxIdleConns)
	db.SetConnMaxLifetime(dbConnMaxLifetime)
	db.SetConnMaxIdleTime(dbConnMaxIdleTime)
	return db, nil
}
