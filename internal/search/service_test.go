package search

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/testutil"
)

// ---------------------------------------------------------------------------
// NewService
// ---------------------------------------------------------------------------

func TestNewService_NilLogger_UsesDefault(t *testing.T) {
	svc := NewService(nil, nil, nil)
	require.NotNil(t, svc)
	require.NotNil(t, svc.logger)
}

func TestNewService_StoresFields(t *testing.T) {
	fts := NewFTSStore()
	mgr := newMockManager()
	svc := NewService(fts, mgr, nil)

	require.Equal(t, fts, svc.ftsStore)
	require.Equal(t, mgr, svc.dbManager)
	require.Nil(t, svc.semantic)
}

// ---------------------------------------------------------------------------
// SetSemanticSearcher
// ---------------------------------------------------------------------------

func TestService_SetSemanticSearcher_SetsField(t *testing.T) {
	svc := NewService(nil, nil, nil)
	require.Nil(t, svc.semantic)

	searcher := &SemanticSearcher{}
	svc.SetSemanticSearcher(searcher)
	require.Equal(t, searcher, svc.semantic)
}

func TestService_SetSemanticSearcher_OverwritesPrevious(t *testing.T) {
	svc := NewService(nil, nil, nil)

	s1 := &SemanticSearcher{}
	s2 := &SemanticSearcher{}
	svc.SetSemanticSearcher(s1)
	svc.SetSemanticSearcher(s2)
	require.Equal(t, s2, svc.semantic)
}

// ---------------------------------------------------------------------------
// SearchSemantic -- nil semantic searcher
// ---------------------------------------------------------------------------

func TestService_SearchSemantic_NilSearcher_ReturnsError(t *testing.T) {
	svc := NewService(nil, nil, nil)
	_, err := svc.SearchSemantic(context.Background(), "user1", "query", 10)
	require.Error(t, err)
	require.Contains(t, err.Error(), "semantic search not configured")
}

// ---------------------------------------------------------------------------
// SearchFTS -- integration with real in-memory SQLite
// ---------------------------------------------------------------------------

func TestService_SearchFTS_ReturnsResults(t *testing.T) {
	mgr := newMockManager()
	ctx := context.Background()

	// Seed a note into the mock user DB.
	db, err := mgr.Open(ctx, "user1")
	require.NoError(t, err)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note1", "Concurrency Patterns", "concurrency.md",
		"Goroutines and channels for concurrent programming",
		"hash1", now, now,
	)
	require.NoError(t, err)

	fts := NewFTSStore()
	svc := NewService(fts, mgr, nil)

	results, total, err := svc.SearchFTS(ctx, "user1", "goroutines", 100, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, results, 1)
	require.Equal(t, "note1", results[0].NoteID)
}

func TestService_SearchFTS_EmptyQuery_ReturnsEmpty(t *testing.T) {
	mgr := newMockManager()
	fts := NewFTSStore()
	svc := NewService(fts, mgr, nil)

	results, total, err := svc.SearchFTS(context.Background(), "user1", "", 100, 0)
	require.NoError(t, err)
	require.Equal(t, 0, total)
	require.Nil(t, results)
}

func TestService_SearchFTS_NoMatches_ReturnsEmpty(t *testing.T) {
	mgr := newMockManager()
	ctx := context.Background()

	db, err := mgr.Open(ctx, "user1")
	require.NoError(t, err)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note1", "Some Title", "some.md", "Some body", "h1", now, now,
	)
	require.NoError(t, err)

	fts := NewFTSStore()
	svc := NewService(fts, mgr, nil)

	results, total, err := svc.SearchFTS(ctx, "user1", "nonexistent", 100, 0)
	require.NoError(t, err)
	require.Equal(t, 0, total)
	require.Nil(t, results)
}

func TestService_SearchFTS_UsesTestUserDB(t *testing.T) {
	// Demonstrate direct usage of testutil.TestUserDB with FTSStore.
	db := testutil.TestUserDB(t)
	ctx := context.Background()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"n1", "Testing Helpers", "helpers.md",
		"Shared test helpers for database setup",
		"h1", now, now,
	)
	require.NoError(t, err)

	fts := NewFTSStore()
	results, total, err := fts.Search(ctx, db, "helpers", 100, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, results, 1)
	require.Equal(t, "n1", results[0].NoteID)
}
