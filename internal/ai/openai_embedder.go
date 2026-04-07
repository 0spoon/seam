package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// OpenAIEmbedder implements EmbeddingGenerator using the OpenAI Embeddings API.
// Compatible with OpenAI, Azure OpenAI, and any OpenAI-compatible endpoint
// that implements POST /v1/embeddings.
type OpenAIEmbedder struct {
	baseURL      string
	apiKey       string
	httpClient   *http.Client
	embedTimeout time.Duration
	dimensions   int // 0 = no truncation; otherwise sent in the request
}

// NewOpenAIEmbedder creates a new OpenAI embeddings client. If baseURL is empty,
// the default OpenAI endpoint is used. If dimensions is zero, the request omits
// the dimensions field and the model returns its native size.
func NewOpenAIEmbedder(apiKey, baseURL string, dimensions int, embedTimeout time.Duration) *OpenAIEmbedder {
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	if embedTimeout <= 0 {
		embedTimeout = 60 * time.Second
	}
	return &OpenAIEmbedder{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
		embedTimeout: embedTimeout,
		dimensions:   dimensions,
	}
}

// openaiEmbeddingRequest is the request body for POST /embeddings.
type openaiEmbeddingRequest struct {
	Input      string `json:"input"`
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions,omitempty"`
}

// openaiEmbeddingData is one entry in the response data array.
type openaiEmbeddingData struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

// openaiEmbeddingResponse is the response from POST /embeddings.
type openaiEmbeddingResponse struct {
	Data  []openaiEmbeddingData `json:"data"`
	Model string                `json:"model"`
	Error *openaiError          `json:"error,omitempty"`
}

// GenerateEmbedding sends text to the OpenAI embeddings endpoint and returns
// the resulting vector. The model argument is the OpenAI model name (e.g.
// "text-embedding-3-large").
func (c *OpenAIEmbedder) GenerateEmbedding(ctx context.Context, model, text string) ([]float64, error) {
	ctx, cancel := context.WithTimeout(ctx, c.embedTimeout)
	defer cancel()

	reqBody := openaiEmbeddingRequest{
		Input:      text,
		Model:      model,
		Dimensions: c.dimensions,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ai.OpenAIEmbedder.GenerateEmbedding: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ai.OpenAIEmbedder.GenerateEmbedding: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai.OpenAIEmbedder.GenerateEmbedding: request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return nil, fmt.Errorf("ai.OpenAIEmbedder.GenerateEmbedding: %w", err)
	}

	var result openaiEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ai.OpenAIEmbedder.GenerateEmbedding: decode: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("ai.OpenAIEmbedder.GenerateEmbedding: empty response (no data)")
	}

	return result.Data[0].Embedding, nil
}

// checkResponse handles non-2xx responses from the OpenAI API. Mirrors the
// pattern in OpenAIClient.checkResponse so the two clients agree on which
// upstream conditions map to which sentinel errors.
func (c *OpenAIEmbedder) checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	var errResp struct {
		Error *openaiError `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != nil {
		slog.Debug("ai.OpenAIEmbedder: API error",
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

// Verify OpenAIEmbedder satisfies EmbeddingGenerator at compile time.
var _ EmbeddingGenerator = (*OpenAIEmbedder)(nil)
