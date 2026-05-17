package health

import (
	"context"
	"database/sql"
	"errors"
)

var errMisconfigured = errors.New("health checker not configured")

type DBChecker struct {
	db *sql.DB
}

func NewDBChecker(db *sql.DB) *DBChecker {
	return &DBChecker{db: db}
}

func (h *DBChecker) Check(ctx context.Context) error {
	if h.db == nil {
		return errMisconfigured
	}
	return h.db.PingContext(ctx)
}
