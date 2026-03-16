package userdb_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/userdb"
)

func newTestManager(t *testing.T) (*userdb.SQLManager, string) {
	t.Helper()
	dataDir := t.TempDir()
	mgr := userdb.NewSQLManager(dataDir, slog.Default())
	t.Cleanup(func() { mgr.CloseAll() })
	return mgr, dataDir
}

func TestManager_Open_CreatesDB(t *testing.T) {
	mgr, dataDir := newTestManager(t)
	ctx := context.Background()

	db, err := mgr.Open(ctx, "ignored")
	require.NoError(t, err)
	require.NotNil(t, db)

	// Verify the DB file was created at the data dir root.
	dbPath := filepath.Join(dataDir, "seam.db")
	_, err = os.Stat(dbPath)
	require.NoError(t, err)

	// Verify notes directory was created.
	notesDir := filepath.Join(dataDir, "notes")
	info, err := os.Stat(notesDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	// Verify inbox directory was created.
	inboxDir := filepath.Join(dataDir, "notes", "inbox")
	info, err = os.Stat(inboxDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestManager_Open_ReturnsSameHandle(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	db1, err := mgr.Open(ctx, "any-user-id")
	require.NoError(t, err)

	// Different userID values return the same handle (single DB).
	db2, err := mgr.Open(ctx, "different-user-id")
	require.NoError(t, err)

	require.Same(t, db1, db2)
}

func TestManager_CloseAll(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	_, err := mgr.Open(ctx, "")
	require.NoError(t, err)

	err = mgr.CloseAll()
	require.NoError(t, err)

	// CloseAll is idempotent.
	err = mgr.CloseAll()
	require.NoError(t, err)
}

func TestManager_UserNotesDir_IgnoresUserID(t *testing.T) {
	mgr, dataDir := newTestManager(t)

	got := mgr.UserNotesDir("anything")
	want := filepath.Join(dataDir, "notes")
	require.Equal(t, want, got)

	// Same result regardless of userID.
	require.Equal(t, want, mgr.UserNotesDir(""))
	require.Equal(t, want, mgr.UserNotesDir("other"))
}

func TestManager_UserDataDir_IgnoresUserID(t *testing.T) {
	mgr, dataDir := newTestManager(t)

	got := mgr.UserDataDir("anything")
	require.Equal(t, dataDir, got)
}

func TestManager_ListUsers_ReturnsSingleEntry(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	users, err := mgr.ListUsers(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{userdb.DefaultUserID}, users)
}

func TestManager_EnsureUserDirs(t *testing.T) {
	mgr, dataDir := newTestManager(t)

	err := mgr.EnsureUserDirs("")
	require.NoError(t, err)

	notesDir := filepath.Join(dataDir, "notes")
	info, err := os.Stat(notesDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	inboxDir := filepath.Join(dataDir, "notes", "inbox")
	info, err = os.Stat(inboxDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	// Idempotent: calling again should not error.
	err = mgr.EnsureUserDirs("")
	require.NoError(t, err)
}

func TestManager_MigrationsApplied(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	db, err := mgr.Open(ctx, "")
	require.NoError(t, err)

	// Verify tables exist by querying them.
	_, err = db.ExecContext(ctx, "SELECT id FROM projects LIMIT 0")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "SELECT id FROM notes LIMIT 0")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "SELECT id FROM tags LIMIT 0")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "SELECT source_note_id FROM links LIMIT 0")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "SELECT id FROM ai_tasks LIMIT 0")
	require.NoError(t, err)
	// Verify auth tables also exist in the single DB.
	_, err = db.ExecContext(ctx, "SELECT id FROM owner LIMIT 0")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "SELECT id FROM api_keys LIMIT 0")
	require.NoError(t, err)
}

func TestManager_NewSQLManagerWithDB(t *testing.T) {
	// Test that NewSQLManagerWithDB uses the provided DB handle.
	base := newTestManager // just for the TempDir
	_, dataDir := base(t)

	// Open a DB manually.
	mgr1 := userdb.NewSQLManager(dataDir, slog.Default())
	ctx := context.Background()
	db, err := mgr1.Open(ctx, "")
	require.NoError(t, err)

	// Create a manager with the pre-opened DB.
	mgr2 := userdb.NewSQLManagerWithDB(db, dataDir, slog.Default())

	db2, err := mgr2.Open(ctx, "")
	require.NoError(t, err)
	require.Same(t, db, db2, "should return the same DB handle")

	// Don't close via mgr2 -- mgr1 owns the DB handle in this test.
}
