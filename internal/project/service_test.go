package project_test

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

const testUserID = "01HTEST000000000000000001"

func newTestService(t *testing.T) (*project.Service, userdb.Manager) {
	t.Helper()
	dataDir := t.TempDir()
	mgr := userdb.NewSQLManager(dataDir, 30*time.Minute, slog.Default())
	t.Cleanup(func() { mgr.CloseAll() })
	store := project.NewStore()
	svc := project.NewService(store, mgr, slog.Default())
	return svc, mgr
}

func TestService_Create_Success(t *testing.T) {
	svc, mgr := newTestService(t)
	ctx := context.Background()

	p, err := svc.Create(ctx, testUserID, "My Project", "a description")
	require.NoError(t, err)
	require.NotNil(t, p)
	require.NotEmpty(t, p.ID)
	require.Equal(t, "My Project", p.Name)
	require.Equal(t, "my-project", p.Slug)
	require.Equal(t, "a description", p.Description)
	require.False(t, p.CreatedAt.IsZero())
	require.False(t, p.UpdatedAt.IsZero())

	// Verify directory was created.
	projectDir := filepath.Join(mgr.UserNotesDir(testUserID), "my-project")
	info, err := os.Stat(projectDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestService_Create_EmptyName(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	_, err := svc.Create(ctx, testUserID, "", "desc")
	require.Error(t, err)
}

func TestService_Create_DuplicateSlug(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	_, err := svc.Create(ctx, testUserID, "My Project", "")
	require.NoError(t, err)

	_, err = svc.Create(ctx, testUserID, "My Project", "")
	require.Error(t, err)
	require.ErrorIs(t, err, project.ErrSlugExists)
}

func TestService_Get_Success(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, testUserID, "Alpha", "")
	require.NoError(t, err)

	got, err := svc.Get(ctx, testUserID, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "Alpha", got.Name)
}

func TestService_Get_NotFound(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	_, err := svc.Get(ctx, testUserID, "nonexistent")
	require.Error(t, err)
	require.ErrorIs(t, err, project.ErrNotFound)
}

func TestService_List_Empty(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// Open the DB so it exists for listing.
	_, err := svc.List(ctx, testUserID)
	require.NoError(t, err)
}

func TestService_List_MultipleProjects(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	_, err := svc.Create(ctx, testUserID, "Alpha", "")
	require.NoError(t, err)
	_, err = svc.Create(ctx, testUserID, "Beta", "")
	require.NoError(t, err)

	projects, err := svc.List(ctx, testUserID)
	require.NoError(t, err)
	require.Len(t, projects, 2)
}

func TestService_Update_Name(t *testing.T) {
	svc, mgr := newTestService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, testUserID, "Old Name", "desc")
	require.NoError(t, err)

	newName := "New Name"
	updated, err := svc.Update(ctx, testUserID, created.ID, &newName, nil)
	require.NoError(t, err)
	require.Equal(t, "New Name", updated.Name)
	require.Equal(t, "new-name", updated.Slug)
	require.Equal(t, "desc", updated.Description)
	require.True(t, updated.UpdatedAt.After(created.UpdatedAt) || updated.UpdatedAt.Equal(created.UpdatedAt))

	// Old directory should be gone, new one should exist.
	oldDir := filepath.Join(mgr.UserNotesDir(testUserID), "old-name")
	_, err = os.Stat(oldDir)
	require.True(t, os.IsNotExist(err))

	newDir := filepath.Join(mgr.UserNotesDir(testUserID), "new-name")
	info, err := os.Stat(newDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestService_Update_DescriptionOnly(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, testUserID, "Project", "old desc")
	require.NoError(t, err)

	newDesc := "new desc"
	updated, err := svc.Update(ctx, testUserID, created.ID, nil, &newDesc)
	require.NoError(t, err)
	require.Equal(t, "Project", updated.Name)
	require.Equal(t, "new desc", updated.Description)
}

func TestService_Update_NotFound(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	name := "X"
	_, err := svc.Update(ctx, testUserID, "nonexistent", &name, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, project.ErrNotFound)
}

func TestService_Delete_EmptyDir(t *testing.T) {
	svc, mgr := newTestService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, testUserID, "Doomed", "")
	require.NoError(t, err)

	projectDir := filepath.Join(mgr.UserNotesDir(testUserID), "doomed")
	_, err = os.Stat(projectDir)
	require.NoError(t, err)

	err = svc.Delete(ctx, testUserID, created.ID, "inbox")
	require.NoError(t, err)

	// Directory should be removed (it was empty).
	_, err = os.Stat(projectDir)
	require.True(t, os.IsNotExist(err))

	// Project should be gone from DB.
	_, err = svc.Get(ctx, testUserID, created.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, project.ErrNotFound)
}

func TestService_Delete_NonEmptyDir(t *testing.T) {
	svc, mgr := newTestService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, testUserID, "HasFiles", "")
	require.NoError(t, err)

	// Put a file in the directory so it is not empty.
	projectDir := filepath.Join(mgr.UserNotesDir(testUserID), "hasfiles")
	err = os.WriteFile(filepath.Join(projectDir, "note.md"), []byte("hello"), 0o644)
	require.NoError(t, err)

	err = svc.Delete(ctx, testUserID, created.ID, "delete")
	require.NoError(t, err)

	// Directory should be removed (RemoveAll cleans up everything).
	_, err = os.Stat(projectDir)
	require.True(t, os.IsNotExist(err))
}

func TestService_Delete_NotFound(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	err := svc.Delete(ctx, testUserID, "nonexistent", "inbox")
	require.Error(t, err)
	require.ErrorIs(t, err, project.ErrNotFound)
}

func TestService_Delete_InvalidCascade(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	err := svc.Delete(ctx, testUserID, "any-id", "invalid")
	require.Error(t, err)
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "simple", input: "My Project", expected: "my-project"},
		{name: "underscores", input: "my_project_name", expected: "my-project-name"},
		{name: "special_chars", input: "Hello, World! (2024)", expected: "hello-world-2024"},
		{name: "multiple_spaces", input: "too   many   spaces", expected: "too-many-spaces"},
		{name: "leading_trailing", input: "  --hello-- ", expected: "hello"},
		{name: "all_special", input: "!!!@@@###", expected: ""},
		{name: "mixed", input: "Project: Alpha_v2 (beta)", expected: "project-alpha-v2-beta"},
		{name: "already_slug", input: "my-project", expected: "my-project"},
		{name: "uppercase", input: "UPPERCASE", expected: "uppercase"},
		{name: "numbers", input: "project123", expected: "project123"},
		{name: "hyphens_collapse", input: "a---b---c", expected: "a-b-c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := project.Slugify(tt.input)
			require.Equal(t, tt.expected, got)
		})
	}
}
