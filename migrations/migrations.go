// Package migrations embeds SQL migration files and exposes them
// for use by other packages (auth, userdb, testutil).
package migrations

import _ "embed"

//go:embed server/001_users.sql
var ServerSQL string

//go:embed user/001_initial.sql
var UserSQL string
