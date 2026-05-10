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

// HealthChecker reports the liveness of a downstream dependency. The HTTP
// handler delegates to this so it never depends on *sql.DB or any other
// concrete infrastructure type — keeping the controller thin and letting
// the set of checked dependencies grow (Redis, GitHub, …) without touching
// the router.
type HealthChecker interface {
	Check(ctx context.Context) error
}

// DBHealthChecker pings a database connection.
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
