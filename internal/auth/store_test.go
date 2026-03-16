package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/testutil"
	"github.com/katata/seam/migrations"
)

func TestStore_CreateUser_Success(t *testing.T) {
	db := testutil.TestDB(t)
	store := auth.NewSQLStore(db)
	ctx := context.Background()

	u := &auth.User{
		ID:        "01HTEST000000000000000001",
		Username:  "alice",
		Email:     "alice@example.com",
		Password:  "$2a$12$hashedpassword",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}

	err := store.CreateUser(ctx, u)
	require.NoError(t, err)

	got, err := store.GetUserByUsername(ctx, "alice")
	require.NoError(t, err)
	require.Equal(t, u.ID, got.ID)
	require.Equal(t, u.Username, got.Username)
	require.Equal(t, u.Email, got.Email)
	require.Equal(t, u.Password, got.Password)
}

func TestStore_CreateUser_DuplicateUsername(t *testing.T) {
	db := testutil.TestDB(t)
	store := auth.NewSQLStore(db)
	ctx := context.Background()

	now := time.Now().UTC()
	u1 := &auth.User{ID: "01HTEST000000000000000001", Username: "alice", Email: "alice@example.com", Password: "hash", CreatedAt: now, UpdatedAt: now}
	u2 := &auth.User{ID: "01HTEST000000000000000002", Username: "alice", Email: "alice2@example.com", Password: "hash", CreatedAt: now, UpdatedAt: now}

	err := store.CreateUser(ctx, u1)
	require.NoError(t, err)

	err = store.CreateUser(ctx, u2)
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrUserExists)
}

func TestStore_CreateUser_DuplicateEmail(t *testing.T) {
	db := testutil.TestDB(t)
	store := auth.NewSQLStore(db)
	ctx := context.Background()

	now := time.Now().UTC()
	u1 := &auth.User{ID: "01HTEST000000000000000001", Username: "alice", Email: "alice@example.com", Password: "hash", CreatedAt: now, UpdatedAt: now}
	u2 := &auth.User{ID: "01HTEST000000000000000002", Username: "bob", Email: "alice@example.com", Password: "hash", CreatedAt: now, UpdatedAt: now}

	err := store.CreateUser(ctx, u1)
	require.NoError(t, err)

	err = store.CreateUser(ctx, u2)
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrUserExists)
}

func TestStore_GetUserByUsername_NotFound(t *testing.T) {
	db := testutil.TestDB(t)
	store := auth.NewSQLStore(db)
	ctx := context.Background()

	_, err := store.GetUserByUsername(ctx, "nonexistent")
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrNotFound)
}

func TestStore_GetUserByID_Success(t *testing.T) {
	db := testutil.TestDB(t)
	store := auth.NewSQLStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	u := &auth.User{ID: "01HTEST000000000000000001", Username: "alice", Email: "alice@example.com", Password: "hash", CreatedAt: now, UpdatedAt: now}
	err := store.CreateUser(ctx, u)
	require.NoError(t, err)

	got, err := store.GetUserByID(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, u.Username, got.Username)
}

func TestStore_GetUserByID_NotFound(t *testing.T) {
	db := testutil.TestDB(t)
	store := auth.NewSQLStore(db)
	ctx := context.Background()

	_, err := store.GetUserByID(ctx, "nonexistent")
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrNotFound)
}

func TestStore_RefreshToken_Lifecycle(t *testing.T) {
	db := testutil.TestDB(t)
	store := auth.NewSQLStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	u := &auth.User{ID: "01HTEST000000000000000001", Username: "alice", Email: "alice@example.com", Password: "hash", CreatedAt: now, UpdatedAt: now}
	err := store.CreateUser(ctx, u)
	require.NoError(t, err)

	// Create a refresh token.
	tokenHash := "sha256-hash-of-token"
	expiresAt := now.Add(7 * 24 * time.Hour)
	err = store.CreateRefreshToken(ctx, u.ID, tokenHash, expiresAt)
	require.NoError(t, err)

	// Retrieve it.
	userID, gotExpiry, err := store.GetRefreshToken(ctx, tokenHash)
	require.NoError(t, err)
	require.Equal(t, u.ID, userID)
	require.Equal(t, expiresAt.Truncate(time.Second), gotExpiry.Truncate(time.Second))

	// Delete it.
	err = store.DeleteRefreshToken(ctx, tokenHash)
	require.NoError(t, err)

	// Should be gone.
	_, _, err = store.GetRefreshToken(ctx, tokenHash)
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrNotFound)
}

func TestStore_DeleteRefreshTokensByUser(t *testing.T) {
	db := testutil.TestDB(t)
	store := auth.NewSQLStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	u := &auth.User{ID: "01HTEST000000000000000001", Username: "alice", Email: "alice@example.com", Password: "hash", CreatedAt: now, UpdatedAt: now}
	err := store.CreateUser(ctx, u)
	require.NoError(t, err)

	// Create multiple refresh tokens.
	expires := now.Add(7 * 24 * time.Hour)
	err = store.CreateRefreshToken(ctx, u.ID, "hash1", expires)
	require.NoError(t, err)
	err = store.CreateRefreshToken(ctx, u.ID, "hash2", expires)
	require.NoError(t, err)

	// Delete all tokens for user.
	err = store.DeleteRefreshTokensByUser(ctx, u.ID)
	require.NoError(t, err)

	// Both should be gone.
	_, _, err = store.GetRefreshToken(ctx, "hash1")
	require.ErrorIs(t, err, auth.ErrNotFound)
	_, _, err = store.GetRefreshToken(ctx, "hash2")
	require.ErrorIs(t, err, auth.ErrNotFound)
}

func TestStore_RefreshToken_CascadeOnUserDelete(t *testing.T) {
	db := testutil.TestDB(t)
	store := auth.NewSQLStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	u := &auth.User{ID: "01HTEST000000000000000001", Username: "alice", Email: "alice@example.com", Password: "hash", CreatedAt: now, UpdatedAt: now}
	err := store.CreateUser(ctx, u)
	require.NoError(t, err)

	err = store.CreateRefreshToken(ctx, u.ID, "hash1", now.Add(time.Hour))
	require.NoError(t, err)

	// Delete user directly (simulating account deletion).
	_, err = db.ExecContext(ctx, "DELETE FROM owner WHERE id = ?", u.ID)
	require.NoError(t, err)

	// Token should be cascade-deleted.
	_, _, err = store.GetRefreshToken(ctx, "hash1")
	require.ErrorIs(t, err, auth.ErrNotFound)
}

func TestStore_MigrationsIdempotent(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := context.Background()

	// Run migrations again (TestDB already ran them once).
	// This should not error because we use IF NOT EXISTS.
	_, err := db.ExecContext(ctx, migrations.InitialSQL)
	require.NoError(t, err)
}
