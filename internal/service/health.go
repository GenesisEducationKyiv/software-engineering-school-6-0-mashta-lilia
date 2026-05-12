package service

import (
	"context"
	"database/sql"
	"errors"
)

// errHealthCheckerMisconfigured is returned by DBHealthChecker.Check when no
// DB handle has been wired. It's surfaced so the /health endpoint reports
// "unhealthy" instead of crashing the server.
var errHealthCheckerMisconfigured = errors.New("health checker not configured")

// DBHealthChecker pings a database connection. It satisfies any
// HealthChecker-shaped interface (e.g., rest.HealthChecker) via Go's
// structural typing — service/ does not need to import rest/.
type DBHealthChecker struct {
	db *sql.DB
}

func NewDBHealthChecker(db *sql.DB) *DBHealthChecker {
	return &DBHealthChecker{db: db}
}

func (h *DBHealthChecker) Check(ctx context.Context) error {
	if h.db == nil {
		return errHealthCheckerMisconfigured
	}
	return h.db.PingContext(ctx)
}
