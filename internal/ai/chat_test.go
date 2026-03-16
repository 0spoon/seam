package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildChatMessages(t *testing.T) {
	t.Run("with_context_and_history", func(t *testing.T) {
		contexts := []noteSnippet{
			{Title: "Note A", Body: "Content about caching"},
			{Title: "Note B", Body: "Content about databases"},
		}
		history := []ChatMessage{
			{Role: "user", Content: "previous question"},
			{Role: "assistant", Content: "previous answer"},
		}

		messages := BuildChatMessages("What about caching?", contexts, history)

		// System message with context.
		require.Equal(t, "system", messages[0].Role)
		require.Contains(t, messages[0].Content, "Note A")
		require.Contains(t, messages[0].Content, "Note B")
		require.Contains(t, messages[0].Content, "Content about caching")

		// History preserved.
		require.Equal(t, "user", messages[1].Role)
		require.Equal(t, "previous question", messages[1].Content)
		require.Equal(t, "assistant", messages[2].Role)

		// Final user message.
		require.Equal(t, "user", messages[len(messages)-1].Role)
		require.Equal(t, "What about caching?", messages[len(messages)-1].Content)
	})

	t.Run("no_context", func(t *testing.T) {
		messages := BuildChatMessages("Hello?", nil, nil)
		require.Len(t, messages, 2) // system + user
		require.Contains(t, messages[0].Content, "No relevant notes")
	})

	t.Run("history_truncation", func(t *testing.T) {
		var longHistory []ChatMessage
		for i := 0; i < 20; i++ {
			longHistory = append(longHistory,
				ChatMessage{Role: "user", Content: fmt.Sprintf("q%d", i)},
				ChatMessage{Role: "assistant", Content: fmt.Sprintf("a%d", i)},
			)
		}

		messages := BuildChatMessages("new question", nil, longHistory)
		// System + truncated history (last 10 messages = 5 turns) + user.
		require.Equal(t, 1+maxConversationTurns*2+1, len(messages))
	})
}

func TestChatService_Ask(t *testing.T) {
	// Mock Ollama server.
	callCount := 0
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path == "/api/embed" {
			resp := ollamaEmbedResponse{Embeddings: [][]float64{{0.1, 0.2, 0.3}}}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		if r.URL.Path == "/api/chat" {
			resp := ollamaChatResponse{Done: true}
			resp.Message.Role = "assistant"
			resp.Message.Content = "Based on your note 'API Design', caching is important."
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
	}))
	defer ollamaServer.Close()

	chromaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path[len(r.URL.Path)-5:] == "query" {
			resp := map[string]interface{}{
				"ids":       [][]string{{"note1_chunk_0"}},
				"distances": [][]float64{{0.2}},
				"metadatas": [][]map[string]string{
					{{"note_id": "note1", "title": "API Design"}},
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

	// Insert a note.
	ctx := context.Background()
	db, _ := mockMgr.Open(ctx, "user1")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	db.ExecContext(ctx,
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"note1", "API Design", "api-design.md", "Caching is important for API performance.", "h1", now, now)

	chat := NewChatService(ollama, ollama, chroma, mockMgr, "embed-model", "chat-model", nil)
	result, err := chat.Ask(ctx, "user1", "Tell me about caching", nil)
	require.NoError(t, err)
	require.Contains(t, result.Response, "caching")
	require.Len(t, result.Citations, 1)
	require.Equal(t, "note1", result.Citations[0].ID)
	require.Equal(t, "API Design", result.Citations[0].Title)
}

func TestChatService_HandleChatTask(t *testing.T) {
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embed" {
			resp := ollamaEmbedResponse{Embeddings: [][]float64{{0.1, 0.2}}}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		resp := ollamaChatResponse{Done: true}
		resp.Message.Role = "assistant"
		resp.Message.Content = "The answer is 42."
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
	chat := NewChatService(ollama, ollama, chroma, mockMgr, "embed", "chat", nil)

	task := &Task{
		ID:      "task1",
		UserID:  "user1",
		Type:    TaskTypeChat,
		Payload: json.RawMessage(`{"query":"What is the answer?"}`),
	}

	result, err := chat.HandleChatTask(context.Background(), task)
	require.NoError(t, err)

	var chatResult ChatResult
	require.NoError(t, json.Unmarshal(result, &chatResult))
	require.Equal(t, "The answer is 42.", chatResult.Response)
}
