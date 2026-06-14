package migrations_test

import (
	"github-release-notifier/services/notification/migrations"
	"testing"

	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/stretchr/testify/require"
)

// Guards the embed pattern + iofs path used by app.openAndMigrateDB: if the SQL
// files stop being embedded, this fails without needing a database.
func TestEmbeddedMigrationsAreReadable(t *testing.T) {
	t.Parallel()

	src, err := iofs.New(migrations.FS, ".")
	require.NoError(t, err)
	t.Cleanup(func() { _ = src.Close() })

	version, err := src.First()
	require.NoError(t, err)
	require.Equal(t, uint(1), version)
}
