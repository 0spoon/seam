// Package ai provides Ollama and ChromaDB clients, an AI task queue,
// and higher-level AI features (embeddings, RAG chat, synthesis, auto-link).
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Domain errors.
var (
	ErrOllamaUnavailable = errors.New("ollama server unavailable")
	ErrModelNotFound     = errors.New("model not found")
)

// ChatMessage represents a message in a chat conversation.
type ChatMessage struct {
	Role    string `json:"role"` // "system", "user", "assistant"
	Content string `json:"content"`
}

// ChatResponse represents a complete (non-streaming) chat response.
type ChatResponse struct {
	Content string `json:"content"`
}

// OllamaClient is an HTTP client for the Ollama REST API.
type OllamaClient struct {
	baseURL          string
	httpClient       *http.Client
	embeddingTimeout time.Duration
	chatTimeout      time.Duration
}

// NewOllamaClient creates a new Ollama API client.
func NewOllamaClient(baseURL string, embeddingTimeout, chatTimeout time.Duration) *OllamaClient {
	return &OllamaClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			// Fallback timeout prevents indefinite hangs if a caller
			// forgets to set a context deadline. Per-request context
			// timeouts override this for normal operations.
			Timeout: 10 * time.Minute,
		},
		embeddingTimeout: embeddingTimeout,
		chatTimeout:      chatTimeout,
	}
}

// ollamaEmbedRequest is the request body for POST /api/embed.
type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// ollamaEmbedResponse is the response body from POST /api/embed.
type ollamaEmbedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

// GenerateEmbedding generates an embedding vector for the given text.
func (c *OllamaClient) GenerateEmbedding(ctx context.Context, model, text string) ([]float64, error) {
	ctx, cancel := context.WithTimeout(ctx, c.embeddingTimeout)
	defer cancel()

	reqBody := ollamaEmbedRequest{
		Model: model,
		Input: text,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ai.OllamaClient.GenerateEmbedding: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ai.OllamaClient.GenerateEmbedding: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai.OllamaClient.GenerateEmbedding: %w: %w", ErrOllamaUnavailable, err)
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return nil, fmt.Errorf("ai.OllamaClient.GenerateEmbedding: %w", err)
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ai.OllamaClient.GenerateEmbedding: decode: %w", err)
	}

	if len(result.Embeddings) == 0 || len(result.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("ai.OllamaClient.GenerateEmbedding: empty embedding returned")
	}

	return result.Embeddings[0], nil
}

// ollamaChatRequest is the request body for POST /api/chat.
type ollamaChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// ollamaChatResponse is a single response object from POST /api/chat.
type ollamaChatResponse struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

// ChatCompletion sends messages to the chat endpoint and returns a complete response.
func (c *OllamaClient) ChatCompletion(ctx context.Context, model string, messages []ChatMessage) (*ChatResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.chatTimeout)
	defer cancel()

	reqBody := ollamaChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ai.OllamaClient.ChatCompletion: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ai.OllamaClient.ChatCompletion: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai.OllamaClient.ChatCompletion: %w: %w", ErrOllamaUnavailable, err)
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return nil, fmt.Errorf("ai.OllamaClient.ChatCompletion: %w", err)
	}

	var result ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ai.OllamaClient.ChatCompletion: decode: %w", err)
	}

	return &ChatResponse{Content: result.Message.Content}, nil
}

// ChatCompletionStream sends messages to the chat endpoint with streaming enabled.
// Returns a channel that yields tokens as they arrive. The channel is closed when
// the response is complete or an error occurs.
func (c *OllamaClient) ChatCompletionStream(ctx context.Context, model string, messages []ChatMessage) (<-chan string, <-chan error) {
	tokenCh := make(chan string, 64)
	errCh := make(chan error, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)

		ctx, cancel := context.WithTimeout(ctx, c.chatTimeout)
		defer cancel()

		reqBody := ollamaChatRequest{
			Model:    model,
			Messages: messages,
			Stream:   true,
		}

		body, err := json.Marshal(reqBody)
		if err != nil {
			errCh <- fmt.Errorf("ai.OllamaClient.ChatCompletionStream: marshal: %w", err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
		if err != nil {
			errCh <- fmt.Errorf("ai.OllamaClient.ChatCompletionStream: new request: %w", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			errCh <- fmt.Errorf("ai.OllamaClient.ChatCompletionStream: %w: %w", ErrOllamaUnavailable, err)
			return
		}
		defer resp.Body.Close()

		if err := c.checkResponse(resp); err != nil {
			errCh <- fmt.Errorf("ai.OllamaClient.ChatCompletionStream: %w", err)
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		// Increase buffer size for potentially large JSON lines.
		scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var chunk ollamaChatResponse
			if err := json.Unmarshal(line, &chunk); err != nil {
				slog.Warn("ai.OllamaClient.ChatCompletionStream: malformed JSON line",
					"error", err, "line", string(line[:min(len(line), 200)]))
				continue
			}
			if chunk.Message.Content != "" {
				select {
				case tokenCh <- chunk.Message.Content:
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				}
			}
			if chunk.Done {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			errCh <- fmt.Errorf("ai.OllamaClient.ChatCompletionStream: scan: %w", err)
		}
	}()

	return tokenCh, errCh
}

// checkResponse handles non-2xx responses from Ollama.
func (c *OllamaClient) checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	msg := string(body)

	// Log full error for debugging; return sanitized error to avoid leaking
	// raw Ollama response messages to callers.
	slog.Debug("ai.OllamaClient: error response",
		"status", resp.StatusCode, "body", msg)
	switch resp.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%w: model not found", ErrModelNotFound)
	default:
		return fmt.Errorf("Ollama returned status %d", resp.StatusCode)
	}
}
