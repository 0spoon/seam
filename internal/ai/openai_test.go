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

func TestOpenAIClient_ChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req openaiChatRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "gpt-4o", req.Model)
		require.False(t, req.Stream)
		require.Len(t, req.Messages, 2)
		require.Equal(t, "system", req.Messages[0].Role)
		require.Equal(t, "user", req.Messages[1].Role)

		resp := openaiChatResponse{
			Choices: []openaiChatChoice{
				{
					Message: struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					}{
						Role:    "assistant",
						Content: "Hello from OpenAI!",
					},
					FinishReason: "stop",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOpenAIClient("test-key", server.URL, 30*time.Second)
	messages := []ChatMessage{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
	}

	resp, err := client.ChatCompletion(context.Background(), "gpt-4o", messages)
	require.NoError(t, err)
	require.Equal(t, "Hello from OpenAI!", resp.Content)
}

func TestOpenAIClient_ChatCompletion_EmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openaiChatResponse{Choices: []openaiChatChoice{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOpenAIClient("test-key", server.URL, 30*time.Second)
	_, err := client.ChatCompletion(context.Background(), "gpt-4o", []ChatMessage{
		{Role: "user", Content: "hello"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty response")
}

func TestOpenAIClient_ChatCompletion_AuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{
				"message": "invalid API key",
				"type":    "invalid_request_error",
			},
		})
	}))
	defer server.Close()

	client := NewOpenAIClient("bad-key", server.URL, 30*time.Second)
	_, err := client.ChatCompletion(context.Background(), "gpt-4o", []ChatMessage{
		{Role: "user", Content: "hello"},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrAuthFailed)
}

func TestOpenAIClient_ChatCompletion_ModelNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{
				"message": "model not found: nonexistent-model",
				"type":    "invalid_request_error",
			},
		})
	}))
	defer server.Close()

	client := NewOpenAIClient("test-key", server.URL, 30*time.Second)
	_, err := client.ChatCompletion(context.Background(), "nonexistent-model", []ChatMessage{
		{Role: "user", Content: "hello"},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrModelNotFound)
}

func TestOpenAIClient_ChatCompletion_RateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{
				"message": "rate limit exceeded",
				"type":    "rate_limit_error",
			},
		})
	}))
	defer server.Close()

	client := NewOpenAIClient("test-key", server.URL, 30*time.Second)
	_, err := client.ChatCompletion(context.Background(), "gpt-4o", []ChatMessage{
		{Role: "user", Content: "hello"},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRateLimited)
}

func TestOpenAIClient_ChatCompletionStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/chat/completions", r.URL.Path)

		var req openaiChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		require.True(t, req.Stream)

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)

		tokens := []string{"Hello", " from", " OpenAI", "!"}
		for _, token := range tokens {
			chunk := fmt.Sprintf(`{"choices":[{"delta":{"content":"%s"},"finish_reason":null}]}`, token)
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewOpenAIClient("test-key", server.URL, 30*time.Second)
	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
	}

	tokenCh, errCh := client.ChatCompletionStream(context.Background(), "gpt-4o", messages)

	var tokens []string
	for token := range tokenCh {
		tokens = append(tokens, token)
	}
	for err := range errCh {
		require.NoError(t, err)
	}

	require.Equal(t, []string{"Hello", " from", " OpenAI", "!"}, tokens)
}

func TestOpenAIClient_ChatCompletionWithToolsStream_TextOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/chat/completions", r.URL.Path)

		var req openaiToolChatStreamRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		require.True(t, req.Stream)
		require.True(t, req.StreamOptions.IncludeUsage)

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)

		chunks := []string{
			`{"choices":[{"delta":{"content":"Hel"},"finish_reason":null}]}`,
			`{"choices":[{"delta":{"content":"lo"},"finish_reason":null}]}`,
			`{"choices":[{"delta":{"content":" world"},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewOpenAIClient("test-key", server.URL, 30*time.Second)
	deltaCh, errCh := client.ChatCompletionWithToolsStream(context.Background(), "gpt-4o", []ToolMessage{
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
	require.Equal(t, 7, final.Usage.InputTokens)
	require.Equal(t, 3, final.Usage.OutputTokens)
	require.Equal(t, 10, final.Usage.TotalTokens)
}

func TestOpenAIClient_ChatCompletionWithToolsStream_ToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)

		// OpenAI streams tool calls as multiple delta chunks: the first
		// carries the id and function.name, subsequent chunks extend
		// function.arguments. Text stays empty throughout.
		chunks := []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_abc","function":{"name":"get_time"}}]},"finish_reason":null}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"tz\":"}}]},"finish_reason":null}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"UTC\"}"}}]},"finish_reason":"tool_calls"}]}`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewOpenAIClient("test-key", server.URL, 30*time.Second)
	deltaCh, errCh := client.ChatCompletionWithToolsStream(context.Background(), "gpt-4o", []ToolMessage{
		{Role: "user", Content: "what time is it"},
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

	require.Zero(t, textCount, "tool-only response should emit no text deltas")
	require.NotNil(t, final)
	require.Equal(t, "tool_calls", final.FinishReason)
	require.Empty(t, final.Content)
	require.Len(t, final.ToolCalls, 1)
	require.Equal(t, "call_abc", final.ToolCalls[0].ID)
	require.Equal(t, "get_time", final.ToolCalls[0].Name)
	require.Equal(t, `{"tz":"UTC"}`, final.ToolCalls[0].Arguments)
}

func TestOpenAIClient_ChatCompletionStream_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{
				"message": "internal server error",
			},
		})
	}))
	defer server.Close()

	client := NewOpenAIClient("test-key", server.URL, 30*time.Second)
	tokenCh, errCh := client.ChatCompletionStream(context.Background(), "gpt-4o", []ChatMessage{
		{Role: "user", Content: "hello"},
	})

	// Drain token channel.
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

func TestOpenAIClient_DefaultBaseURL(t *testing.T) {
	client := NewOpenAIClient("test-key", "", 30*time.Second)
	require.Equal(t, defaultOpenAIBaseURL, client.baseURL)
}

// Verify OpenAIClient satisfies ChatCompleter at compile time.
var _ ChatCompleter = (*OpenAIClient)(nil)
