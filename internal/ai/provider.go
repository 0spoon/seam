package ai

import (
	"context"
	"errors"
)

// Provider-agnostic domain errors returned by ChatCompleter implementations.
// Handlers should use errors.Is() to map these to appropriate HTTP status codes.
var (
	ErrRateLimited = errors.New("rate limited by LLM provider")
	ErrAuthFailed  = errors.New("LLM provider authentication failed")
)

// EmbeddingGenerator generates embedding vectors from text.
// The only implementation is OllamaClient -- embeddings always run locally
// to keep ChromaDB vectors consistent and avoid per-token API costs.
type EmbeddingGenerator interface {
	GenerateEmbedding(ctx context.Context, model, text string) ([]float64, error)
}

// ChatCompleter performs LLM chat completions.
// Implementations: OllamaClient (local), OpenAIClient, AnthropicClient.
type ChatCompleter interface {
	ChatCompletion(ctx context.Context, model string, messages []ChatMessage) (*ChatResponse, error)
	ChatCompletionStream(ctx context.Context, model string, messages []ChatMessage) (<-chan string, <-chan error)
}
