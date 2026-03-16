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

// newTestAnthropicClient creates an AnthropicClient pointed at a test server.
func newTestAnthropicClient(apiKey, baseURL string) *AnthropicClient {
	c := NewAnthropicClient(apiKey, 30*time.Second, 0)
	c.baseURL = baseURL
	return c
}

func TestAnthropicClient_ConvertMessages(t *testing.T) {
	messages := []ChatMessage{
		{Role: "system", Content: "You are helpful."},
		{Role: "system", Content: "Be concise."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi!"},
		{Role: "user", Content: "How are you?"},
	}

	system, converted := convertMessages(messages)
	require.Equal(t, "You are helpful.\n\nBe concise.", system)
	require.Len(t, converted, 3)
	require.Equal(t, "user", converted[0].Role)
	require.Equal(t, "Hello", converted[0].Content)
	require.Equal(t, "assistant", converted[1].Role)
	require.Equal(t, "user", converted[2].Role)
}

func TestAnthropicClient_ConvertMessages_NoSystem(t *testing.T) {
	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
	}

	system, converted := convertMessages(messages)
	require.Equal(t, "", system)
	require.Len(t, converted, 1)
}

func TestAnthropicClient_ChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/messages", r.URL.Path)
		require.Equal(t, "test-key", r.Header.Get("x-api-key"))
		require.Equal(t, anthropicAPIVersion, r.Header.Get("anthropic-version"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req anthropicRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "claude-sonnet-4-20250514", req.Model)
		require.False(t, req.Stream)
		require.Equal(t, defaultMaxTokens, req.MaxTokens)
		// System message should be extracted to top-level field.
		require.Equal(t, "You are helpful.", req.System)
		require.Len(t, req.Messages, 1)
		require.Equal(t, "user", req.Messages[0].Role)

		resp := anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "Hello from Anthropic!"},
			},
			StopReason: "end_turn",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestAnthropicClient("test-key", server.URL)
	messages := []ChatMessage{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
	}

	resp, err := client.ChatCompletion(context.Background(), "claude-sonnet-4-20250514", messages)
	require.NoError(t, err)
	require.Equal(t, "Hello from Anthropic!", resp.Content)
}

func TestAnthropicClient_ChatCompletion_EmptyContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := anthropicResponse{Content: []anthropicContentBlock{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestAnthropicClient("test-key", server.URL)
	_, err := client.ChatCompletion(context.Background(), "claude-sonnet-4-20250514", []ChatMessage{
		{Role: "user", Content: "hello"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty response")
}

func TestAnthropicClient_ChatCompletion_AuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(anthropicErrorResponse{
			Type: "error",
			Error: &anthropicErrorDetail{
				Type:    "authentication_error",
				Message: "invalid x-api-key",
			},
		})
	}))
	defer server.Close()

	client := newTestAnthropicClient("bad-key", server.URL)
	_, err := client.ChatCompletion(context.Background(), "claude-sonnet-4-20250514", []ChatMessage{
		{Role: "user", Content: "hello"},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrAuthFailed)
}

func TestAnthropicClient_ChatCompletion_ModelNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(anthropicErrorResponse{
			Type: "error",
			Error: &anthropicErrorDetail{
				Type:    "not_found_error",
				Message: "model not found",
			},
		})
	}))
	defer server.Close()

	client := newTestAnthropicClient("test-key", server.URL)
	_, err := client.ChatCompletion(context.Background(), "nonexistent", []ChatMessage{
		{Role: "user", Content: "hello"},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrModelNotFound)
}

func TestAnthropicClient_ChatCompletion_RateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(anthropicErrorResponse{
			Type: "error",
			Error: &anthropicErrorDetail{
				Type:    "rate_limit_error",
				Message: "rate limit exceeded",
			},
		})
	}))
	defer server.Close()

	client := newTestAnthropicClient("test-key", server.URL)
	_, err := client.ChatCompletion(context.Background(), "claude-sonnet-4-20250514", []ChatMessage{
		{Role: "user", Content: "hello"},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRateLimited)
}

func TestAnthropicClient_ChatCompletionStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/messages", r.URL.Path)
		require.Equal(t, "test-key", r.Header.Get("x-api-key"))

		var req anthropicRequest
		json.NewDecoder(r.Body).Decode(&req)
		require.True(t, req.Stream)

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)

		// message_start
		fmt.Fprintf(w, "event: message_start\n")
		fmt.Fprintf(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"role\":\"assistant\"}}\n\n")
		flusher.Flush()

		// content_block_start
		fmt.Fprintf(w, "event: content_block_start\n")
		fmt.Fprintf(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		flusher.Flush()

		// content_block_delta (tokens)
		tokens := []string{"Hello", " from", " Anthropic", "!"}
		for _, token := range tokens {
			fmt.Fprintf(w, "event: content_block_delta\n")
			deltaJSON, _ := json.Marshal(map[string]interface{}{
				"type": "content_block_delta",
				"delta": map[string]string{
					"type": "text_delta",
					"text": token,
				},
			})
			fmt.Fprintf(w, "data: %s\n\n", deltaJSON)
			flusher.Flush()
		}

		// content_block_stop
		fmt.Fprintf(w, "event: content_block_stop\n")
		fmt.Fprintf(w, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		flusher.Flush()

		// message_delta
		fmt.Fprintf(w, "event: message_delta\n")
		fmt.Fprintf(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n")
		flusher.Flush()

		// message_stop
		fmt.Fprintf(w, "event: message_stop\n")
		fmt.Fprintf(w, "data: {\"type\":\"message_stop\"}\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := newTestAnthropicClient("test-key", server.URL)
	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
	}

	tokenCh, errCh := client.ChatCompletionStream(context.Background(), "claude-sonnet-4-20250514", messages)

	var tokens []string
	for token := range tokenCh {
		tokens = append(tokens, token)
	}
	for err := range errCh {
		require.NoError(t, err)
	}

	require.Equal(t, []string{"Hello", " from", " Anthropic", "!"}, tokens)
}

func TestAnthropicClient_ChatCompletionStream_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(anthropicErrorResponse{
			Type: "error",
			Error: &anthropicErrorDetail{
				Type:    "api_error",
				Message: "internal server error",
			},
		})
	}))
	defer server.Close()

	client := newTestAnthropicClient("test-key", server.URL)
	tokenCh, errCh := client.ChatCompletionStream(context.Background(), "claude-sonnet-4-20250514", []ChatMessage{
		{Role: "user", Content: "hello"},
	})

	for range tokenCh {
	}
	var gotErr error
	for err := range errCh {
		if err != nil {
			gotErr = err
		}
	}
	require.Error(t, gotErr)
	require.Contains(t, gotErr.Error(), "API error")
}

func TestAnthropicClient_MultipleTextBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "First block. "},
				{Type: "text", Text: "Second block."},
			},
			StopReason: "end_turn",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestAnthropicClient("test-key", server.URL)
	resp, err := client.ChatCompletion(context.Background(), "claude-sonnet-4-20250514", []ChatMessage{
		{Role: "user", Content: "hello"},
	})
	require.NoError(t, err)
	require.Equal(t, "First block. Second block.", resp.Content)
}

// Verify AnthropicClient satisfies ChatCompleter at compile time.
var _ ChatCompleter = (*AnthropicClient)(nil)
