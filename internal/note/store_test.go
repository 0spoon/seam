package note

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/testutil"
)

func TestSQLStore_Create_Get(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	n := &Note{
		ID:          "01AAAAAAAAAAAAAAAAAAAAAA01",
		Title:       "Test Note",
		FilePath:    "test-note.md",
		Body:        "Hello world",
		ContentHash: "abc123",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err := store.Create(ctx, db, n)
	require.NoError(t, err)

	got, err := store.Get(ctx, db, n.ID)
	require.NoError(t, err)
	require.Equal(t, n.ID, got.ID)
	require.Equal(t, n.Title, got.Title)
	require.Equal(t, n.FilePath, got.FilePath)
	require.Equal(t, n.Body, got.Body)
	require.Equal(t, n.ContentHash, got.ContentHash)
	require.Equal(t, "", got.ProjectID)
}

func TestSQLStore_Create_WithProject(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	// Create a project first.
	_, err := db.ExecContext(ctx,
		`INSERT INTO projects (id, name, slug, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"PROJ01", "My Project", "my-project", "", now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	require.NoError(t, err)

	n := &Note{
		ID:          "01AAAAAAAAAAAAAAAAAAAAAA02",
		Title:       "Project Note",
		ProjectID:   "PROJ01",
		FilePath:    "my-project/project-note.md",
		Body:        "In a project",
		ContentHash: "def456",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err = store.Create(ctx, db, n)
	require.NoError(t, err)

	got, err := store.Get(ctx, db, n.ID)
	require.NoError(t, err)
	require.Equal(t, "PROJ01", got.ProjectID)
}

func TestSQLStore_GetByFilePath(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	n := &Note{
		ID:          "01AAAAAAAAAAAAAAAAAAAAAA03",
		Title:       "File Path Note",
		FilePath:    "path/to/note.md",
		Body:        "body",
		ContentHash: "hash1",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err := store.Create(ctx, db, n)
	require.NoError(t, err)

	got, err := store.GetByFilePath(ctx, db, "path/to/note.md")
	require.NoError(t, err)
	require.Equal(t, n.ID, got.ID)
}

func TestSQLStore_GetByFilePath_NotFound(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	_, err := store.GetByFilePath(ctx, db, "nonexistent.md")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestSQLStore_Get_NotFound(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	_, err := store.Get(ctx, db, "NONEXISTENT")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestSQLStore_Update(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	n := &Note{
		ID:          "01AAAAAAAAAAAAAAAAAAAAAA04",
		Title:       "Original Title",
		FilePath:    "original.md",
		Body:        "original body",
		ContentHash: "hash2",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err := store.Create(ctx, db, n)
	require.NoError(t, err)

	n.Title = "Updated Title"
	n.Body = "updated body"
	n.ContentHash = "hash2-updated"
	n.UpdatedAt = now.Add(time.Minute)

	err = store.Update(ctx, db, n)
	require.NoError(t, err)

	got, err := store.Get(ctx, db, n.ID)
	require.NoError(t, err)
	require.Equal(t, "Updated Title", got.Title)
	require.Equal(t, "updated body", got.Body)
}

func TestSQLStore_Update_NotFound(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	n := &Note{
		ID:          "NONEXISTENT",
		Title:       "x",
		FilePath:    "x.md",
		ContentHash: "x",
		UpdatedAt:   time.Now().UTC(),
	}
	err := store.Update(ctx, db, n)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestSQLStore_Delete(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	n := &Note{
		ID:          "01AAAAAAAAAAAAAAAAAAAAAA05",
		Title:       "Delete Me",
		FilePath:    "delete-me.md",
		Body:        "body",
		ContentHash: "hash3",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err := store.Create(ctx, db, n)
	require.NoError(t, err)

	err = store.Delete(ctx, db, n.ID)
	require.NoError(t, err)

	_, err = store.Get(ctx, db, n.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestSQLStore_Delete_NotFound(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	err := store.Delete(ctx, db, "NONEXISTENT")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestSQLStore_List_Basic(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	// Create 3 notes.
	for i, title := range []string{"Alpha", "Beta", "Gamma"} {
		n := &Note{
			ID:          fmt.Sprintf("NOTE%02d", i+1),
			Title:       title,
			FilePath:    fmt.Sprintf("%s.md", title),
			Body:        "body",
			ContentHash: fmt.Sprintf("hash%d", i),
			CreatedAt:   now.Add(time.Duration(i) * time.Minute),
			UpdatedAt:   now.Add(time.Duration(i) * time.Minute),
		}
		require.NoError(t, store.Create(ctx, db, n))
	}

	notes, total, err := store.List(ctx, db, NoteFilter{})
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, notes, 3)
	// Default sort is by updated_at DESC.
	require.Equal(t, "Gamma", notes[0].Title)
}

func TestSQLStore_List_WithPagination(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 5; i++ {
		n := &Note{
			ID:          fmt.Sprintf("PAGE%02d", i),
			Title:       fmt.Sprintf("Note %d", i),
			FilePath:    fmt.Sprintf("note-%d.md", i),
			Body:        "body",
			ContentHash: fmt.Sprintf("hash%d", i),
			CreatedAt:   now.Add(time.Duration(i) * time.Minute),
			UpdatedAt:   now.Add(time.Duration(i) * time.Minute),
		}
		require.NoError(t, store.Create(ctx, db, n))
	}

	notes, total, err := store.List(ctx, db, NoteFilter{Limit: 2, Offset: 0})
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, notes, 2)

	notes2, _, err := store.List(ctx, db, NoteFilter{Limit: 2, Offset: 2})
	require.NoError(t, err)
	require.Len(t, notes2, 2)
	// Should be different notes.
	require.NotEqual(t, notes[0].ID, notes2[0].ID)
}

func TestSQLStore_List_InboxOnly(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	// Create project.
	_, err := db.ExecContext(ctx,
		`INSERT INTO projects (id, name, slug, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"PROJ02", "Test Project", "test-project", "", now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	require.NoError(t, err)

	// Inbox note.
	n1 := &Note{
		ID: "INBOX01", Title: "Inbox Note", FilePath: "inbox.md",
		Body: "body", ContentHash: "h1", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(ctx, db, n1))

	// Project note.
	n2 := &Note{
		ID: "PROJ_NOTE01", Title: "Project Note", ProjectID: "PROJ02",
		FilePath: "test-project/note.md", Body: "body", ContentHash: "h2",
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(ctx, db, n2))

	notes, total, err := store.List(ctx, db, NoteFilter{InboxOnly: true})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, notes, 1)
	require.Equal(t, "INBOX01", notes[0].ID)
}

func TestSQLStore_List_ByTag(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	n1 := &Note{
		ID: "TAG01", Title: "Tagged", FilePath: "tagged.md",
		Body: "body", ContentHash: "h1", CreatedAt: now, UpdatedAt: now,
	}
	n2 := &Note{
		ID: "TAG02", Title: "Not Tagged", FilePath: "not-tagged.md",
		Body: "body", ContentHash: "h2", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(ctx, db, n1))
	require.NoError(t, store.Create(ctx, db, n2))

	require.NoError(t, store.UpdateTags(ctx, db, "TAG01", []string{"important"}))

	notes, total, err := store.List(ctx, db, NoteFilter{Tag: "important"})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, notes, 1)
	require.Equal(t, "TAG01", notes[0].ID)
}

func TestSQLStore_UpdateTags_ReplacesExisting(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	n := &Note{
		ID: "TAGS01", Title: "Tags Note", FilePath: "tags.md",
		Body: "body", ContentHash: "h", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(ctx, db, n))

	// Set initial tags.
	require.NoError(t, store.UpdateTags(ctx, db, n.ID, []string{"alpha", "beta"}))

	got, err := store.Get(ctx, db, n.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"alpha", "beta"}, got.Tags)

	// Replace tags.
	require.NoError(t, store.UpdateTags(ctx, db, n.ID, []string{"gamma"}))

	got, err = store.Get(ctx, db, n.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"gamma"}, got.Tags)
}

func TestSQLStore_UpdateTags_ClearAll(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	n := &Note{
		ID: "TAGS02", Title: "Tags Note 2", FilePath: "tags2.md",
		Body: "body", ContentHash: "h", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(ctx, db, n))
	require.NoError(t, store.UpdateTags(ctx, db, n.ID, []string{"alpha"}))

	// Clear all tags.
	require.NoError(t, store.UpdateTags(ctx, db, n.ID, nil))

	got, err := store.Get(ctx, db, n.ID)
	require.NoError(t, err)
	require.Empty(t, got.Tags)
}

func TestSQLStore_ListTags(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 3; i++ {
		n := &Note{
			ID: fmt.Sprintf("LT%02d", i), Title: fmt.Sprintf("Note %d", i),
			FilePath: fmt.Sprintf("lt-%d.md", i), Body: "body",
			ContentHash: fmt.Sprintf("h%d", i), CreatedAt: now, UpdatedAt: now,
		}
		require.NoError(t, store.Create(ctx, db, n))
	}

	// Give "go" to all 3, "rust" to 1.
	for i := 0; i < 3; i++ {
		require.NoError(t, store.UpdateTags(ctx, db, fmt.Sprintf("LT%02d", i), []string{"go"}))
	}
	require.NoError(t, store.UpdateTags(ctx, db, "LT00", []string{"go", "rust"}))

	tags, err := store.ListTags(ctx, db)
	require.NoError(t, err)
	require.Len(t, tags, 2)
	// "go" should be first (count 3).
	require.Equal(t, "go", tags[0].Name)
	require.Equal(t, 3, tags[0].Count)
	require.Equal(t, "rust", tags[1].Name)
	require.Equal(t, 1, tags[1].Count)
}

func TestSQLStore_UpdateLinks_And_GetBacklinks(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	// Create two notes: source links to target.
	target := &Note{
		ID: "TARGET01", Title: "Target Note", FilePath: "target-note.md",
		Body: "I am the target", ContentHash: "ht", CreatedAt: now, UpdatedAt: now,
	}
	source := &Note{
		ID: "SOURCE01", Title: "Source Note", FilePath: "source-note.md",
		Body: "See [[Target Note]]", ContentHash: "hs", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(ctx, db, target))
	require.NoError(t, store.Create(ctx, db, source))

	links := []Link{{Target: "Target Note"}}
	require.NoError(t, store.UpdateLinks(ctx, db, source.ID, links))

	backlinks, err := store.GetBacklinks(ctx, db, target.ID)
	require.NoError(t, err)
	require.Len(t, backlinks, 1)
	require.Equal(t, "SOURCE01", backlinks[0].ID)
}

func TestSQLStore_UpdateLinks_DanglingLink(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	source := &Note{
		ID: "SRC02", Title: "Source", FilePath: "source.md",
		Body: "See [[Nonexistent]]", ContentHash: "hs", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(ctx, db, source))

	// Link to a note that does not exist -- should create dangling link.
	links := []Link{{Target: "Nonexistent"}}
	require.NoError(t, store.UpdateLinks(ctx, db, source.ID, links))

	// Now create the target note and resolve dangling links.
	target := &Note{
		ID: "TGT02", Title: "Nonexistent", FilePath: "nonexistent.md",
		Body: "Now I exist", ContentHash: "ht", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(ctx, db, target))
	require.NoError(t, store.ResolveDanglingLinks(ctx, db, target.ID, target.Title, target.FilePath))

	// Verify the link is now resolved.
	backlinks, err := store.GetBacklinks(ctx, db, target.ID)
	require.NoError(t, err)
	require.Len(t, backlinks, 1)
	require.Equal(t, "SRC02", backlinks[0].ID)
}

func TestSQLStore_ResolveLink_ByTitle(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	n := &Note{
		ID: "RL01", Title: "My Title", FilePath: "my-title.md",
		Body: "body", ContentHash: "h", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(ctx, db, n))

	id, err := store.ResolveLink(ctx, db, "My Title")
	require.NoError(t, err)
	require.Equal(t, "RL01", id)
}

func TestSQLStore_ResolveLink_ByFilename(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	n := &Note{
		ID: "RL02", Title: "Fancy Title", FilePath: "project/fancy-title.md",
		Body: "body", ContentHash: "h", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(ctx, db, n))

	id, err := store.ResolveLink(ctx, db, "fancy-title")
	require.NoError(t, err)
	require.Equal(t, "RL02", id)
}

func TestSQLStore_ResolveLink_NotFound(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	_, err := store.ResolveLink(ctx, db, "does not exist")
	require.ErrorIs(t, err, ErrNotFound)
}
