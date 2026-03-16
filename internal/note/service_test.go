package note

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/userdb"
)

const testUserID = "test-user-001"

// setupService creates a Service backed by a real SQLManager and temp directory.
func setupService(t *testing.T) (*Service, userdb.Manager, string) {
	t.Helper()

	dataDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	mgr := userdb.NewSQLManager(dataDir, logger)
	t.Cleanup(func() { mgr.CloseAll() })

	noteStore := NewSQLStore()
	versionStore := NewVersionStore()
	projStore := project.NewStore()

	svc := NewService(noteStore, versionStore, projStore, mgr, nil, logger)
	return svc, mgr, dataDir
}

func TestService_Create_WritesFile(t *testing.T) {
	svc, mgr, _ := setupService(t)
	ctx := context.Background()

	n, err := svc.Create(ctx, testUserID, CreateNoteReq{
		Title: "My First Note",
		Body:  "Hello from the service layer.",
		Tags:  []string{"test"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, n.ID)
	require.Equal(t, "My First Note", n.Title)
	require.Equal(t, "my-first-note.md", n.FilePath)
	require.Contains(t, n.Tags, "test")

	// Verify file exists on disk.
	notesDir := mgr.UserNotesDir(testUserID)
	absPath := filepath.Join(notesDir, n.FilePath)
	content, err := os.ReadFile(absPath)
	require.NoError(t, err)
	require.Contains(t, string(content), "My First Note")
	require.Contains(t, string(content), "Hello from the service layer.")
}

func TestService_Create_InProject(t *testing.T) {
	svc, mgr, _ := setupService(t)
	ctx := context.Background()

	// Create a project first.
	projSvc := project.NewService(project.NewStore(), mgr, nil)
	p, err := projSvc.Create(ctx, testUserID, "Research", "")
	require.NoError(t, err)

	n, err := svc.Create(ctx, testUserID, CreateNoteReq{
		Title:     "Study Notes",
		Body:      "Important findings.",
		ProjectID: p.ID,
	})
	require.NoError(t, err)
	require.Equal(t, p.ID, n.ProjectID)
	require.Equal(t, "research/study-notes.md", n.FilePath)

	// Verify file is in the project directory.
	notesDir := mgr.UserNotesDir(testUserID)
	absPath := filepath.Join(notesDir, n.FilePath)
	_, err = os.Stat(absPath)
	require.NoError(t, err)
}

func TestService_Create_DuplicateFilename(t *testing.T) {
	svc, _, _ := setupService(t)
	ctx := context.Background()

	n1, err := svc.Create(ctx, testUserID, CreateNoteReq{Title: "Same Name", Body: "first"})
	require.NoError(t, err)
	require.Equal(t, "same-name.md", n1.FilePath)

	n2, err := svc.Create(ctx, testUserID, CreateNoteReq{Title: "Same Name", Body: "second"})
	require.NoError(t, err)
	require.Equal(t, "same-name-2.md", n2.FilePath)
}

func TestService_Get_ReadsFromDisk(t *testing.T) {
	svc, mgr, _ := setupService(t)
	ctx := context.Background()

	n, err := svc.Create(ctx, testUserID, CreateNoteReq{
		Title: "Disk Read",
		Body:  "Original body",
	})
	require.NoError(t, err)

	// Modify the file on disk directly (simulating external edit).
	notesDir := mgr.UserNotesDir(testUserID)
	absPath := filepath.Join(notesDir, n.FilePath)
	content, err := os.ReadFile(absPath)
	require.NoError(t, err)

	// Replace the body portion while keeping frontmatter intact.
	fm, _, err := ParseFrontmatter(string(content))
	require.NoError(t, err)
	newContent, err := SerializeFrontmatter(fm, "Edited externally")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(absPath, []byte(newContent), 0o644))

	// Get should return the disk version.
	got, err := svc.Get(ctx, testUserID, n.ID)
	require.NoError(t, err)
	require.Contains(t, got.Body, "Edited externally")
}

func TestService_Update_Body(t *testing.T) {
	svc, mgr, _ := setupService(t)
	ctx := context.Background()

	n, err := svc.Create(ctx, testUserID, CreateNoteReq{
		Title: "Update Me",
		Body:  "Original",
	})
	require.NoError(t, err)

	newBody := "Updated body content with #newtag"
	updated, err := svc.Update(ctx, testUserID, n.ID, UpdateNoteReq{
		Body: &newBody,
	})
	require.NoError(t, err)
	require.Equal(t, "Updated body content with #newtag", updated.Body)
	require.Contains(t, updated.Tags, "newtag")

	// Verify file on disk.
	notesDir := mgr.UserNotesDir(testUserID)
	absPath := filepath.Join(notesDir, updated.FilePath)
	content, err := os.ReadFile(absPath)
	require.NoError(t, err)
	require.Contains(t, string(content), "Updated body content")
}

func TestService_Update_MoveProject(t *testing.T) {
	svc, mgr, _ := setupService(t)
	ctx := context.Background()

	// Create a project.
	projSvc := project.NewService(project.NewStore(), mgr, nil)
	p, err := projSvc.Create(ctx, testUserID, "Destination", "")
	require.NoError(t, err)

	// Create an inbox note.
	n, err := svc.Create(ctx, testUserID, CreateNoteReq{
		Title: "Movable",
		Body:  "Moving this note",
	})
	require.NoError(t, err)
	require.Equal(t, "movable.md", n.FilePath)

	// Move to project.
	projectID := p.ID
	updated, err := svc.Update(ctx, testUserID, n.ID, UpdateNoteReq{
		ProjectID: &projectID,
	})
	require.NoError(t, err)
	require.Equal(t, "destination/movable.md", updated.FilePath)
	require.Equal(t, p.ID, updated.ProjectID)

	// Verify old file is gone and new file exists.
	notesDir := mgr.UserNotesDir(testUserID)
	_, err = os.Stat(filepath.Join(notesDir, "movable.md"))
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(notesDir, "destination/movable.md"))
	require.NoError(t, err)
}

func TestService_Delete_RemovesFile(t *testing.T) {
	svc, mgr, _ := setupService(t)
	ctx := context.Background()

	n, err := svc.Create(ctx, testUserID, CreateNoteReq{
		Title: "Delete Me",
		Body:  "Goodbye",
	})
	require.NoError(t, err)

	notesDir := mgr.UserNotesDir(testUserID)
	absPath := filepath.Join(notesDir, n.FilePath)
	_, err = os.Stat(absPath)
	require.NoError(t, err)

	err = svc.Delete(ctx, testUserID, n.ID)
	require.NoError(t, err)

	// Verify file is gone.
	_, err = os.Stat(absPath)
	require.True(t, os.IsNotExist(err))

	// Verify DB record is gone.
	_, err = svc.Get(ctx, testUserID, n.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestService_Delete_NotFound(t *testing.T) {
	svc, _, _ := setupService(t)
	ctx := context.Background()

	// Force the user dir to be created by opening the DB.
	_, err := svc.userDBManager.Open(ctx, testUserID)
	require.NoError(t, err)

	err = svc.Delete(ctx, testUserID, "NONEXISTENT")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestService_List_Empty(t *testing.T) {
	svc, _, _ := setupService(t)
	ctx := context.Background()

	notes, total, err := svc.List(ctx, testUserID, NoteFilter{})
	require.NoError(t, err)
	require.Equal(t, 0, total)
	require.Empty(t, notes)
}

func TestService_List_WithNotes(t *testing.T) {
	svc, _, _ := setupService(t)
	ctx := context.Background()

	_, err := svc.Create(ctx, testUserID, CreateNoteReq{Title: "One", Body: "1"})
	require.NoError(t, err)
	_, err = svc.Create(ctx, testUserID, CreateNoteReq{Title: "Two", Body: "2"})
	require.NoError(t, err)

	notes, total, err := svc.List(ctx, testUserID, NoteFilter{})
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, notes, 2)
}

func TestService_Reindex_NewFile(t *testing.T) {
	svc, mgr, _ := setupService(t)
	ctx := context.Background()

	// Force user dirs to exist.
	require.NoError(t, mgr.EnsureUserDirs(testUserID))

	notesDir := mgr.UserNotesDir(testUserID)

	// Write a .md file directly (simulating external creation).
	fm := &Frontmatter{
		ID:       "REINDEX01",
		Title:    "External Note",
		Created:  time.Now().UTC(),
		Modified: time.Now().UTC(),
	}
	content, err := SerializeFrontmatter(fm, "Created externally")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(notesDir, "external-note.md"), []byte(content), 0o644))

	// Reindex should create DB record.
	err = svc.Reindex(ctx, testUserID, "external-note.md")
	require.NoError(t, err)

	// Should be able to get it from the service.
	got, err := svc.Get(ctx, testUserID, "REINDEX01")
	require.NoError(t, err)
	require.Equal(t, "External Note", got.Title)
	require.Contains(t, got.Body, "Created externally")
}

func TestService_Reindex_DeletedFile(t *testing.T) {
	svc, _, _ := setupService(t)
	ctx := context.Background()

	// Create a note via service.
	n, err := svc.Create(ctx, testUserID, CreateNoteReq{
		Title: "Will Be Deleted",
		Body:  "Soon gone",
	})
	require.NoError(t, err)

	// Delete the file from disk manually (simulating external deletion).
	notesDir := svc.userDBManager.UserNotesDir(testUserID)
	require.NoError(t, os.Remove(filepath.Join(notesDir, n.FilePath)))

	// Reindex should remove DB record.
	err = svc.Reindex(ctx, testUserID, n.FilePath)
	require.NoError(t, err)

	_, err = svc.Get(ctx, testUserID, n.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestService_Reindex_UnchangedFile(t *testing.T) {
	svc, _, _ := setupService(t)
	ctx := context.Background()

	n, err := svc.Create(ctx, testUserID, CreateNoteReq{
		Title: "Stable",
		Body:  "Unchanged content",
	})
	require.NoError(t, err)

	// Reindex without changing the file should be a no-op.
	err = svc.Reindex(ctx, testUserID, n.FilePath)
	require.NoError(t, err)

	got, err := svc.Get(ctx, testUserID, n.ID)
	require.NoError(t, err)
	require.Equal(t, "Stable", got.Title)
}

func TestService_Create_ParsesWikilinks(t *testing.T) {
	svc, mgr, _ := setupService(t)
	ctx := context.Background()

	// Create a target note first.
	target, err := svc.Create(ctx, testUserID, CreateNoteReq{
		Title: "Target",
		Body:  "I am the target",
	})
	require.NoError(t, err)

	// Create a note that links to the target.
	source, err := svc.Create(ctx, testUserID, CreateNoteReq{
		Title: "Source",
		Body:  "See [[Target]] for details",
	})
	require.NoError(t, err)

	// Verify backlink.
	db, err := mgr.Open(ctx, testUserID)
	require.NoError(t, err)

	backlinks, err := svc.store.GetBacklinks(ctx, db, target.ID)
	require.NoError(t, err)
	require.Len(t, backlinks, 1)
	require.Equal(t, source.ID, backlinks[0].ID)
}

func TestService_Create_ResolvesDanglingLinks(t *testing.T) {
	svc, mgr, _ := setupService(t)
	ctx := context.Background()

	// Create a note with a dangling link first.
	source, err := svc.Create(ctx, testUserID, CreateNoteReq{
		Title: "Eager Linker",
		Body:  "References [[Future Note]]",
	})
	require.NoError(t, err)
	_ = source

	// Now create the target note -- dangling link should resolve.
	target, err := svc.Create(ctx, testUserID, CreateNoteReq{
		Title: "Future Note",
		Body:  "I was referenced before I existed",
	})
	require.NoError(t, err)

	db, err := mgr.Open(ctx, testUserID)
	require.NoError(t, err)

	backlinks, err := svc.store.GetBacklinks(ctx, db, target.ID)
	require.NoError(t, err)
	require.Len(t, backlinks, 1)
	require.Equal(t, source.ID, backlinks[0].ID)
}

func TestService_Create_EmptyTitle(t *testing.T) {
	svc, _, _ := setupService(t)
	ctx := context.Background()

	_, err := svc.Create(ctx, testUserID, CreateNoteReq{Title: "", Body: "body"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "title is required")
}
