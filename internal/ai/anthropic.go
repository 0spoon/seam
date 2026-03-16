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

const (
	anthropicBaseURL    = "https://api.anthropic.com/v1"
	anthropicAPIVersion = "2023-06-01"
	// defaultMaxTokens is the default max_tokens for Anthropic requests.
	// Anthropic requires this field; 4096 is a reasonable default for most tasks.
	defaultMaxTokens = 4096
)

// AnthropicClient implements ChatCompleter using the Anthropic Messages API.
type AnthropicClient struct {
	baseURL     string
	apiKey      string
	httpClient  *http.Client
	chatTimeout time.Duration
	maxTokens   int
}

// NewAnthropicClient creates a new Anthropic API client.
func NewAnthropicClient(apiKey string, chatTimeout time.Duration) *AnthropicClient {
	return &AnthropicClient{
		baseURL: anthropicBaseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
		chatTimeout: chatTimeout,
		maxTokens:   defaultMaxTokens,
	}
}

// anthropicMessage is a message in the Anthropic Messages API format.
// Anthropic does not allow "system" role in messages; system content
// goes in the top-level "system" field instead.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicRequest is the request body for POST /v1/messages.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Stream    bool               `json:"stream"`
}

// anthropicContentBlock is a content block in the response.
type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// anthropicResponse is the response from POST /v1/messages.
type anthropicResponse struct {
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Error      *anthropicErrorDetail   `json:"error,omitempty"`
}

// anthropicErrorDetail holds API error details.
type anthropicErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// anthropicErrorResponse is the top-level error envelope.
type anthropicErrorResponse struct {
	Type  string                `json:"type"`
	Error *anthropicErrorDetail `json:"error"`
}

// convertMessages splits ChatMessage slice into an Anthropic-compatible
// (system, messages) pair. System messages are extracted and concatenated
// into a single system string; all other messages become anthropicMessage.
func convertMessages(messages []ChatMessage) (string, []anthropicMessage) {
	var systemParts []string
	var converted []anthropicMessage

	for _, m := range messages {
		if m.Role == "system" {
			systemParts = append(systemParts, m.Content)
			continue
		}
		converted = append(converted, anthropicMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	system := strings.Join(systemParts, "\n\n")
	return system, converted
}

// ChatCompletion sends messages to the Anthropic Messages endpoint and
// returns a complete response.
func (c *AnthropicClient) ChatCompletion(ctx context.Context, model string, messages []ChatMessage) (*ChatResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.chatTimeout)
	defer cancel()

	system, converted := convertMessages(messages)

	reqBody := anthropicRequest{
		Model:     model,
		MaxTokens: c.maxTokens,
		System:    system,
		Messages:  converted,
		Stream:    false,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ai.AnthropicClient.ChatCompletion: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ai.AnthropicClient.ChatCompletion: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai.AnthropicClient.ChatCompletion: request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return nil, fmt.Errorf("ai.AnthropicClient.ChatCompletion: %w", err)
	}

	var result anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ai.AnthropicClient.ChatCompletion: decode: %w", err)
	}

	// Extract text from content blocks.
	var textParts []string
	for _, block := range result.Content {
		if block.Type == "text" {
			textParts = append(textParts, block.Text)
		}
	}
	if len(textParts) == 0 {
		return nil, fmt.Errorf("ai.AnthropicClient.ChatCompletion: empty response (no text content)")
	}

	return &ChatResponse{Content: strings.Join(textParts, "")}, nil
}

// ChatCompletionStream sends messages to the Anthropic Messages endpoint with
// streaming enabled. Returns a channel that yields tokens as they arrive.
//
// Anthropic SSE events:
//   - message_start: contains the message metadata
//   - content_block_start: begins a content block
//   - content_block_delta: contains incremental text (type: "text_delta")
//   - content_block_stop: ends a content block
//   - message_delta: contains stop_reason
//   - message_stop: signals end of stream
func (c *AnthropicClient) ChatCompletionStream(ctx context.Context, model string, messages []ChatMessage) (<-chan string, <-chan error) {
	tokenCh := make(chan string, 64)
	errCh := make(chan error, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)

		ctx, cancel := context.WithTimeout(ctx, c.chatTimeout)
		defer cancel()

		system, converted := convertMessages(messages)

		reqBody := anthropicRequest{
			Model:     model,
			MaxTokens: c.maxTokens,
			System:    system,
			Messages:  converted,
			Stream:    true,
		}

		body, err := json.Marshal(reqBody)
		if err != nil {
			errCh <- fmt.Errorf("ai.AnthropicClient.ChatCompletionStream: marshal: %w", err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(body))
		if err != nil {
			errCh <- fmt.Errorf("ai.AnthropicClient.ChatCompletionStream: new request: %w", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", c.apiKey)
		req.Header.Set("anthropic-version", anthropicAPIVersion)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			errCh <- fmt.Errorf("ai.AnthropicClient.ChatCompletionStream: request failed: %w", err)
			return
		}
		defer resp.Body.Close()

		if err := c.checkResponse(resp); err != nil {
			errCh <- fmt.Errorf("ai.AnthropicClient.ChatCompletionStream: %w", err)
			return
		}

		// Parse SSE events. Anthropic uses "event:" and "data:" lines.
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

		var currentEvent string
		for scanner.Scan() {
			line := scanner.Text()

			if strings.HasPrefix(line, "event: ") {
				currentEvent = strings.TrimPrefix(line, "event: ")
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			switch currentEvent {
			case "content_block_delta":
				var delta struct {
					Delta struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"delta"`
				}
				if err := json.Unmarshal([]byte(data), &delta); err != nil {
					slog.Warn("ai.AnthropicClient.ChatCompletionStream: malformed delta",
						"error", err, "data", data[:min(len(data), 200)])
					continue
				}
				if delta.Delta.Type == "text_delta" && delta.Delta.Text != "" {
					select {
					case tokenCh <- delta.Delta.Text:
					case <-ctx.Done():
						errCh <- ctx.Err()
						return
					}
				}

			case "message_stop":
				return

			case "error":
				var errEvt struct {
					Error struct {
						Type    string `json:"type"`
						Message string `json:"message"`
					} `json:"error"`
				}
				if err := json.Unmarshal([]byte(data), &errEvt); err == nil {
					errCh <- fmt.Errorf("ai.AnthropicClient.ChatCompletionStream: API error: %s", errEvt.Error.Message)
				}
				return
			}
		}
		if err := scanner.Err(); err != nil {
			errCh <- fmt.Errorf("ai.AnthropicClient.ChatCompletionStream: scan: %w", err)
		}
	}()

	return tokenCh, errCh
}

// checkResponse handles non-2xx responses from the Anthropic API.
func (c *AnthropicClient) checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	// Try to parse structured error.
	var errResp anthropicErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != nil {
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return fmt.Errorf("authentication failed: %s", errResp.Error.Message)
		case http.StatusNotFound:
			return fmt.Errorf("%w: %s", ErrModelNotFound, errResp.Error.Message)
		case http.StatusTooManyRequests:
			return fmt.Errorf("rate limited: %s", errResp.Error.Message)
		default:
			return fmt.Errorf("API error (status %d): %s", resp.StatusCode, errResp.Error.Message)
		}
	}

	return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
}
