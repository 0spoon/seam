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

func TestOllamaClient_GenerateEmbedding(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/api/embed", r.URL.Path)
			require.Equal(t, http.MethodPost, r.Method)

			var req ollamaEmbedRequest
			err := json.NewDecoder(r.Body).Decode(&req)
			require.NoError(t, err)
			require.Equal(t, "test-model", req.Model)
			require.Equal(t, "hello world", req.Input)

			resp := ollamaEmbedResponse{
				Embeddings: [][]float64{{0.1, 0.2, 0.3, 0.4, 0.5}},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewOllamaClient(server.URL, 30*time.Second, 120*time.Second)
		embedding, err := client.GenerateEmbedding(context.Background(), "test-model", "hello world")
		require.NoError(t, err)
		require.Equal(t, []float64{0.1, 0.2, 0.3, 0.4, 0.5}, embedding)
	})

	t.Run("model_not_found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`model "bad-model" not found`))
		}))
		defer server.Close()

		client := NewOllamaClient(server.URL, 30*time.Second, 120*time.Second)
		_, err := client.GenerateEmbedding(context.Background(), "bad-model", "hello")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrModelNotFound)
	})

	t.Run("server_down", func(t *testing.T) {
		client := NewOllamaClient("http://127.0.0.1:1", 2*time.Second, 2*time.Second)
		_, err := client.GenerateEmbedding(context.Background(), "model", "hello")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrOllamaUnavailable)
	})

	t.Run("empty_embedding", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := ollamaEmbedResponse{Embeddings: [][]float64{}}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewOllamaClient(server.URL, 30*time.Second, 120*time.Second)
		_, err := client.GenerateEmbedding(context.Background(), "model", "hello")
		require.Error(t, err)
		require.Contains(t, err.Error(), "empty embedding")
	})

	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(500 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewOllamaClient(server.URL, 100*time.Millisecond, 100*time.Millisecond)
		_, err := client.GenerateEmbedding(context.Background(), "model", "hello")
		require.Error(t, err)
	})
}

func TestOllamaClient_ChatCompletion(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/api/chat", r.URL.Path)

			var req ollamaChatRequest
			err := json.NewDecoder(r.Body).Decode(&req)
			require.NoError(t, err)
			require.False(t, req.Stream)
			require.Equal(t, "test-model", req.Model)
			require.Len(t, req.Messages, 1)

			resp := ollamaChatResponse{Done: true}
			resp.Message.Role = "assistant"
			resp.Message.Content = "I am an AI assistant."
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewOllamaClient(server.URL, 30*time.Second, 120*time.Second)
		resp, err := client.ChatCompletion(context.Background(), "test-model", []ChatMessage{
			{Role: "user", Content: "Hello"},
		})
		require.NoError(t, err)
		require.Equal(t, "I am an AI assistant.", resp.Content)
	})

	t.Run("server_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal error"))
		}))
		defer server.Close()

		client := NewOllamaClient(server.URL, 30*time.Second, 120*time.Second)
		_, err := client.ChatCompletion(context.Background(), "model", []ChatMessage{
			{Role: "user", Content: "Hello"},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "500")
	})
}

func TestOllamaClient_ChatCompletionStream(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/api/chat", r.URL.Path)

			var req ollamaChatRequest
			err := json.NewDecoder(r.Body).Decode(&req)
			require.NoError(t, err)
			require.True(t, req.Stream)

			flusher, ok := w.(http.Flusher)
			require.True(t, ok)

			tokens := []string{"Hello", " ", "world", "!"}
			for _, token := range tokens {
				chunk := ollamaChatResponse{Done: false}
				chunk.Message.Role = "assistant"
				chunk.Message.Content = token
				data, _ := json.Marshal(chunk)
				fmt.Fprintf(w, "%s\n", data)
				flusher.Flush()
			}

			// Final done message.
			final := ollamaChatResponse{Done: true}
			final.Message.Role = "assistant"
			data, _ := json.Marshal(final)
			fmt.Fprintf(w, "%s\n", data)
			flusher.Flush()
		}))
		defer server.Close()

		client := NewOllamaClient(server.URL, 30*time.Second, 120*time.Second)
		tokenCh, errCh := client.ChatCompletionStream(context.Background(), "test-model", []ChatMessage{
			{Role: "user", Content: "Hello"},
		})

		var collected []string
		for token := range tokenCh {
			collected = append(collected, token)
		}

		// Check for errors.
		for err := range errCh {
			require.NoError(t, err)
		}

		require.Equal(t, []string{"Hello", " ", "world", "!"}, collected)
	})

	t.Run("context_cancelled", func(t *testing.T) {
		started := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			// Write headers so the client gets a response, then block.
			flusher, _ := w.(http.Flusher)
			w.WriteHeader(http.StatusOK)
			flusher.Flush()
			// Block until client disconnects.
			<-r.Context().Done()
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())

		client := NewOllamaClient(server.URL, 30*time.Second, 5*time.Second)
		tokenCh, errCh := client.ChatCompletionStream(ctx, "model", []ChatMessage{
			{Role: "user", Content: "Hello"},
		})

		// Wait for server to start responding, then cancel.
		<-started
		cancel()

		// Drain channels.
		for range tokenCh {
		}
		for range errCh {
		}
	})
}

func TestOllamaClient_ChatCompletionWithToolsStream_TextOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/chat", r.URL.Path)

		var req ollamaToolChatRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		require.True(t, req.Stream)

		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		w.Header().Set("Content-Type", "application/x-ndjson")

		lines := []string{
			`{"message":{"role":"assistant","content":"Hel"},"done":false}`,
			`{"message":{"role":"assistant","content":"lo"},"done":false}`,
			`{"message":{"role":"assistant","content":" world"},"done":false}`,
			`{"message":{"role":"assistant","content":""},"done":true,"prompt_eval_count":5,"eval_count":3}`,
		}
		for _, l := range lines {
			fmt.Fprintln(w, l)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, 30*time.Second, 30*time.Second)
	deltaCh, errCh := client.ChatCompletionWithToolsStream(context.Background(), "test-model", []ToolMessage{
		{Role: "user", Content: "hi"},
	}, nil)

	var textDeltas []string
	var final *ToolChatResponse
	for d := range deltaCh {
		if d.Final != nil {
			final = d.Final
			continue
		}
		if d.TextDelta != "" {
			textDeltas = append(textDeltas, d.TextDelta)
		}
	}
	require.NoError(t, <-errCh)

	require.Equal(t, []string{"Hel", "lo", " world"}, textDeltas)
	require.NotNil(t, final)
	require.Equal(t, "Hello world", final.Content)
	require.Equal(t, "stop", final.FinishReason)
	require.Empty(t, final.ToolCalls)
	require.NotNil(t, final.Usage)
	require.Equal(t, 5, final.Usage.InputTokens)
	require.Equal(t, 3, final.Usage.OutputTokens)
	require.Equal(t, 8, final.Usage.TotalTokens)
}

func TestOllamaClient_ChatCompletionWithToolsStream_ToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		w.Header().Set("Content-Type", "application/x-ndjson")

		// Ollama emits tool calls on the terminal frame with done=true.
		lines := []string{
			`{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"get_time","arguments":{"tz":"UTC"}}}]},"done":true,"prompt_eval_count":4,"eval_count":2}`,
		}
		for _, l := range lines {
			fmt.Fprintln(w, l)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, 30*time.Second, 30*time.Second)
	deltaCh, errCh := client.ChatCompletionWithToolsStream(context.Background(), "test-model", []ToolMessage{
		{Role: "user", Content: "time?"},
	}, []ToolDefinition{
		{Name: "get_time", Description: "time", Parameters: json.RawMessage(`{"type":"object"}`)},
	})

	var textCount int
	var final *ToolChatResponse
	for d := range deltaCh {
		if d.Final != nil {
			final = d.Final
			continue
		}
		if d.TextDelta != "" {
			textCount++
		}
	}
	require.NoError(t, <-errCh)

	require.Zero(t, textCount)
	require.NotNil(t, final)
	require.Equal(t, "tool_calls", final.FinishReason)
	require.Empty(t, final.Content)
	require.Len(t, final.ToolCalls, 1)
	require.Equal(t, "get_time", final.ToolCalls[0].Name)
	require.JSONEq(t, `{"tz":"UTC"}`, final.ToolCalls[0].Arguments)
}
