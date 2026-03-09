// Package migrations embeds SQL migration files and exposes them
// for use by other packages (auth, userdb, testutil).
package migrations

import (
	"database/sql"
	_ "embed"
)

//go:embed server/001_users.sql
var ServerSQL string

//go:embed user/001_initial.sql
var userInitialSQL string

//go:embed user/002_add_slug.sql
var userAddSlugSQL string

//go:embed user/003_settings.sql
var userSettingsSQL string

//go:embed user/004_chat_history.sql
var userChatHistorySQL string

// UserSQL contains all user-database migrations concatenated in order.
// Used by legacy callers and tests that run the full schema in one shot.
var UserSQL = userInitialSQL + "\n" + userAddSlugSQL + "\n" + userSettingsSQL + "\n" + userChatHistorySQL

// Migration represents a single numbered migration step.
type Migration struct {
	Version int
	SQL     string
	// PreHook runs before the migration SQL, outside the transaction.
	// Used for operations like ALTER TABLE that cannot run inside a
	// transaction in some SQLite drivers.
	PreHook func(db *sql.DB) error
}

// UserMigrations returns the ordered list of user-database migrations.
// Each migration is tagged with a version number so the runner can skip
// already-applied migrations and handle future ALTER TABLE statements.
func UserMigrations() []Migration {
	return []Migration{
		{Version: 1, SQL: userInitialSQL},
		{
			Version: 2,
			SQL:     userAddSlugSQL,
			PreHook: func(db *sql.DB) error {
				// Add the slug column if it does not already exist.
				// 001_initial.sql includes slug for fresh databases, but
				// databases created before slug was added need ALTER TABLE.
				if !HasColumn(db, "notes", "slug") {
					if _, err := db.Exec("ALTER TABLE notes ADD COLUMN slug TEXT NOT NULL DEFAULT ''"); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{Version: 3, SQL: userSettingsSQL},
		{Version: 4, SQL: userChatHistorySQL},
	}
}

// ServerMigrations returns the ordered list of server-database migrations.
func ServerMigrations() []Migration {
	return []Migration{
		{Version: 1, SQL: ServerSQL},
	}
}
