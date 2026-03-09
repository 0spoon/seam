// Package testutil provides shared test helpers for database setup,
// temporary directories, and other common test infrastructure.
package testutil

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/katata/seam/migrations"
)

// TestServerDB returns an isolated in-memory SQLite database with server.db
// migrations applied. Each call creates a fresh database named after the test.
func TestServerDB(t *testing.T) *sql.DB {
	t.Helper()
	return OpenTestDB(t, migrations.ServerSQL)
}

// TestUserDB returns an isolated in-memory SQLite database with per-user
// seam.db migrations applied.
func TestUserDB(t *testing.T) *sql.DB {
	t.Helper()
	return OpenTestDB(t, migrations.UserSQL)
}

// TestDataDir returns a temporary directory suitable for use as a data_dir
// in tests. The directory is automatically cleaned up when the test finishes.
func TestDataDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// OpenTestDB creates an isolated in-memory SQLite database with the given
// migration SQL applied.
func OpenTestDB(t *testing.T, migrationSQL string) *sql.DB {
	t.Helper()

	// Use a unique name per test to ensure isolation with parallel tests.
	name := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := sql.Open("sqlite", name)
	require.NoError(t, err)

	// Set WAL mode and enable foreign keys.
	_, err = db.Exec("PRAGMA journal_mode=WAL")
	require.NoError(t, err)
	_, err = db.Exec("PRAGMA foreign_keys=ON")
	require.NoError(t, err)

	// Run migrations.
	_, err = db.Exec(migrationSQL)
	require.NoError(t, err)

	t.Cleanup(func() { db.Close() })
	return db
}
