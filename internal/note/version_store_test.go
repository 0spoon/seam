package note

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/testutil"
)

func TestVersionStore_Create_Get(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := NewVersionStore()
	noteStore := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	// Create a note first (versions reference notes via FK).
	n := &Note{
		ID:          "NOTE_V_01",
		Title:       "Version Test Note",
		FilePath:    "version-test.md",
		Body:        "original body",
		ContentHash: "hash_orig",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, noteStore.Create(ctx, db, n))

	v := &NoteVersion{
		NoteID:      "NOTE_V_01",
		Version:     1,
		Title:       "Version Test Note",
		Body:        "original body",
		ContentHash: "hash_orig",
		CreatedAt:   now,
	}
	require.NoError(t, store.Create(ctx, db, v))
	require.NotEmpty(t, v.ID)

	got, err := store.Get(ctx, db, "NOTE_V_01", 1)
	require.NoError(t, err)
	require.Equal(t, v.ID, got.ID)
	require.Equal(t, "NOTE_V_01", got.NoteID)
	require.Equal(t, 1, got.Version)
	require.Equal(t, "Version Test Note", got.Title)
	require.Equal(t, "original body", got.Body)
	require.Equal(t, "hash_orig", got.ContentHash)
}

func TestVersionStore_Get_NotFound(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := NewVersionStore()
	ctx := context.Background()

	_, err := store.Get(ctx, db, "NONEXISTENT", 1)
	require.ErrorIs(t, err, ErrVersionNotFound)
}

func TestVersionStore_List_NewestFirst(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := NewVersionStore()
	noteStore := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	n := &Note{
		ID:          "NOTE_V_02",
		Title:       "List Test",
		FilePath:    "list-test.md",
		Body:        "body",
		ContentHash: "h",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, noteStore.Create(ctx, db, n))

	for i := 1; i <= 5; i++ {
		v := &NoteVersion{
			NoteID:      "NOTE_V_02",
			Version:     i,
			Title:       fmt.Sprintf("Title v%d", i),
			Body:        fmt.Sprintf("Body v%d", i),
			ContentHash: fmt.Sprintf("hash_v%d", i),
			CreatedAt:   now.Add(time.Duration(i) * time.Minute),
		}
		require.NoError(t, store.Create(ctx, db, v))
	}

	versions, total, err := store.List(ctx, db, "NOTE_V_02", 10, 0)
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, versions, 5)
	// Newest first.
	require.Equal(t, 5, versions[0].Version)
	require.Equal(t, 1, versions[4].Version)
}

func TestVersionStore_List_Pagination(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := NewVersionStore()
	noteStore := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	n := &Note{
		ID:          "NOTE_V_03",
		Title:       "Pagination Test",
		FilePath:    "pagination-test.md",
		Body:        "body",
		ContentHash: "h",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, noteStore.Create(ctx, db, n))

	for i := 1; i <= 5; i++ {
		v := &NoteVersion{
			NoteID:      "NOTE_V_03",
			Version:     i,
			Title:       fmt.Sprintf("Title v%d", i),
			Body:        fmt.Sprintf("Body v%d", i),
			ContentHash: fmt.Sprintf("hash_v%d", i),
			CreatedAt:   now.Add(time.Duration(i) * time.Minute),
		}
		require.NoError(t, store.Create(ctx, db, v))
	}

	// Page 1: limit=2, offset=0 -> versions 5, 4
	versions, total, err := store.List(ctx, db, "NOTE_V_03", 2, 0)
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, versions, 2)
	require.Equal(t, 5, versions[0].Version)
	require.Equal(t, 4, versions[1].Version)

	// Page 2: limit=2, offset=2 -> versions 3, 2
	versions2, _, err := store.List(ctx, db, "NOTE_V_03", 2, 2)
	require.NoError(t, err)
	require.Len(t, versions2, 2)
	require.Equal(t, 3, versions2[0].Version)
	require.Equal(t, 2, versions2[1].Version)
}

func TestVersionStore_NextVersion(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := NewVersionStore()
	noteStore := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	n := &Note{
		ID:          "NOTE_V_04",
		Title:       "NextVersion Test",
		FilePath:    "nextversion-test.md",
		Body:        "body",
		ContentHash: "h",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, noteStore.Create(ctx, db, n))

	// No versions yet -> should return 1.
	next, err := store.NextVersion(ctx, db, "NOTE_V_04")
	require.NoError(t, err)
	require.Equal(t, 1, next)

	// Add a version and check again.
	v := &NoteVersion{
		NoteID:      "NOTE_V_04",
		Version:     1,
		Title:       "v1",
		Body:        "body v1",
		ContentHash: "h1",
		CreatedAt:   now,
	}
	require.NoError(t, store.Create(ctx, db, v))

	next, err = store.NextVersion(ctx, db, "NOTE_V_04")
	require.NoError(t, err)
	require.Equal(t, 2, next)
}

func TestVersionStore_Cleanup(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := NewVersionStore()
	noteStore := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	n := &Note{
		ID:          "NOTE_V_05",
		Title:       "Cleanup Test",
		FilePath:    "cleanup-test.md",
		Body:        "body",
		ContentHash: "h",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, noteStore.Create(ctx, db, n))

	// Create 10 versions.
	for i := 1; i <= 10; i++ {
		v := &NoteVersion{
			NoteID:      "NOTE_V_05",
			Version:     i,
			Title:       fmt.Sprintf("Title v%d", i),
			Body:        fmt.Sprintf("Body v%d", i),
			ContentHash: fmt.Sprintf("hash_v%d", i),
			CreatedAt:   now.Add(time.Duration(i) * time.Minute),
		}
		require.NoError(t, store.Create(ctx, db, v))
	}

	// Cleanup to keep max 3.
	require.NoError(t, store.Cleanup(ctx, db, "NOTE_V_05", 3))

	// Should have 3 versions left (8, 9, 10).
	versions, total, err := store.List(ctx, db, "NOTE_V_05", 100, 0)
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, versions, 3)
	require.Equal(t, 10, versions[0].Version)
	require.Equal(t, 9, versions[1].Version)
	require.Equal(t, 8, versions[2].Version)
}

func TestVersionStore_Cleanup_NoOp(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := NewVersionStore()
	noteStore := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	n := &Note{
		ID:          "NOTE_V_06",
		Title:       "Cleanup NoOp",
		FilePath:    "cleanup-noop.md",
		Body:        "body",
		ContentHash: "h",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, noteStore.Create(ctx, db, n))

	// Create 2 versions.
	for i := 1; i <= 2; i++ {
		v := &NoteVersion{
			NoteID:      "NOTE_V_06",
			Version:     i,
			Title:       fmt.Sprintf("v%d", i),
			Body:        fmt.Sprintf("body v%d", i),
			ContentHash: fmt.Sprintf("h%d", i),
			CreatedAt:   now,
		}
		require.NoError(t, store.Create(ctx, db, v))
	}

	// Cleanup with max 5 -> no-op.
	require.NoError(t, store.Cleanup(ctx, db, "NOTE_V_06", 5))

	_, total, err := store.List(ctx, db, "NOTE_V_06", 100, 0)
	require.NoError(t, err)
	require.Equal(t, 2, total)
}

func TestVersionStore_CascadeDelete(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := NewVersionStore()
	noteStore := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	n := &Note{
		ID:          "NOTE_V_07",
		Title:       "Cascade Delete",
		FilePath:    "cascade-delete.md",
		Body:        "body",
		ContentHash: "h",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, noteStore.Create(ctx, db, n))

	v := &NoteVersion{
		NoteID:      "NOTE_V_07",
		Version:     1,
		Title:       "v1",
		Body:        "body v1",
		ContentHash: "h1",
		CreatedAt:   now,
	}
	require.NoError(t, store.Create(ctx, db, v))

	// Delete the note -- versions should cascade delete.
	require.NoError(t, noteStore.Delete(ctx, db, "NOTE_V_07"))

	_, err := store.Get(ctx, db, "NOTE_V_07", 1)
	require.ErrorIs(t, err, ErrVersionNotFound)
}
