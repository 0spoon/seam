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

func TestOpenAIEmbedder_GenerateEmbedding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/embeddings", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req openaiEmbeddingRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "text-embedding-3-large", req.Model)
		require.Equal(t, "hello world", req.Input)
		// Dimensions field should be omitted (zero value -> omitempty).
		require.Equal(t, 0, req.Dimensions)

		// Make sure dimensions field was actually absent on the wire, not
		// just zero in our struct.
		var raw map[string]any
		body, _ := json.Marshal(req)
		require.NoError(t, json.Unmarshal(body, &raw))
		_, hasDims := raw["dimensions"]
		require.False(t, hasDims, "dimensions should be omitted when zero")

		resp := openaiEmbeddingResponse{
			Data: []openaiEmbeddingData{
				{Embedding: []float64{0.1, 0.2, 0.3}, Index: 0},
			},
			Model: "text-embedding-3-large",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := NewOpenAIEmbedder("test-key", server.URL, 0, 30*time.Second)
	vec, err := embedder.GenerateEmbedding(context.Background(), "text-embedding-3-large", "hello world")
	require.NoError(t, err)
	require.Equal(t, []float64{0.1, 0.2, 0.3}, vec)
}

func TestOpenAIEmbedder_GenerateEmbedding_WithDimensions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openaiEmbeddingRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, 1024, req.Dimensions)

		resp := openaiEmbeddingResponse{
			Data: []openaiEmbeddingData{
				{Embedding: make([]float64, 1024), Index: 0},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := NewOpenAIEmbedder("test-key", server.URL, 1024, 30*time.Second)
	vec, err := embedder.GenerateEmbedding(context.Background(), "text-embedding-3-large", "hi")
	require.NoError(t, err)
	require.Len(t, vec, 1024)
}

func TestOpenAIEmbedder_GenerateEmbedding_EmptyData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := openaiEmbeddingResponse{Data: []openaiEmbeddingData{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := NewOpenAIEmbedder("test-key", server.URL, 0, 30*time.Second)
	_, err := embedder.GenerateEmbedding(context.Background(), "text-embedding-3-large", "x")
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty response")
}

func TestOpenAIEmbedder_GenerateEmbedding_AuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"message": "invalid API key",
				"type":    "invalid_request_error",
			},
		})
	}))
	defer server.Close()

	embedder := NewOpenAIEmbedder("bad-key", server.URL, 0, 30*time.Second)
	_, err := embedder.GenerateEmbedding(context.Background(), "text-embedding-3-large", "x")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrAuthFailed)
}

func TestOpenAIEmbedder_GenerateEmbedding_ModelNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"message": "model not found",
				"type":    "invalid_request_error",
			},
		})
	}))
	defer server.Close()

	embedder := NewOpenAIEmbedder("test-key", server.URL, 0, 30*time.Second)
	_, err := embedder.GenerateEmbedding(context.Background(), "nonexistent", "x")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrModelNotFound)
}

func TestOpenAIEmbedder_GenerateEmbedding_RateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"message": "rate limit exceeded",
				"type":    "rate_limit_error",
			},
		})
	}))
	defer server.Close()

	embedder := NewOpenAIEmbedder("test-key", server.URL, 0, 30*time.Second)
	_, err := embedder.GenerateEmbedding(context.Background(), "text-embedding-3-large", "x")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRateLimited)
}

func TestOpenAIEmbedder_DefaultBaseURL(t *testing.T) {
	embedder := NewOpenAIEmbedder("test-key", "", 0, 30*time.Second)
	require.Equal(t, defaultOpenAIBaseURL, embedder.baseURL)
}
