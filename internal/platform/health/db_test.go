package health_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github-release-notifier/internal/platform/health"

	_ "github.com/lib/pq" // register postgres driver for sql.Open
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBChecker_NilReceiver_ReturnsError(t *testing.T) {
	var c *health.DBChecker
	err := c.Check(context.Background())
	require.Error(t, err)
}

func TestDBChecker_NilDB_ReturnsError(t *testing.T) {
	c := health.NewDBChecker(nil)
	err := c.Check(context.Background())
	require.Error(t, err)
}

func TestDBChecker_UnreachableDB_PropagatesContextCancellation(t *testing.T) {
	// Open a sql.DB pointing at an address nothing listens on; the lazy
	// connect lets sql.Open succeed but Ping must fail. We then pre-cancel
	// the context so the failure mode is deterministic (context.Canceled)
	// rather than a wall-clock dial timeout.
	db, err := sql.Open("postgres", "postgres://nobody:nope@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	require.NoError(t, err)
	defer db.Close()

	c := health.NewDBChecker(db)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = c.Check(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled),
		"DBChecker.Check must surface the cancellation cause; got %v", err)
}
