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

		messages := BuildChatMessages("What about caching?", contexts, history, "")

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
		messages := BuildChatMessages("Hello?", nil, nil, "")
		require.Len(t, messages, 2) // system + user
		require.Contains(t, messages[0].Content, "No relevant notes")
	})

	t.Run("history_truncation_to_recent_window", func(t *testing.T) {
		// 30 turns = 60 messages; the recent window keeps only the
		// last maxRecentMessages of those, so the prompt should be
		// system + maxRecentMessages history entries + final user msg.
		var longHistory []ChatMessage
		for i := 0; i < 30; i++ {
			longHistory = append(longHistory,
				ChatMessage{Role: "user", Content: fmt.Sprintf("q%d", i)},
				ChatMessage{Role: "assistant", Content: fmt.Sprintf("a%d", i)},
			)
		}

		messages := BuildChatMessages("new question", nil, longHistory, "")
		require.Equal(t, 1+maxRecentMessages+1, len(messages))

		// The retained slice should be the last maxRecentMessages of
		// longHistory, so messages[1] is the oldest entry that
		// survived truncation.
		oldestKept := longHistory[len(longHistory)-maxRecentMessages]
		require.Equal(t, oldestKept.Content, messages[1].Content)
	})

	t.Run("summary_included_in_system_prompt", func(t *testing.T) {
		summary := "User and assistant earlier discussed migrating from MySQL to PostgreSQL."
		messages := BuildChatMessages("And what about indexing?", nil, nil, summary)

		require.Equal(t, "system", messages[0].Role)
		require.Contains(t, messages[0].Content, "Summary of earlier conversation turns")
		require.Contains(t, messages[0].Content, summary)
	})

	t.Run("blank_summary_omitted", func(t *testing.T) {
		messages := BuildChatMessages("hi", nil, nil, "   \n\t  ")
		require.NotContains(t, messages[0].Content, "Summary of earlier conversation turns")
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
	result, err := chat.Ask(ctx, "user1", "Tell me about caching", nil, "")
	require.NoError(t, err)
	require.Contains(t, result.Response, "caching")
	require.Len(t, result.Citations, 1)
	require.Equal(t, "note1", result.Citations[0].ID)
	require.Equal(t, "API Design", result.Citations[0].Title)
}

func TestChatService_SummarizeHistory(t *testing.T) {
	t.Run("empty_history_returns_existing", func(t *testing.T) {
		chat := &ChatService{}
		got, err := chat.SummarizeHistory(context.Background(), nil, "  prior summary  ")
		require.NoError(t, err)
		require.Equal(t, "prior summary", got)
	})

	t.Run("invalid_role_rejected", func(t *testing.T) {
		chat := &ChatService{}
		_, err := chat.SummarizeHistory(context.Background(),
			[]ChatMessage{{Role: "system", Content: "x"}}, "")
		require.ErrorIs(t, err, ErrInvalidRole)
	})

	t.Run("calls_llm_with_summarization_prompt", func(t *testing.T) {
		var capturedMessages []ChatMessage
		var capturedModel string
		ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/chat" {
				return
			}
			var req ollamaChatRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			capturedModel = req.Model
			capturedMessages = req.Messages

			resp := ollamaChatResponse{Done: true}
			resp.Message.Role = "assistant"
			resp.Message.Content = "  Refreshed summary text.  "
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer ollamaServer.Close()

		ollama := NewOllamaClient(ollamaServer.URL, 30*time.Second, 120*time.Second)
		chat := NewChatService(ollama, ollama, nil, nil, "embed", "summary-model", nil)

		history := []ChatMessage{
			{Role: "user", Content: "Tell me about goroutines."},
			{Role: "assistant", Content: "They are lightweight threads."},
			{Role: "user", Content: "And channels?"},
			{Role: "assistant", Content: "They synchronize goroutines."},
		}

		got, err := chat.SummarizeHistory(context.Background(), history, "Earlier: user is learning Go.")
		require.NoError(t, err)
		require.Equal(t, "Refreshed summary text.", got)
		require.Equal(t, "summary-model", capturedModel)
		require.Len(t, capturedMessages, 2)
		require.Equal(t, "system", capturedMessages[0].Role)
		require.Contains(t, capturedMessages[0].Content, "compress conversations")
		require.Equal(t, "user", capturedMessages[1].Role)
		// Both the existing summary and the new transcript must reach the model.
		require.Contains(t, capturedMessages[1].Content, "Earlier: user is learning Go.")
		require.Contains(t, capturedMessages[1].Content, "goroutines")
		require.Contains(t, capturedMessages[1].Content, "channels")
	})
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
