package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const defaultOpenAIBaseURL = "https://api.openai.com/v1"

// OpenAIClient implements ChatCompleter using the OpenAI Chat Completions API.
// Compatible with OpenAI, Azure OpenAI, and any OpenAI-compatible endpoint.
type OpenAIClient struct {
	baseURL     string
	apiKey      string
	httpClient  *http.Client
	chatTimeout time.Duration
}

// NewOpenAIClient creates a new OpenAI API client. If baseURL is empty,
// the default OpenAI endpoint is used.
func NewOpenAIClient(apiKey, baseURL string, chatTimeout time.Duration) *OpenAIClient {
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	return &OpenAIClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
		chatTimeout: chatTimeout,
	}
}

// openaiChatRequest is the request body for POST /chat/completions.
type openaiChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// openaiChatChoice is a single choice in the response.
type openaiChatChoice struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
}

// openaiChatResponse is the response from POST /chat/completions.
type openaiChatResponse struct {
	Choices []openaiChatChoice `json:"choices"`
	Error   *openaiError       `json:"error,omitempty"`
}

// openaiError represents an error response from the API.
type openaiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// ChatCompletion sends messages to the OpenAI chat endpoint and returns
// a complete response.
func (c *OpenAIClient) ChatCompletion(ctx context.Context, model string, messages []ChatMessage) (*ChatResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.chatTimeout)
	defer cancel()

	reqBody := openaiChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ai.OpenAIClient.ChatCompletion: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ai.OpenAIClient.ChatCompletion: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai.OpenAIClient.ChatCompletion: request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return nil, fmt.Errorf("ai.OpenAIClient.ChatCompletion: %w", err)
	}

	var result openaiChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ai.OpenAIClient.ChatCompletion: decode: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("ai.OpenAIClient.ChatCompletion: empty response (no choices)")
	}

	return &ChatResponse{Content: result.Choices[0].Message.Content}, nil
}

// openaiStreamDelta holds the delta content in a streaming chunk.
type openaiStreamDelta struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// ChatCompletionStream sends messages to the OpenAI chat endpoint with
// streaming enabled. Returns a channel that yields tokens as they arrive.
func (c *OpenAIClient) ChatCompletionStream(ctx context.Context, model string, messages []ChatMessage) (<-chan string, <-chan error) {
	tokenCh := make(chan string, 64)
	errCh := make(chan error, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)

		ctx, cancel := context.WithTimeout(ctx, c.chatTimeout)
		defer cancel()

		reqBody := openaiChatRequest{
			Model:    model,
			Messages: messages,
			Stream:   true,
		}

		body, err := json.Marshal(reqBody)
		if err != nil {
			errCh <- fmt.Errorf("ai.OpenAIClient.ChatCompletionStream: marshal: %w", err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			errCh <- fmt.Errorf("ai.OpenAIClient.ChatCompletionStream: new request: %w", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			errCh <- fmt.Errorf("ai.OpenAIClient.ChatCompletionStream: request failed: %w", err)
			return
		}
		defer resp.Body.Close()

		if err := c.checkResponse(resp); err != nil {
			errCh <- fmt.Errorf("ai.OpenAIClient.ChatCompletionStream: %w", err)
			return
		}

		// OpenAI streams as SSE: lines prefixed with "data: ", terminated
		// by "data: [DONE]".
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}

			var chunk openaiStreamDelta
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				truncated := data
				if runes := []rune(data); len(runes) > 200 {
					truncated = string(runes[:200])
				}
				slog.Warn("ai.OpenAIClient.ChatCompletionStream: malformed JSON chunk",
					"error", err, "data", truncated)
				continue
			}
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				select {
				case tokenCh <- chunk.Choices[0].Delta.Content:
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				}
			}
		}
		if err := scanner.Err(); err != nil {
			errCh <- fmt.Errorf("ai.OpenAIClient.ChatCompletionStream: scan: %w", err)
		}
	}()

	return tokenCh, errCh
}

// checkResponse handles non-2xx responses from the OpenAI API.
func (c *OpenAIClient) checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	// Try to parse structured error.
	var errResp struct {
		Error *openaiError `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != nil {
		// Log the full error for debugging; return sanitized sentinel errors.
		slog.Debug("ai.OpenAIClient: API error",
			"status", resp.StatusCode, "message", errResp.Error.Message)
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return fmt.Errorf("%w: invalid API key", ErrAuthFailed)
		case http.StatusNotFound:
			return fmt.Errorf("%w: model not found", ErrModelNotFound)
		case http.StatusTooManyRequests:
			return fmt.Errorf("%w: try again later", ErrRateLimited)
		default:
			return fmt.Errorf("API error (status %d)", resp.StatusCode)
		}
	}

	return fmt.Errorf("unexpected status %d", resp.StatusCode)
}
