// Package migrations embeds SQL migration files and exposes them
// for use by other packages (auth, userdb, testutil).
//
// This is the first public release: the schema is intentionally a single
// flattened migration. Future schema changes should add new numbered
// files (002_*.sql, 003_*.sql, ...) and append them to Migrations() so
// that existing databases upgrade in place.
package migrations

import (
	"database/sql"
	_ "embed"
)

//go:embed 001_initial.sql
var InitialSQL string

// Migration represents a single numbered migration step.
type Migration struct {
	Version int
	SQL     string
	// PreHook runs before the migration SQL, outside the transaction.
	// Used for operations like ALTER TABLE that cannot run inside a
	// transaction in some SQLite drivers.
	PreHook func(db *sql.DB) error
}

// Migrations returns the ordered list of database migrations.
func Migrations() []Migration {
	return []Migration{
		{Version: 1, SQL: InitialSQL},
	}
}
