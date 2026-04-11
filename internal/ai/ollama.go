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

// TokenUsage holds token consumption counts returned by LLM providers.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// ChatResponse represents a complete (non-streaming) chat response.
type ChatResponse struct {
	Content string      `json:"content"`
	Usage   *TokenUsage `json:"usage,omitempty"`
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

// ollamaToolChatRequest is the request body with tool definitions.
type ollamaToolChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaToolMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	Tools    []ollamaTool        `json:"tools,omitempty"`
}

// ollamaTool is an Ollama function tool definition.
type ollamaTool struct {
	Type     string             `json:"type"` // "function"
	Function ollamaToolFunction `json:"function"`
}

// ollamaToolFunction describes a function the model can call.
type ollamaToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ollamaToolMessage is a message in the Ollama format with tool support.
type ollamaToolMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

// ollamaToolCall represents a tool call in Ollama's response.
type ollamaToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

// ollamaChatResponse is a single response object from POST /api/chat.
type ollamaChatResponse struct {
	Message struct {
		Role      string           `json:"role"`
		Content   string           `json:"content"`
		ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	} `json:"message"`
	Done            bool `json:"done"`
	PromptEvalCount int  `json:"prompt_eval_count,omitempty"`
	EvalCount       int  `json:"eval_count,omitempty"`
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

	resp2 := &ChatResponse{Content: result.Message.Content}
	if result.PromptEvalCount > 0 || result.EvalCount > 0 {
		resp2.Usage = &TokenUsage{
			InputTokens:  result.PromptEvalCount,
			OutputTokens: result.EvalCount,
			TotalTokens:  result.PromptEvalCount + result.EvalCount,
		}
	}
	return resp2, nil
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

// ChatCompletionWithTools sends messages with tool definitions to the Ollama
// chat endpoint and returns a response that may contain tool calls.
func (c *OllamaClient) ChatCompletionWithTools(ctx context.Context, model string, messages []ToolMessage, tools []ToolDefinition) (*ToolChatResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.chatTimeout)
	defer cancel()

	// Convert tool definitions to Ollama format.
	var ollamaTools []ollamaTool
	for _, t := range tools {
		ollamaTools = append(ollamaTools, ollamaTool{
			Type:     "function",
			Function: ollamaToolFunction(t),
		})
	}

	// Convert messages to Ollama format.
	var ollamaMessages []ollamaToolMessage
	for _, m := range messages {
		msg := ollamaToolMessage{
			Role:    m.Role,
			Content: m.Content,
		}
		for _, tc := range m.ToolCalls {
			argsRaw := json.RawMessage(tc.Arguments)
			msg.ToolCalls = append(msg.ToolCalls, ollamaToolCall{
				Function: struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}{
					Name:      tc.Name,
					Arguments: argsRaw,
				},
			})
		}
		ollamaMessages = append(ollamaMessages, msg)
	}

	reqBody := ollamaToolChatRequest{
		Model:    model,
		Messages: ollamaMessages,
		Stream:   false,
		Tools:    ollamaTools,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ai.OllamaClient.ChatCompletionWithTools: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ai.OllamaClient.ChatCompletionWithTools: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai.OllamaClient.ChatCompletionWithTools: %w: %w", ErrOllamaUnavailable, err)
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return nil, fmt.Errorf("ai.OllamaClient.ChatCompletionWithTools: %w", err)
	}

	var result ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ai.OllamaClient.ChatCompletionWithTools: decode: %w", err)
	}

	tcr := &ToolChatResponse{
		Content:      result.Message.Content,
		FinishReason: "stop",
	}
	if result.PromptEvalCount > 0 || result.EvalCount > 0 {
		tcr.Usage = &TokenUsage{
			InputTokens:  result.PromptEvalCount,
			OutputTokens: result.EvalCount,
			TotalTokens:  result.PromptEvalCount + result.EvalCount,
		}
	}

	if len(result.Message.ToolCalls) > 0 {
		tcr.FinishReason = "tool_calls"
		for _, tc := range result.Message.ToolCalls {
			argsJSON, marshalErr := json.Marshal(tc.Function.Arguments)
			if marshalErr != nil {
				return nil, fmt.Errorf("ai.OllamaClient.ChatCompletionWithTools: marshal tool args for %s: %w", tc.Function.Name, marshalErr)
			}
			tcr.ToolCalls = append(tcr.ToolCalls, ToolCall{
				ID:        fmt.Sprintf("call_%s_%d", tc.Function.Name, len(tcr.ToolCalls)),
				Name:      tc.Function.Name,
				Arguments: string(argsJSON),
			})
		}
	}

	return tcr, nil
}

// ChatCompletionWithToolsStream streams an Ollama tool-chat as NDJSON.
// Each response line carries a partial message.content chunk; the final
// line has done=true and may carry accumulated tool_calls. The returned
// channel emits a text-delta frame for every non-empty content chunk,
// then exactly one Final frame with the accumulated content, tool calls,
// and usage before closing.
//
// Ollama delivers tool calls only on the terminal frame in practice,
// so we collect them there; if a future Ollama release interleaves
// tool_calls into earlier frames we still accumulate by overwrite-wins.
func (c *OllamaClient) ChatCompletionWithToolsStream(ctx context.Context, model string, messages []ToolMessage, tools []ToolDefinition) (<-chan ToolChatDelta, <-chan error) {
	deltaCh := make(chan ToolChatDelta, 64)
	errCh := make(chan error, 1)

	go func() {
		defer close(deltaCh)
		defer close(errCh)

		ctx, cancel := context.WithTimeout(ctx, c.chatTimeout)
		defer cancel()

		var ollamaTools []ollamaTool
		for _, t := range tools {
			ollamaTools = append(ollamaTools, ollamaTool{
				Type:     "function",
				Function: ollamaToolFunction(t),
			})
		}

		var ollamaMessages []ollamaToolMessage
		for _, m := range messages {
			msg := ollamaToolMessage{
				Role:    m.Role,
				Content: m.Content,
			}
			for _, tc := range m.ToolCalls {
				argsRaw := json.RawMessage(tc.Arguments)
				msg.ToolCalls = append(msg.ToolCalls, ollamaToolCall{
					Function: struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}{
						Name:      tc.Name,
						Arguments: argsRaw,
					},
				})
			}
			ollamaMessages = append(ollamaMessages, msg)
		}

		reqBody := ollamaToolChatRequest{
			Model:    model,
			Messages: ollamaMessages,
			Stream:   true,
			Tools:    ollamaTools,
		}

		body, err := json.Marshal(reqBody)
		if err != nil {
			errCh <- fmt.Errorf("ai.OllamaClient.ChatCompletionWithToolsStream: marshal: %w", err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
		if err != nil {
			errCh <- fmt.Errorf("ai.OllamaClient.ChatCompletionWithToolsStream: new request: %w", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			errCh <- fmt.Errorf("ai.OllamaClient.ChatCompletionWithToolsStream: %w: %w", ErrOllamaUnavailable, err)
			return
		}
		defer resp.Body.Close()

		if err := c.checkResponse(resp); err != nil {
			errCh <- fmt.Errorf("ai.OllamaClient.ChatCompletionWithToolsStream: %w", err)
			return
		}

		var (
			contentBuf    bytes.Buffer
			finalToolCalls []ollamaToolCall
			promptEval     int
			evalCount      int
		)

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var chunk ollamaChatResponse
			if err := json.Unmarshal(line, &chunk); err != nil {
				slog.Warn("ai.OllamaClient.ChatCompletionWithToolsStream: malformed JSON line",
					"error", err, "line", string(line[:min(len(line), 200)]))
				continue
			}

			if chunk.Message.Content != "" {
				contentBuf.WriteString(chunk.Message.Content)
				select {
				case deltaCh <- ToolChatDelta{TextDelta: chunk.Message.Content}:
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				}
			}
			if len(chunk.Message.ToolCalls) > 0 {
				finalToolCalls = chunk.Message.ToolCalls
			}

			if chunk.Done {
				if chunk.PromptEvalCount > 0 {
					promptEval = chunk.PromptEvalCount
				}
				if chunk.EvalCount > 0 {
					evalCount = chunk.EvalCount
				}
				break
			}
		}
		if err := scanner.Err(); err != nil {
			errCh <- fmt.Errorf("ai.OllamaClient.ChatCompletionWithToolsStream: scan: %w", err)
			return
		}

		final := &ToolChatResponse{
			Content:      contentBuf.String(),
			FinishReason: "stop",
		}
		if promptEval > 0 || evalCount > 0 {
			final.Usage = &TokenUsage{
				InputTokens:  promptEval,
				OutputTokens: evalCount,
				TotalTokens:  promptEval + evalCount,
			}
		}
		if len(finalToolCalls) > 0 {
			final.FinishReason = "tool_calls"
			for _, tc := range finalToolCalls {
				argsJSON, marshalErr := json.Marshal(tc.Function.Arguments)
				if marshalErr != nil {
					errCh <- fmt.Errorf("ai.OllamaClient.ChatCompletionWithToolsStream: marshal tool args for %s: %w", tc.Function.Name, marshalErr)
					return
				}
				final.ToolCalls = append(final.ToolCalls, ToolCall{
					ID:        fmt.Sprintf("call_%s_%d", tc.Function.Name, len(final.ToolCalls)),
					Name:      tc.Function.Name,
					Arguments: string(argsJSON),
				})
			}
		}

		select {
		case deltaCh <- ToolChatDelta{Final: final}:
		case <-ctx.Done():
			errCh <- ctx.Err()
			return
		}
	}()

	return deltaCh, errCh
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
