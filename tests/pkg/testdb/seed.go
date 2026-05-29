package testdb

import (
	"context"
	"database/sql"
	"github-release-notifier/internal/subscription"
	"testing"

	"github.com/stretchr/testify/require"
)

// TruncateAll wipes the tables tests assert against so each run starts
// from a clean slate. Cascades remove subscription rows when their tracked
// repo is purged.
func TruncateAll(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"TRUNCATE subscriptions, tracked_repositories CASCADE")
	require.NoError(t, err)
}

// SeedSubscription inserts a tracked repo + subscription row directly via
// raw SQL, bypassing the service layer. Used by Confirm/Unsubscribe/List
// tests that need a known token without going through Subscribe.
//
// Raw SQL (not the repo abstraction) so the row lands in its final state
// in a single write — the equivalent helper in e2e/fixtures/seed.ts does
// the same, and we avoid two-step Create+UpdateStatus side-effects (extra
// trigger fires, updated_at advanced past created_at).
func SeedSubscription(
	t *testing.T, db *sql.DB,
	email, owner, name, token string, status subscription.Status,
) int64 {
	t.Helper()
	ctx := context.Background()

	_, err := db.ExecContext(
		ctx,
		`INSERT INTO tracked_repositories (owner, name)
		 VALUES ($1, $2)
		 ON CONFLICT (owner, name) DO NOTHING`,
		owner, name,
	)
	require.NoError(t, err, "upsert tracked repo")

	var id int64
	err = db.QueryRowContext(
		ctx,
		`INSERT INTO subscriptions (email, repo_owner, repo_name, token, status)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`,
		email, owner, name, token, status,
	).Scan(&id)
	require.NoError(t, err, "create subscription")
	return id
}

// StatusOf reads the current status of a subscription by token directly
// from the DB (bypasses the service layer for assertion-side verification).
func StatusOf(t *testing.T, db *sql.DB, token string) subscription.Status {
	t.Helper()
	var s string
	err := db.QueryRowContext(
		context.Background(),
		"SELECT status FROM subscriptions WHERE token = $1", token,
	).Scan(&s)
	require.NoError(t, err, "read status")
	return subscription.Status(s)
}
