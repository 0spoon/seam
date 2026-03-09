package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/reqctx"
)

func setupTestHandler(t *testing.T) (*Handler, *mockDBManager) {
	t.Helper()

	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embed" {
			resp := ollamaEmbedResponse{Embeddings: [][]float64{{0.1, 0.2, 0.3}}}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		resp := ollamaChatResponse{Done: true}
		resp.Message.Role = "assistant"
		resp.Message.Content = "Test response from AI."
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(ollamaServer.Close)

	chromaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path[len(r.URL.Path)-5:] == "query" {
			resp := map[string]interface{}{
				"ids":       [][]string{{"note1_chunk_0"}},
				"distances": [][]float64{{0.2}},
				"metadatas": [][]map[string]string{
					{{"note_id": "note1", "title": "Test Note"}},
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

	ollama := NewOllamaClient(ollamaServer.URL, 30*time.Second, 120*time.Second)
	chroma := NewChromaClient(chromaServer.URL)
	mockMgr := newMockDBManager()

	// Seed data.
	ctx := context.Background()
	db, _ := mockMgr.Open(ctx, "user1")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	db.ExecContext(ctx,
		`INSERT INTO projects (id, name, slug, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		"proj1", "Test Project", "test-project", now, now)
	db.ExecContext(ctx,
		`INSERT INTO notes (id, title, project_id, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"note1", "Test Note", "proj1", "test.md", "This is test content about caching", "h1", now, now)

	embedder := NewEmbedder(ollama, chroma, mockMgr, "embed-model", nil)
	chatSvc := NewChatService(ollama, chroma, mockMgr, "embed-model", "chat-model", nil)
	synth := NewSynthesizer(ollama, mockMgr, "chat-model", nil)
	linker := NewAutoLinker(ollama, chroma, mockMgr, "embed-model", "chat-model", nil, nil)

	handler := NewHandler(nil, chatSvc, synth, linker, embedder, nil, mockMgr, nil)
	return handler, mockMgr
}

func TestHandler_Ask(t *testing.T) {
	handler, _ := setupTestHandler(t)

	body := `{"query":"What about caching?"}`
	req := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reqctx.WithUserID(req.Context(), "user1"))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result ChatResult
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.NotEmpty(t, result.Response)
}

func TestHandler_Ask_MissingQuery(t *testing.T) {
	handler, _ := setupTestHandler(t)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reqctx.WithUserID(req.Context(), "user1"))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Synthesize(t *testing.T) {
	handler, _ := setupTestHandler(t)

	body := `{"scope":"project","project_id":"proj1","prompt":"summarize"}`
	req := httptest.NewRequest(http.MethodPost, "/synthesize", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reqctx.WithUserID(req.Context(), "user1"))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result SynthesisResult
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.NotEmpty(t, result.Response)
}

func TestHandler_RelatedNotes(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/notes/note1/related", nil)
	req = req.WithContext(reqctx.WithUserID(req.Context(), "user1"))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}

func TestHandler_RelatedNotes_NotFound(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/notes/nonexistent/related", nil)
	req = req.WithContext(reqctx.WithUserID(req.Context(), "user1"))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

// setupTestHandlerWithWriter creates a Handler with a Writer configured for assist tests.
func setupTestHandlerWithWriter(t *testing.T) (*Handler, *mockDBManager) {
	t.Helper()

	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaChatResponse{Done: true}
		resp.Message.Role = "assistant"
		resp.Message.Content = "Expanded text from AI."
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(ollamaServer.Close)

	ollama := NewOllamaClient(ollamaServer.URL, 30*time.Second, 120*time.Second)
	mockMgr := newMockDBManager()

	// Seed data.
	ctx := context.Background()
	db, _ := mockMgr.Open(ctx, "user1")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	db.ExecContext(ctx,
		`INSERT INTO projects (id, name, slug, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		"proj1", "Test Project", "test-project", now, now)
	db.ExecContext(ctx,
		`INSERT INTO notes (id, title, project_id, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"note1", "Test Note", "proj1", "test.md", "This is test content about caching", "h1", now, now)

	writer := NewWriter(ollama, mockMgr, "chat-model", nil)
	handler := NewHandler(nil, nil, nil, nil, nil, writer, mockMgr, nil)
	return handler, mockMgr
}

func TestHandler_Assist_Success(t *testing.T) {
	handler, _ := setupTestHandlerWithWriter(t)

	body := `{"action":"expand","selection":"some text"}`
	req := httptest.NewRequest(http.MethodPost, "/notes/note1/assist", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reqctx.WithUserID(req.Context(), "user1"))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.NotEmpty(t, result["result"])
}

func TestHandler_Assist_MissingAction(t *testing.T) {
	handler, _ := setupTestHandlerWithWriter(t)

	body := `{"selection":"some text"}`
	req := httptest.NewRequest(http.MethodPost, "/notes/note1/assist", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reqctx.WithUserID(req.Context(), "user1"))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Assist_InvalidAction(t *testing.T) {
	handler, _ := setupTestHandlerWithWriter(t)

	body := `{"action":"invalid-action","selection":"some text"}`
	req := httptest.NewRequest(http.MethodPost, "/notes/note1/assist", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reqctx.WithUserID(req.Context(), "user1"))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)

	var result map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.Contains(t, result["error"], "invalid action")
}

func TestHandler_Assist_NoteNotFound(t *testing.T) {
	handler, _ := setupTestHandlerWithWriter(t)

	// No selection -- writer will try to load the note body from DB.
	body := `{"action":"expand"}`
	req := httptest.NewRequest(http.MethodPost, "/notes/nonexistent/assist", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reqctx.WithUserID(req.Context(), "user1"))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandler_Assist_WriterNil(t *testing.T) {
	handler, _ := setupTestHandler(t)

	body := `{"action":"expand","selection":"some text"}`
	req := httptest.NewRequest(http.MethodPost, "/notes/note1/assist", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reqctx.WithUserID(req.Context(), "user1"))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)

	var result map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.Contains(t, result["error"], "not configured")
}

func TestHandler_Unauthorized(t *testing.T) {
	handler, _ := setupTestHandler(t)

	// No user ID in context.
	req := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewBufferString(`{"query":"test"}`))

	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Mount("/", handler.Routes())
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}
