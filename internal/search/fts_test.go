package search

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/testutil"
)

func TestFTSStore_Search_BasicQuery(t *testing.T) {
	db := testutil.TestUserDB(t)
	ctx := context.Background()

	// Insert test notes.
	_, err := db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note1", "API Design Patterns", "api-design-patterns.md",
		"REST API design best practices for Go applications",
		"hash1", "2026-03-01T10:00:00Z", "2026-03-01T10:00:00Z",
	)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note2", "Database Schema Design", "database-schema.md",
		"SQLite database schema design for applications",
		"hash2", "2026-03-02T10:00:00Z", "2026-03-02T10:00:00Z",
	)
	require.NoError(t, err)

	store := NewFTSStore()

	// Search for "design" -- should match both notes.
	results, total, err := store.Search(ctx, db, "design", 100, 0)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, results, 2)

	// Search for "REST" -- should match only first note.
	results, total, err = store.Search(ctx, db, "REST", 100, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, results, 1)
	require.Equal(t, "note1", results[0].NoteID)
}

func TestFTSStore_Search_PrefixQuery(t *testing.T) {
	db := testutil.TestUserDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note1", "Caching Strategies", "caching.md",
		"Various caching strategies for web applications",
		"hash1", "2026-03-01T10:00:00Z", "2026-03-01T10:00:00Z",
	)
	require.NoError(t, err)

	store := NewFTSStore()

	results, total, err := store.Search(ctx, db, "cach*", 100, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, results, 1)
}

func TestFTSStore_Search_EmptyQuery(t *testing.T) {
	db := testutil.TestUserDB(t)
	ctx := context.Background()

	store := NewFTSStore()

	results, total, err := store.Search(ctx, db, "", 100, 0)
	require.NoError(t, err)
	require.Equal(t, 0, total)
	require.Nil(t, results)
}

func TestFTSStore_Search_NoResults(t *testing.T) {
	db := testutil.TestUserDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note1", "Test Note", "test.md", "Some body text", "hash1",
		"2026-03-01T10:00:00Z", "2026-03-01T10:00:00Z",
	)
	require.NoError(t, err)

	store := NewFTSStore()

	results, total, err := store.Search(ctx, db, "nonexistent", 100, 0)
	require.NoError(t, err)
	require.Equal(t, 0, total)
	require.Nil(t, results)
}

func TestFTSStore_Search_SpecialCharacters(t *testing.T) {
	db := testutil.TestUserDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note1", "Test", "test.md", "Some content", "hash1",
		"2026-03-01T10:00:00Z", "2026-03-01T10:00:00Z",
	)
	require.NoError(t, err)

	store := NewFTSStore()

	// These should not cause FTS5 syntax errors.
	for _, q := range []string{
		`test AND OR NOT`,
		`"quoted"`,
		`(parens)`,
		`test*`,
		`NEAR test`,
	} {
		_, _, err := store.Search(ctx, db, q, 100, 0)
		require.NoError(t, err, "query should not error: %q", q)
	}
}

func TestFTSStore_Search_Pagination(t *testing.T) {
	db := testutil.TestUserDB(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := db.ExecContext(ctx,
			`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			"note"+string(rune('a'+i)), "Test Note", "test-"+string(rune('a'+i))+".md",
			"Common search term content", "hash"+string(rune('a'+i)),
			"2026-03-01T10:00:00Z", "2026-03-01T10:00:00Z",
		)
		require.NoError(t, err)
	}

	store := NewFTSStore()

	// Get first page.
	results, total, err := store.Search(ctx, db, "common", 2, 0)
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, results, 2)

	// Get second page.
	results, _, err = store.Search(ctx, db, "common", 2, 2)
	require.NoError(t, err)
	require.Len(t, results, 2)
}

func TestFTSStore_Search_AutoSync(t *testing.T) {
	db := testutil.TestUserDB(t)
	ctx := context.Background()

	// Insert a note.
	_, err := db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note1", "Original Title", "test.md", "Original body text", "hash1",
		"2026-03-01T10:00:00Z", "2026-03-01T10:00:00Z",
	)
	require.NoError(t, err)

	store := NewFTSStore()

	// Should find by original content.
	results, _, err := store.Search(ctx, db, "Original", 100, 0)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Update the note.
	_, err = db.ExecContext(ctx,
		`UPDATE notes SET title = ?, body = ? WHERE id = ?`,
		"Updated Title", "Updated body text", "note1",
	)
	require.NoError(t, err)

	// Should find by new content (trigger auto-synced FTS).
	results, _, err = store.Search(ctx, db, "Updated", 100, 0)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Should NOT find by old content.
	results, _, err = store.Search(ctx, db, "Original", 100, 0)
	require.NoError(t, err)
	require.Len(t, results, 0)

	// Delete the note.
	_, err = db.ExecContext(ctx, `DELETE FROM notes WHERE id = ?`, "note1")
	require.NoError(t, err)

	// Should no longer be found.
	results, _, err = store.Search(ctx, db, "Updated", 100, 0)
	require.NoError(t, err)
	require.Len(t, results, 0)
}

func TestSanitizeFTSQuery(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple word", "test", `"test"`},
		{"multiple words", "hello world", `"hello" "world"`},
		{"strip operators", "test AND other", `"test" "other"`},
		{"strip parens", "(test)", `"test"`},
		{"strip quotes", `"test"`, `"test"`},
		{"prefix query", "cach*", `"cach"*`},
		{"empty", "", ""},
		{"only operators", "AND OR NOT", ""},
		{"mixed case operators", "test and or not other", `"test" "other"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeFTSQuery(tc.input)
			require.Equal(t, tc.want, got)
		})
	}
}
