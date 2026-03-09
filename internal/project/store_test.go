package project_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/testutil"
)

func TestStore_Create_Success(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := project.NewStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	p := &project.Project{
		ID:          "01HTEST000000000000000001",
		Name:        "My Project",
		Slug:        "my-project",
		Description: "A test project",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err := store.Create(ctx, db, p)
	require.NoError(t, err)

	got, err := store.Get(ctx, db, p.ID)
	require.NoError(t, err)
	require.Equal(t, p.ID, got.ID)
	require.Equal(t, p.Name, got.Name)
	require.Equal(t, p.Slug, got.Slug)
	require.Equal(t, p.Description, got.Description)
	require.Equal(t, p.CreatedAt, got.CreatedAt)
	require.Equal(t, p.UpdatedAt, got.UpdatedAt)
}

func TestStore_Create_DuplicateSlug(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := project.NewStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	p1 := &project.Project{
		ID: "01HTEST000000000000000001", Name: "Project A", Slug: "same-slug",
		Description: "", CreatedAt: now, UpdatedAt: now,
	}
	p2 := &project.Project{
		ID: "01HTEST000000000000000002", Name: "Project B", Slug: "same-slug",
		Description: "", CreatedAt: now, UpdatedAt: now,
	}

	err := store.Create(ctx, db, p1)
	require.NoError(t, err)

	err = store.Create(ctx, db, p2)
	require.Error(t, err)
	require.ErrorIs(t, err, project.ErrSlugExists)
}

func TestStore_Get_NotFound(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := project.NewStore()
	ctx := context.Background()

	_, err := store.Get(ctx, db, "nonexistent")
	require.Error(t, err)
	require.ErrorIs(t, err, project.ErrNotFound)
}

func TestStore_GetBySlug_Success(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := project.NewStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	p := &project.Project{
		ID: "01HTEST000000000000000001", Name: "My Project", Slug: "my-project",
		Description: "desc", CreatedAt: now, UpdatedAt: now,
	}
	err := store.Create(ctx, db, p)
	require.NoError(t, err)

	got, err := store.GetBySlug(ctx, db, "my-project")
	require.NoError(t, err)
	require.Equal(t, p.ID, got.ID)
	require.Equal(t, p.Name, got.Name)
}

func TestStore_GetBySlug_NotFound(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := project.NewStore()
	ctx := context.Background()

	_, err := store.GetBySlug(ctx, db, "nonexistent")
	require.Error(t, err)
	require.ErrorIs(t, err, project.ErrNotFound)
}

func TestStore_List_Empty(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := project.NewStore()
	ctx := context.Background()

	projects, err := store.List(ctx, db)
	require.NoError(t, err)
	require.Empty(t, projects)
}

func TestStore_List_MultipleProjects(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := project.NewStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	p1 := &project.Project{
		ID: "01HTEST000000000000000001", Name: "Alpha", Slug: "alpha",
		Description: "", CreatedAt: now, UpdatedAt: now,
	}
	p2 := &project.Project{
		ID: "01HTEST000000000000000002", Name: "Beta", Slug: "beta",
		Description: "", CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	}

	err := store.Create(ctx, db, p1)
	require.NoError(t, err)
	err = store.Create(ctx, db, p2)
	require.NoError(t, err)

	projects, err := store.List(ctx, db)
	require.NoError(t, err)
	require.Len(t, projects, 2)
	// Ordered by created_at DESC, so Beta first.
	require.Equal(t, "Beta", projects[0].Name)
	require.Equal(t, "Alpha", projects[1].Name)
}

func TestStore_Update_Success(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := project.NewStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	p := &project.Project{
		ID: "01HTEST000000000000000001", Name: "Original", Slug: "original",
		Description: "old desc", CreatedAt: now, UpdatedAt: now,
	}
	err := store.Create(ctx, db, p)
	require.NoError(t, err)

	p.Name = "Updated"
	p.Slug = "updated"
	p.Description = "new desc"
	p.UpdatedAt = now.Add(time.Minute)

	err = store.Update(ctx, db, p)
	require.NoError(t, err)

	got, err := store.Get(ctx, db, p.ID)
	require.NoError(t, err)
	require.Equal(t, "Updated", got.Name)
	require.Equal(t, "updated", got.Slug)
	require.Equal(t, "new desc", got.Description)
	require.Equal(t, p.UpdatedAt, got.UpdatedAt)
}

func TestStore_Update_NotFound(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := project.NewStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	p := &project.Project{
		ID: "nonexistent", Name: "X", Slug: "x",
		Description: "", CreatedAt: now, UpdatedAt: now,
	}
	err := store.Update(ctx, db, p)
	require.Error(t, err)
	require.ErrorIs(t, err, project.ErrNotFound)
}

func TestStore_Update_DuplicateSlug(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := project.NewStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	p1 := &project.Project{
		ID: "01HTEST000000000000000001", Name: "Alpha", Slug: "alpha",
		Description: "", CreatedAt: now, UpdatedAt: now,
	}
	p2 := &project.Project{
		ID: "01HTEST000000000000000002", Name: "Beta", Slug: "beta",
		Description: "", CreatedAt: now, UpdatedAt: now,
	}

	err := store.Create(ctx, db, p1)
	require.NoError(t, err)
	err = store.Create(ctx, db, p2)
	require.NoError(t, err)

	// Try to update p2's slug to conflict with p1.
	p2.Slug = "alpha"
	p2.UpdatedAt = now.Add(time.Minute)
	err = store.Update(ctx, db, p2)
	require.Error(t, err)
	require.ErrorIs(t, err, project.ErrSlugExists)
}

func TestStore_Delete_Success(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := project.NewStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	p := &project.Project{
		ID: "01HTEST000000000000000001", Name: "Doomed", Slug: "doomed",
		Description: "", CreatedAt: now, UpdatedAt: now,
	}
	err := store.Create(ctx, db, p)
	require.NoError(t, err)

	err = store.Delete(ctx, db, p.ID)
	require.NoError(t, err)

	_, err = store.Get(ctx, db, p.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, project.ErrNotFound)
}

func TestStore_Delete_NotFound(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := project.NewStore()
	ctx := context.Background()

	err := store.Delete(ctx, db, "nonexistent")
	require.Error(t, err)
	require.ErrorIs(t, err, project.ErrNotFound)
}

func TestStore_Create_EmptyDescription(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := project.NewStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	p := &project.Project{
		ID: "01HTEST000000000000000001", Name: "No Desc", Slug: "no-desc",
		Description: "", CreatedAt: now, UpdatedAt: now,
	}

	err := store.Create(ctx, db, p)
	require.NoError(t, err)

	got, err := store.Get(ctx, db, p.ID)
	require.NoError(t, err)
	require.Equal(t, "", got.Description)
}
