package migrations

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// ---------------------------------------------------------------------------
// Run
// ---------------------------------------------------------------------------

func TestRun_EmptyMigrations_NoOp(t *testing.T) {
	db := openTestDB(t)
	err := Run(db, nil)
	require.NoError(t, err)

	// schema_migrations table should still be created.
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestRun_AppliesMigrations_InOrder(t *testing.T) {
	db := openTestDB(t)

	migs := []Migration{
		{Version: 1, SQL: `CREATE TABLE t1 (id INTEGER PRIMARY KEY)`},
		{Version: 2, SQL: `CREATE TABLE t2 (id INTEGER PRIMARY KEY)`},
	}

	err := Run(db, migs)
	require.NoError(t, err)

	// Both tables should exist.
	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='t1'").Scan(&name)
	require.NoError(t, err)
	require.Equal(t, "t1", name)

	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='t2'").Scan(&name)
	require.NoError(t, err)
	require.Equal(t, "t2", name)
}

func TestRun_RecordsVersions_InSchemaMigrations(t *testing.T) {
	db := openTestDB(t)

	migs := []Migration{
		{Version: 1, SQL: `CREATE TABLE t1 (id INTEGER PRIMARY KEY)`},
		{Version: 2, SQL: `CREATE TABLE t2 (id INTEGER PRIMARY KEY)`},
		{Version: 3, SQL: `CREATE TABLE t3 (id INTEGER PRIMARY KEY)`},
	}

	err := Run(db, migs)
	require.NoError(t, err)

	rows, err := db.Query("SELECT version FROM schema_migrations ORDER BY version")
	require.NoError(t, err)
	defer rows.Close()

	var versions []int
	for rows.Next() {
		var v int
		require.NoError(t, rows.Scan(&v))
		versions = append(versions, v)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []int{1, 2, 3}, versions)
}

func TestRun_SkipsAlreadyApplied(t *testing.T) {
	db := openTestDB(t)

	// Apply first migration.
	migs1 := []Migration{
		{Version: 1, SQL: `CREATE TABLE t1 (id INTEGER PRIMARY KEY)`},
	}
	err := Run(db, migs1)
	require.NoError(t, err)

	// Run again with both migrations; only version 2 should be applied.
	migs2 := []Migration{
		{Version: 1, SQL: `CREATE TABLE t1 (id INTEGER PRIMARY KEY)`}, // already applied
		{Version: 2, SQL: `CREATE TABLE t2 (id INTEGER PRIMARY KEY)`},
	}
	err = Run(db, migs2)
	require.NoError(t, err)

	// t2 should exist.
	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='t2'").Scan(&name)
	require.NoError(t, err)
	require.Equal(t, "t2", name)

	// Only 2 versions recorded.
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 2, count)
}

func TestRun_PreHook_ExecutesBeforeMigration(t *testing.T) {
	db := openTestDB(t)

	hookExecuted := false
	migs := []Migration{
		{Version: 1, SQL: `CREATE TABLE t1 (id INTEGER PRIMARY KEY, extra TEXT)`},
		{
			Version: 2,
			SQL:     `INSERT INTO t1 (id, extra) VALUES (1, 'from_migration')`,
			PreHook: func(db *sql.DB) error {
				hookExecuted = true
				return nil
			},
		},
	}

	err := Run(db, migs)
	require.NoError(t, err)
	require.True(t, hookExecuted)

	// Verify the migration SQL ran after the hook.
	var extra string
	err = db.QueryRow("SELECT extra FROM t1 WHERE id = 1").Scan(&extra)
	require.NoError(t, err)
	require.Equal(t, "from_migration", extra)
}

func TestRun_PreHook_ErrorStopsMigration(t *testing.T) {
	db := openTestDB(t)

	migs := []Migration{
		{Version: 1, SQL: `CREATE TABLE t1 (id INTEGER PRIMARY KEY)`},
		{
			Version: 2,
			SQL:     `CREATE TABLE t2 (id INTEGER PRIMARY KEY)`,
			PreHook: func(db *sql.DB) error {
				return fmt.Errorf("hook failed")
			},
		},
	}

	err := Run(db, migs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "pre-hook version 2")
	require.Contains(t, err.Error(), "hook failed")

	// Version 1 should be applied, version 2 should not.
	var maxVersion int
	err = db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&maxVersion)
	require.NoError(t, err)
	require.Equal(t, 1, maxVersion)
}

func TestRun_SQLError_RollsBackTransaction(t *testing.T) {
	db := openTestDB(t)

	migs := []Migration{
		{Version: 1, SQL: `CREATE TABLE t1 (id INTEGER PRIMARY KEY)`},
		{Version: 2, SQL: `INVALID SQL STATEMENT`},
	}

	err := Run(db, migs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "version 2")

	// Version 1 should still be applied.
	var maxVersion int
	err = db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&maxVersion)
	require.NoError(t, err)
	require.Equal(t, 1, maxVersion)

	// Version 2 should NOT be recorded (rolled back).
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 2").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestRun_Idempotent_MultipleCalls(t *testing.T) {
	db := openTestDB(t)

	migs := []Migration{
		{Version: 1, SQL: `CREATE TABLE t1 (id INTEGER PRIMARY KEY)`},
	}

	// Run twice.
	require.NoError(t, Run(db, migs))
	require.NoError(t, Run(db, migs))

	// Still only one version recorded.
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

// TestRun_AppliesAllSeamMigrationsAndExposesToolColumns exercises the real
// embedded migrations against a fresh database and asserts the new tool
// columns on the messages table are present, the relaxed role CHECK allows
// 'tool' and 'system', and unknown roles are still rejected.
//
// It talks to the messages table via raw SQL to avoid importing the chat
// package and creating a layering violation (migrations is a leaf package).
func TestRun_AppliesAllSeamMigrationsAndExposesToolColumns(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, Run(db, Migrations()))

	// Seed a conversation to satisfy the messages.conversation_id FK.
	_, err := db.Exec(
		`INSERT INTO conversations (id, title, created_at, updated_at)
		 VALUES (?, ?, ?, ?)`,
		"conv1", "Test", "2026-04-07T00:00:00Z", "2026-04-07T00:00:00Z",
	)
	require.NoError(t, err)

	// Insert a tool message populating every new column.
	const (
		msgID      = "msg_tool"
		toolCalls  = `[{"id":"a","name":"t","arguments":"{}"}]`
		toolCallID = "a"
		toolName   = "t"
		iteration  = 2
		content    = "tool result"
	)
	_, err = db.Exec(
		`INSERT INTO messages (
		    id, conversation_id, role, content, citations,
		    tool_calls, tool_call_id, tool_name, iteration, created_at
		 )
		 VALUES (?, ?, 'tool', ?, NULL, ?, ?, ?, ?, ?)`,
		msgID, "conv1", content, toolCalls, toolCallID, toolName, iteration,
		"2026-04-07T00:00:01Z",
	)
	require.NoError(t, err)

	// Read it back and verify every column survived.
	var (
		gotRole       string
		gotContent    string
		gotToolCalls  sql.NullString
		gotToolCallID sql.NullString
		gotToolName   sql.NullString
		gotIteration  int
	)
	err = db.QueryRow(
		`SELECT role, content, tool_calls, tool_call_id, tool_name, iteration
		 FROM messages WHERE id = ?`, msgID,
	).Scan(&gotRole, &gotContent, &gotToolCalls, &gotToolCallID, &gotToolName, &gotIteration)
	require.NoError(t, err)
	require.Equal(t, "tool", gotRole)
	require.Equal(t, content, gotContent)
	require.True(t, gotToolCalls.Valid)
	require.Equal(t, toolCalls, gotToolCalls.String)
	require.True(t, gotToolCallID.Valid)
	require.Equal(t, toolCallID, gotToolCallID.String)
	require.True(t, gotToolName.Valid)
	require.Equal(t, toolName, gotToolName.String)
	require.Equal(t, iteration, gotIteration)

	// Relaxed CHECK: 'system' must now be accepted.
	_, err = db.Exec(
		`INSERT INTO messages (id, conversation_id, role, content, created_at)
		 VALUES (?, 'conv1', 'system', 'max iterations reached', ?)`,
		"msg_system", "2026-04-07T00:00:02Z",
	)
	require.NoError(t, err, "role='system' should be accepted by the relaxed CHECK")

	// Unknown roles must still be rejected by the CHECK constraint.
	_, err = db.Exec(
		`INSERT INTO messages (id, conversation_id, role, content, created_at)
		 VALUES (?, 'conv1', 'banana', 'nope', ?)`,
		"msg_bad", "2026-04-07T00:00:03Z",
	)
	require.Error(t, err, "role='banana' should violate the CHECK constraint")
}
