package api_test

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"testing"

	"github-release-notifier/tests/pkg/testapp"
)

var (
	sharedApp *testapp.App
	setupErr  error
)

func TestMain(m *testing.M) {
	// All container lifecycle is funneled through runTests so deferred
	// cleanup runs even on mid-setup failures; calling os.Exit from
	// TestMain directly would skip those defers and leak containers.
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}

	app, cleanup, err := testapp.New(context.Background())
	defer cleanup()
	if err != nil {
		// Record the cause so envForTest can report it to each skipped/
		// failed test rather than logging it once at startup and losing it.
		setupErr = err
		slog.Error("integration setup failed", "err", err)
		return 1
	}
	sharedApp = app

	return m.Run()
}

// envForTest returns the shared app, skipping when -short is set and
// failing loudly if suite setup didn't succeed.
func envForTest(t *testing.T) *testapp.App {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if sharedApp == nil {
		t.Fatalf("integration app not initialized; setup error: %v", setupErr)
	}
	return sharedApp
}
