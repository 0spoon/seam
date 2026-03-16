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
var UserSQL string

//go:embed user/002_agent_sessions.sql
var UserSQL002 string

//go:embed user/003_tasks.sql
var UserSQL003 string

//go:embed user/004_webhooks.sql
var UserSQL004 string

//go:embed user/005_task_composite_index.sql
var UserSQL005 string

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
func UserMigrations() []Migration {
	return []Migration{
		{Version: 1, SQL: UserSQL},
		{Version: 2, SQL: UserSQL002},
		{Version: 3, SQL: UserSQL003},
		{Version: 4, SQL: UserSQL004},
		{Version: 5, SQL: UserSQL005},
	}
}

// ServerMigrations returns the ordered list of server-database migrations.
func ServerMigrations() []Migration {
	return []Migration{
		{Version: 1, SQL: ServerSQL},
	}
}
