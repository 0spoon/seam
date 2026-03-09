package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/internal/reqctx"
)

func setupTestHandler(t *testing.T) *Handler {
	t.Helper()

	mockMgr := newMockManager()
	ctx := context.Background()

	// Seed data for user1.
	db, err := mockMgr.Open(ctx, "user1")
	require.NoError(t, err)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note1", "API Design", "api.md", "REST API design patterns and caching strategies", "h1", now, now)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note2", "Go Testing", "testing.md", "Unit testing with testify and table-driven tests", "h2", now, now)
	require.NoError(t, err)

	// Insert FTS index entries.
	_, err = db.ExecContext(ctx,
		`INSERT INTO notes_fts (rowid, title, body) VALUES (
		 (SELECT rowid FROM notes WHERE id = 'note1'), 'API Design', 'REST API design patterns and caching strategies')`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`INSERT INTO notes_fts (rowid, title, body) VALUES (
		 (SELECT rowid FROM notes WHERE id = 'note2'), 'Go Testing', 'Unit testing with testify and table-driven tests')`)
	require.NoError(t, err)

	ftsStore := NewFTSStore()
	svc := NewService(ftsStore, mockMgr, nil)

	// Set up a mock semantic searcher if needed.
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"embeddings": [][]float64{{0.1, 0.2, 0.3}}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(ollamaServer.Close)

	chromaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if containsPath(r.URL.Path, "query") {
			resp := map[string]interface{}{
				"ids":       [][]string{{"note1_chunk_0", "note2_chunk_0"}},
				"distances": [][]float64{{0.2, 0.5}},
				"metadatas": [][]map[string]string{
					{{"note_id": "note1", "title": "API Design"}, {"note_id": "note2", "title": "Go Testing"}},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		resp := map[string]string{"id": "col-1", "name": "test"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(chromaServer.Close)

	ollama := ai.NewOllamaClient(ollamaServer.URL, 30*time.Second, 120*time.Second)
	chroma := ai.NewChromaClient(chromaServer.URL)
	semanticSearcher := NewSemanticSearcher(ollama, chroma, mockMgr, "embed-model", nil)
	svc.SetSemanticSearcher(semanticSearcher)

	return NewHandler(svc, nil)
}

func TestHandler_SearchFTS(t *testing.T) {
	handler := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/?q=API", nil)
	req = req.WithContext(reqctx.WithUserID(req.Context(), "user1"))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))

	// Should have X-Total-Count header.
	require.NotEmpty(t, w.Header().Get("X-Total-Count"))

	var results []FTSResult
	require.NoError(t, json.NewDecoder(w.Body).Decode(&results))
	require.NotEmpty(t, results)
	require.Equal(t, "note1", results[0].NoteID)
}

func TestHandler_SearchFTS_MissingQuery(t *testing.T) {
	handler := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(reqctx.WithUserID(req.Context(), "user1"))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_SearchFTS_Unauthorized(t *testing.T) {
	handler := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/?q=test", nil)
	// No user ID in context.

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_SearchFTS_CustomLimitOffset(t *testing.T) {
	handler := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/?q=test&limit=1&offset=0", nil)
	req = req.WithContext(reqctx.WithUserID(req.Context(), "user1"))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}

func TestHandler_SearchSemantic(t *testing.T) {
	handler := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/semantic?q=caching+patterns", nil)
	req = req.WithContext(reqctx.WithUserID(req.Context(), "user1"))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var results []SemanticResult
	require.NoError(t, json.NewDecoder(w.Body).Decode(&results))
	require.NotEmpty(t, results)
}

func TestHandler_SearchSemantic_MissingQuery(t *testing.T) {
	handler := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/semantic", nil)
	req = req.WithContext(reqctx.WithUserID(req.Context(), "user1"))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_SearchSemantic_Unauthorized(t *testing.T) {
	handler := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/semantic?q=test", nil)

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_SearchSemantic_CustomLimit(t *testing.T) {
	handler := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/semantic?q=api&limit=5", nil)
	req = req.WithContext(reqctx.WithUserID(req.Context(), "user1"))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}
