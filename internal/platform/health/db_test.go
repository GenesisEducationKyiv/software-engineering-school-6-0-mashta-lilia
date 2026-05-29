package health_test

import (
	"context"
	"testing"

	"github-release-notifier/internal/platform/health"

	"github.com/stretchr/testify/require"
)

func TestDBChecker_NilReceiver_ReturnsError(t *testing.T) {
	t.Parallel()
	var c *health.DBChecker
	err := c.Check(context.Background())
	require.Error(t, err)
}

func TestDBChecker_NilDB_ReturnsError(t *testing.T) {
	t.Parallel()
	c := health.NewDBChecker(nil)
	err := c.Check(context.Background())
	require.Error(t, err)
}
