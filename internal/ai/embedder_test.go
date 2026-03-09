package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestChunkText(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		require.Nil(t, ChunkText("", 100, 20))
	})

	t.Run("short_text", func(t *testing.T) {
		chunks := ChunkText("hello world", 100, 20)
		require.Equal(t, []string{"hello world"}, chunks)
	})

	t.Run("splits_long_text", func(t *testing.T) {
		// Create text longer than chunk size.
		text := ""
		for i := 0; i < 50; i++ {
			text += "This is a sentence for testing. "
		}
		chunks := ChunkText(text, 200, 40)
		require.True(t, len(chunks) > 1, "expected multiple chunks, got %d", len(chunks))

		// All text should be covered (union of chunks covers original).
		for _, c := range chunks {
			require.NotEmpty(t, c)
		}
	})

	t.Run("respects_paragraph_breaks", func(t *testing.T) {
		text := "First paragraph content here.\n\nSecond paragraph that continues with more content for testing purposes."
		chunks := ChunkText(text, 50, 10)
		require.True(t, len(chunks) >= 2)
	})

	t.Run("whitespace_only", func(t *testing.T) {
		require.Nil(t, ChunkText("   \n\n  ", 100, 20))
	})
}

func TestEmbedder_EmbedNote(t *testing.T) {
	// Mock Ollama server.
	var mu sync.Mutex
	var embedCalls int
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		embedCalls++
		mu.Unlock()

		resp := ollamaEmbedResponse{
			Embeddings: [][]float64{{0.1, 0.2, 0.3}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ollamaServer.Close()

	// Mock ChromaDB server.
	var chromaCalls []string
	chromaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		chromaCalls = append(chromaCalls, r.URL.Path)
		mu.Unlock()

		// Handle collection creation and upsert.
		if r.Method == http.MethodPost {
			// Return collection response for create.
			resp := chromaCollectionResponse{ID: "col-test", Name: "test"}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer chromaServer.Close()

	ollama := NewOllamaClient(ollamaServer.URL, 30*time.Second, 120*time.Second)
	chroma := NewChromaClient(chromaServer.URL)
	mockMgr := newMockDBManager()
	embedder := NewEmbedder(ollama, chroma, mockMgr, "test-model", nil)

	err := embedder.EmbedNote(context.Background(), "user1", "note1", "Test Note", "Short body content")
	require.NoError(t, err)

	mu.Lock()
	require.Equal(t, 1, embedCalls)        // single chunk for short text
	require.True(t, len(chromaCalls) >= 2) // create collection + upsert
	mu.Unlock()
}

func TestEmbedder_HandleEmbedTask(t *testing.T) {
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaEmbedResponse{
			Embeddings: [][]float64{{0.1, 0.2, 0.3}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ollamaServer.Close()

	chromaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chromaCollectionResponse{ID: "col-test", Name: "test"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer chromaServer.Close()

	ollama := NewOllamaClient(ollamaServer.URL, 30*time.Second, 120*time.Second)
	chroma := NewChromaClient(chromaServer.URL)
	mockMgr := newMockDBManager()
	embedder := NewEmbedder(ollama, chroma, mockMgr, "test-model", nil)

	// Create a note in the mock DB for the task handler to query.
	ctx := context.Background()
	db, _ := mockMgr.Open(ctx, "user1")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note1", "Test Note", "test-note.md", "This is the body", "abc", now, now)
	require.NoError(t, err)

	task := &Task{
		ID:      "task1",
		UserID:  "user1",
		Type:    TaskTypeEmbed,
		Payload: json.RawMessage(`{"note_id":"note1"}`),
	}

	result, err := embedder.HandleEmbedTask(ctx, task)
	require.NoError(t, err)
	require.JSONEq(t, `{"status":"embedded"}`, string(result))
}

func TestEmbedder_HandleDeleteEmbedTask(t *testing.T) {
	chromaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chromaCollectionResponse{ID: "col-test", Name: "test"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer chromaServer.Close()

	chroma := NewChromaClient(chromaServer.URL)
	mockMgr := newMockDBManager()
	embedder := NewEmbedder(nil, chroma, mockMgr, "test-model", nil)

	task := &Task{
		ID:      "task2",
		UserID:  "user1",
		Type:    TaskTypeDeleteEmbed,
		Payload: json.RawMessage(`{"note_id":"note1"}`),
	}

	result, err := embedder.HandleDeleteEmbedTask(context.Background(), task)
	require.NoError(t, err)
	require.JSONEq(t, `{"status":"deleted"}`, string(result))
}
