package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/migrations"
)

// mockManager implements userdb.Manager for search tests.
// It uses in-memory SQLite databases per user, with a unique prefix
// to ensure test isolation across parallel tests.
type mockManager struct {
	mu     sync.Mutex
	dbs    map[string]*sql.DB
	prefix string
}

var mockCounter uint64
var mockCounterMu sync.Mutex

func newMockManager() *mockManager {
	mockCounterMu.Lock()
	mockCounter++
	id := mockCounter
	mockCounterMu.Unlock()
	return &mockManager{
		dbs:    make(map[string]*sql.DB),
		prefix: fmt.Sprintf("search_mock_%d", id),
	}
}

func (m *mockManager) Open(_ context.Context, userID string) (*sql.DB, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if db, ok := m.dbs[userID]; ok {
		return db, nil
	}
	name := fmt.Sprintf("file:%s_%s?mode=memory&cache=shared", m.prefix, userID)
	db, err := sql.Open("sqlite", name)
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")
	db.Exec(migrations.UserSQL)
	m.dbs[userID] = db
	return db, nil
}

func (m *mockManager) Close(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if db, ok := m.dbs[userID]; ok {
		db.Close()
		delete(m.dbs, userID)
	}
	return nil
}

func (m *mockManager) CloseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, db := range m.dbs {
		db.Close()
	}
	m.dbs = nil
	return nil
}

func (m *mockManager) UserNotesDir(userID string) string             { return "/tmp/" + userID }
func (m *mockManager) UserDataDir(userID string) string              { return "/tmp/" + userID }
func (m *mockManager) ListUsers(_ context.Context) ([]string, error) { return nil, nil }
func (m *mockManager) EnsureUserDirs(userID string) error            { return nil }

func TestExtractSnippet(t *testing.T) {
	t.Run("empty_body", func(t *testing.T) {
		require.Equal(t, "", extractSnippet("", "test", 200))
	})

	t.Run("short_body", func(t *testing.T) {
		snippet := extractSnippet("Hello world", "hello", 200)
		require.Contains(t, snippet, "Hello world")
	})

	t.Run("finds_query_word", func(t *testing.T) {
		body := "Lorem ipsum dolor sit amet. The important concept here is caching. And then more text follows."
		snippet := extractSnippet(body, "caching", 60)
		require.Contains(t, snippet, "caching")
	})

	t.Run("start_of_body_when_no_match", func(t *testing.T) {
		body := "The beginning of a long document about various topics."
		snippet := extractSnippet(body, "nonexistent", 30)
		require.Contains(t, snippet, "The beginning")
	})
}

func TestSemanticSearcher_Search(t *testing.T) {
	// Mock Ollama for embedding.
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Embeddings [][]float64 `json:"embeddings"`
		}{
			Embeddings: [][]float64{{0.1, 0.2, 0.3}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ollamaServer.Close()

	// Mock ChromaDB.
	chromaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Collection creation/get.
		if !containsPath(r.URL.Path, "query") {
			resp := map[string]string{"id": "col-123", "name": "test"}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}

		// Query response with results from two notes, one with two chunks.
		resp := map[string]interface{}{
			"ids":       [][]string{{"note1_chunk_0", "note2_chunk_0", "note1_chunk_1"}},
			"distances": [][]float64{{0.2, 0.5, 0.3}},
			"metadatas": [][]map[string]string{
				{
					{"note_id": "note1", "title": "First Note", "chunk_index": "0"},
					{"note_id": "note2", "title": "Second Note", "chunk_index": "0"},
					{"note_id": "note1", "title": "First Note", "chunk_index": "1"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer chromaServer.Close()

	ollama := ai.NewOllamaClient(ollamaServer.URL, 30*time.Second, 120*time.Second)
	chroma := ai.NewChromaClient(chromaServer.URL)
	mgr := newMockManager()

	// Insert notes into mock DB for snippet retrieval.
	ctx := context.Background()
	db, err := mgr.Open(ctx, "user1")
	require.NoError(t, err)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note1", "First Note", "first.md", "Content about caching strategies", "h1", now, now)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note2", "Second Note", "second.md", "Content about databases", "h2", now, now)
	require.NoError(t, err)

	searcher := NewSemanticSearcher(ollama, chroma, mgr, "test-model", nil)

	results, err := searcher.Search(ctx, "user1", "caching", 10)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// note1 should be first (lower distance = higher score).
	require.Equal(t, "note1", results[0].NoteID)
	require.Equal(t, "First Note", results[0].Title)
	require.InDelta(t, 0.8, results[0].Score, 0.01) // 1 - 0.2

	require.Equal(t, "note2", results[1].NoteID)
	require.InDelta(t, 0.5, results[1].Score, 0.01) // 1 - 0.5
}

func TestSemanticSearcher_Deduplication(t *testing.T) {
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Embeddings [][]float64 `json:"embeddings"`
		}{Embeddings: [][]float64{{0.1, 0.2}}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ollamaServer.Close()

	chromaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !containsPath(r.URL.Path, "query") {
			resp := map[string]string{"id": "col-1", "name": "test"}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		// Same note appears 3 times (3 chunks) with different distances.
		resp := map[string]interface{}{
			"ids":       [][]string{{"n1_0", "n1_1", "n1_2"}},
			"distances": [][]float64{{0.3, 0.1, 0.5}},
			"metadatas": [][]map[string]string{
				{
					{"note_id": "n1", "title": "Note One", "chunk_index": "0"},
					{"note_id": "n1", "title": "Note One", "chunk_index": "1"},
					{"note_id": "n1", "title": "Note One", "chunk_index": "2"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer chromaServer.Close()

	ollama := ai.NewOllamaClient(ollamaServer.URL, 30*time.Second, 120*time.Second)
	chroma := ai.NewChromaClient(chromaServer.URL)
	mgr := newMockManager()

	ctx := context.Background()
	db, _ := mgr.Open(ctx, "user1")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"n1", "Note One", "note.md", "Some body", "h", now, now)

	searcher := NewSemanticSearcher(ollama, chroma, mgr, "model", nil)
	results, err := searcher.Search(ctx, "user1", "query", 10)
	require.NoError(t, err)
	require.Len(t, results, 1) // Deduplicated to single note.
	require.Equal(t, "n1", results[0].NoteID)
	require.InDelta(t, 0.9, results[0].Score, 0.01) // Best distance was 0.1.
}

func containsPath(path, substr string) bool {
	return len(path) > 0 && len(substr) > 0 && path != "/" && len(path) > len(substr) && path[len(path)-len(substr):] == substr
}
