package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAutoLinker_SuggestLinks(t *testing.T) {
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embed" {
			resp := ollamaEmbedResponse{Embeddings: [][]float64{{0.1, 0.2, 0.3}}}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		// Chat completion for link suggestions.
		resp := ollamaChatResponse{Done: true}
		resp.Message.Role = "assistant"
		resp.Message.Content = `[{"target_note_id":"note2","target_title":"Database Design","reason":"Both notes discuss data modeling"}]`
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ollamaServer.Close()

	chromaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path[len(r.URL.Path)-5:] == "query" {
			resp := map[string]interface{}{
				"ids":       [][]string{{"note1_chunk_0", "note2_chunk_0", "note3_chunk_0"}},
				"distances": [][]float64{{0.1, 0.3, 0.5}},
				"metadatas": [][]map[string]string{
					{
						{"note_id": "note1", "title": "API Design"}, // self - should be excluded
						{"note_id": "note2", "title": "Database Design"},
						{"note_id": "note3", "title": "Testing"},
					},
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
	defer chromaServer.Close()

	ollama := NewOllamaClient(ollamaServer.URL, 30*time.Second, 120*time.Second)
	chroma := NewChromaClient(chromaServer.URL)
	mockMgr := newMockDBManager()

	ctx := context.Background()
	db, _ := mockMgr.Open(ctx, "user1")
	now := time.Now().UTC().Format(time.RFC3339Nano)

	db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note1", "API Design", "api.md", "API design patterns and data modeling", "h1", now, now)
	db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note2", "Database Design", "db.md", "Database design and data modeling", "h2", now, now)
	db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note3", "Testing", "test.md", "Testing strategies", "h3", now, now)

	linker := NewAutoLinker(ollama, ollama, chroma, mockMgr, "embed-model", "chat-model", nil, nil)
	suggestions, err := linker.SuggestLinks(ctx, "user1", "note1")
	require.NoError(t, err)
	require.Len(t, suggestions, 1)
	require.Equal(t, "note2", suggestions[0].TargetNoteID)
	require.Equal(t, "Database Design", suggestions[0].TargetTitle)
	require.Contains(t, suggestions[0].Reason, "data modeling")
}

func TestAutoLinker_SuggestLinks_NoRelated(t *testing.T) {
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaEmbedResponse{Embeddings: [][]float64{{0.1, 0.2}}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ollamaServer.Close()

	chromaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path[len(r.URL.Path)-5:] == "query" {
			resp := map[string]interface{}{
				"ids":       [][]string{{"note1_chunk_0"}}, // only self
				"distances": [][]float64{{0.0}},
				"metadatas": [][]map[string]string{
					{{"note_id": "note1", "title": "Lonely Note"}},
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
	defer chromaServer.Close()

	ollama := NewOllamaClient(ollamaServer.URL, 30*time.Second, 120*time.Second)
	chroma := NewChromaClient(chromaServer.URL)
	mockMgr := newMockDBManager()

	ctx := context.Background()
	db, _ := mockMgr.Open(ctx, "user1")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note1", "Lonely Note", "lonely.md", "Just a lonely note", "h1", now, now)

	linker := NewAutoLinker(ollama, ollama, chroma, mockMgr, "embed", "chat", nil, nil)
	suggestions, err := linker.SuggestLinks(ctx, "user1", "note1")
	require.NoError(t, err)
	require.Nil(t, suggestions)
}

func TestAutoLinker_HandleAutolinkTask(t *testing.T) {
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embed" {
			resp := ollamaEmbedResponse{Embeddings: [][]float64{{0.1}}}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		resp := ollamaChatResponse{Done: true}
		resp.Message.Role = "assistant"
		resp.Message.Content = `[]`
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ollamaServer.Close()

	chromaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path[len(r.URL.Path)-5:] == "query" {
			resp := map[string]interface{}{
				"ids":       [][]string{{}},
				"distances": [][]float64{{}},
				"metadatas": [][]map[string]string{{}},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		resp := map[string]string{"id": "col-1", "name": "test"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer chromaServer.Close()

	ollama := NewOllamaClient(ollamaServer.URL, 30*time.Second, 120*time.Second)
	chroma := NewChromaClient(chromaServer.URL)
	mockMgr := newMockDBManager()

	ctx := context.Background()
	db, _ := mockMgr.Open(ctx, "user1")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note1", "Test Note", "test.md", "content", "h", now, now)

	linker := NewAutoLinker(ollama, ollama, chroma, mockMgr, "embed", "chat", nil, nil)
	task := &Task{
		ID:      "task1",
		UserID:  "user1",
		Type:    TaskTypeAutolink,
		Payload: json.RawMessage(`{"note_id":"note1"}`),
	}

	result, err := linker.HandleAutolinkTask(ctx, task)
	require.NoError(t, err)
	require.NotNil(t, result)
}
