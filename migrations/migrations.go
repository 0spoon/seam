// Package migrations embeds SQL migration files and exposes them
// for use by other packages (auth, userdb, testutil).
package migrations

import _ "embed"

//go:embed server/001_users.sql
var ServerSQL string

//go:embed user/001_initial.sql
var userInitialSQL string

//go:embed user/002_add_slug.sql
var userAddSlugSQL string

// UserSQL contains all user-database migrations concatenated in order.
var UserSQL = userInitialSQL + "\n" + userAddSlugSQL
