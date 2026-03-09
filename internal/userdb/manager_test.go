package userdb_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/userdb"
)

func newTestManager(t *testing.T) (*userdb.SQLManager, string) {
	t.Helper()
	dataDir := t.TempDir()
	mgr := userdb.NewSQLManager(dataDir, 30*time.Minute, slog.Default())
	t.Cleanup(func() { mgr.CloseAll() })
	return mgr, dataDir
}

func TestManager_Open_CreatesDB(t *testing.T) {
	mgr, dataDir := newTestManager(t)
	ctx := context.Background()

	db, err := mgr.Open(ctx, "user1")
	require.NoError(t, err)
	require.NotNil(t, db)

	// Verify the DB file was created.
	dbPath := filepath.Join(dataDir, "users", "user1", "seam.db")
	_, err = os.Stat(dbPath)
	require.NoError(t, err)

	// Verify notes directory was created.
	notesDir := filepath.Join(dataDir, "users", "user1", "notes")
	info, err := os.Stat(notesDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestManager_Open_ReturnsCachedHandle(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	db1, err := mgr.Open(ctx, "user1")
	require.NoError(t, err)

	db2, err := mgr.Open(ctx, "user1")
	require.NoError(t, err)

	// Same pointer: the handle is cached.
	require.Same(t, db1, db2)
}

func TestManager_Open_MultipleConcurrentUsers(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	db1, err := mgr.Open(ctx, "user1")
	require.NoError(t, err)

	db2, err := mgr.Open(ctx, "user2")
	require.NoError(t, err)

	// Different users get different handles.
	require.NotSame(t, db1, db2)
}

func TestManager_Close_RemovesFromCache(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	db1, err := mgr.Open(ctx, "user1")
	require.NoError(t, err)
	require.NotNil(t, db1)

	err = mgr.Close("user1")
	require.NoError(t, err)

	// Opening again should return a new handle (not the closed one).
	db2, err := mgr.Open(ctx, "user1")
	require.NoError(t, err)
	require.NotSame(t, db1, db2)
}

func TestManager_CloseAll(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	_, err := mgr.Open(ctx, "user1")
	require.NoError(t, err)
	_, err = mgr.Open(ctx, "user2")
	require.NoError(t, err)

	err = mgr.CloseAll()
	require.NoError(t, err)
}

func TestManager_UserNotesDir(t *testing.T) {
	mgr, dataDir := newTestManager(t)
	got := mgr.UserNotesDir("user1")
	want := filepath.Join(dataDir, "users", "user1", "notes")
	require.Equal(t, want, got)
}

func TestManager_ListUsers(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	// No users yet.
	users, err := mgr.ListUsers(ctx)
	require.NoError(t, err)
	require.Empty(t, users)

	// Create some user directories by opening DBs.
	_, err = mgr.Open(ctx, "user1")
	require.NoError(t, err)
	_, err = mgr.Open(ctx, "user2")
	require.NoError(t, err)

	users, err = mgr.ListUsers(ctx)
	require.NoError(t, err)
	require.Len(t, users, 2)
	require.Contains(t, users, "user1")
	require.Contains(t, users, "user2")
}

func TestManager_EnsureUserDirs(t *testing.T) {
	mgr, dataDir := newTestManager(t)

	err := mgr.EnsureUserDirs("user1")
	require.NoError(t, err)

	notesDir := filepath.Join(dataDir, "users", "user1", "notes")
	info, err := os.Stat(notesDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	// Idempotent: calling again should not error.
	err = mgr.EnsureUserDirs("user1")
	require.NoError(t, err)
}

func TestManager_MigrationsApplied(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	db, err := mgr.Open(ctx, "user1")
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
}
