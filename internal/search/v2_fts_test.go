package search

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/testutil"
)

func TestFTSStore_SearchScoped_IncludeProject(t *testing.T) {
	db := testutil.TestUserDB(t)
	ctx := context.Background()

	// Insert parent projects to satisfy foreign key constraints.
	_, err := db.ExecContext(ctx,
		`INSERT INTO projects (id, name, slug, created_at, updated_at)
		 VALUES ('proj-agent', 'Agent', 'agent', datetime('now'), datetime('now')),
		        ('proj-user', 'User', 'user', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	// Insert notes in different projects.
	_, err = db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, project_id, created_at, updated_at)
		 VALUES ('n1', 'Agent Note', 'agent-memory/note1.md', 'agent knowledge about patterns', 'h1', 'proj-agent', datetime('now'), datetime('now')),
		        ('n2', 'User Note', 'inbox/note2.md', 'user note about patterns', 'h2', 'proj-user', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	store := NewFTSStore()

	// Search scoped to agent project only.
	results, total, err := store.SearchScoped(ctx, db, "patterns", 10, 0, "proj-agent", "")
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, results, 1)
	require.Equal(t, "n1", results[0].NoteID)
}

func TestFTSStore_SearchScoped_ExcludeProject(t *testing.T) {
	db := testutil.TestUserDB(t)
	ctx := context.Background()

	// Insert parent projects to satisfy foreign key constraints.
	_, err := db.ExecContext(ctx,
		`INSERT INTO projects (id, name, slug, created_at, updated_at)
		 VALUES ('proj-agent', 'Agent', 'agent', datetime('now'), datetime('now')),
		        ('proj-user', 'User', 'user', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, project_id, created_at, updated_at)
		 VALUES ('n1', 'Agent Note', 'agent-memory/note1.md', 'agent knowledge about patterns', 'h1', 'proj-agent', datetime('now'), datetime('now')),
		        ('n2', 'User Note', 'inbox/note2.md', 'user note about patterns', 'h2', 'proj-user', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	store := NewFTSStore()

	// Search excluding agent project.
	results, total, err := store.SearchScoped(ctx, db, "patterns", 10, 0, "", "proj-agent")
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, results, 1)
	require.Equal(t, "n2", results[0].NoteID)
}

func TestFTSStore_SearchScoped_NoFilter(t *testing.T) {
	db := testutil.TestUserDB(t)
	ctx := context.Background()

	// Insert parent projects to satisfy foreign key constraints.
	_, err := db.ExecContext(ctx,
		`INSERT INTO projects (id, name, slug, created_at, updated_at)
		 VALUES ('proj-a', 'Project A', 'proj-a', datetime('now'), datetime('now')),
		        ('proj-b', 'Project B', 'proj-b', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, project_id, created_at, updated_at)
		 VALUES ('n1', 'Note A', 'inbox/a.md', 'searchable content alpha', 'h1', 'proj-a', datetime('now'), datetime('now')),
		        ('n2', 'Note B', 'inbox/b.md', 'searchable content beta', 'h2', 'proj-b', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	store := NewFTSStore()

	// Search without project filter returns all.
	results, total, err := store.SearchScoped(ctx, db, "searchable", 10, 0, "", "")
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, results, 2)
}
