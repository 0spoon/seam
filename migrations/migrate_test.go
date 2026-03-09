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

// ---------------------------------------------------------------------------
// HasColumn
// ---------------------------------------------------------------------------

func TestHasColumn_ExistingColumn_ReturnsTrue(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec(`CREATE TABLE test_tbl (id INTEGER PRIMARY KEY, name TEXT, age INTEGER)`)
	require.NoError(t, err)

	require.True(t, HasColumn(db, "test_tbl", "id"))
	require.True(t, HasColumn(db, "test_tbl", "name"))
	require.True(t, HasColumn(db, "test_tbl", "age"))
}

func TestHasColumn_NonExistingColumn_ReturnsFalse(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec(`CREATE TABLE test_tbl (id INTEGER PRIMARY KEY)`)
	require.NoError(t, err)

	require.False(t, HasColumn(db, "test_tbl", "nonexistent"))
}

func TestHasColumn_NonExistingTable_ReturnsFalse(t *testing.T) {
	db := openTestDB(t)
	require.False(t, HasColumn(db, "nonexistent_table", "id"))
}

func TestHasColumn_CaseInsensitive(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec(`CREATE TABLE test_tbl (MyColumn TEXT)`)
	require.NoError(t, err)

	require.True(t, HasColumn(db, "test_tbl", "mycolumn"))
	require.True(t, HasColumn(db, "test_tbl", "MYCOLUMN"))
	require.True(t, HasColumn(db, "test_tbl", "MyColumn"))
}
