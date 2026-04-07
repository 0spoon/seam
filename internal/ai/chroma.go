package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Domain errors for ChromaDB operations.
var ErrChromaUnavailable = errors.New("chromadb server unavailable")

// QueryResult represents a single result from a ChromaDB vector query.
type QueryResult struct {
	ID       string            `json:"id"`
	Distance float64           `json:"distance"`
	Metadata map[string]string `json:"metadata"`
}

// ChromaConfig holds configurable options for the ChromaDB client.
type ChromaConfig struct {
	Tenant   string        // ChromaDB tenant (default: "default_tenant")
	Database string        // ChromaDB database (default: "default_database")
	Timeout  time.Duration // HTTP client timeout (default: 30s)
}

// ChromaClient is an HTTP client for the ChromaDB REST API v2.
type ChromaClient struct {
	baseURL    string
	tenant     string
	database   string
	httpClient *http.Client
}

// NewChromaClient creates a new ChromaDB API client. The timeout parameter
// controls the HTTP client timeout; if zero, defaults to 30 seconds.
// Use NewChromaClientWithConfig for full configuration control.
func NewChromaClient(baseURL string, timeout ...time.Duration) *ChromaClient {
	cfg := ChromaConfig{}
	if len(timeout) > 0 {
		cfg.Timeout = timeout[0]
	}
	return NewChromaClientWithConfig(baseURL, cfg)
}

// NewChromaClientWithConfig creates a new ChromaDB API client with full
// configuration. Zero values in the config use defaults.
func NewChromaClientWithConfig(baseURL string, cfg ChromaConfig) *ChromaClient {
	if cfg.Tenant == "" {
		cfg.Tenant = "default_tenant"
	}
	if cfg.Database == "" {
		cfg.Database = "default_database"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &ChromaClient{
		baseURL:  baseURL,
		tenant:   cfg.Tenant,
		database: cfg.Database,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// ErrMismatchedArrayLengths is returned when ids, embeddings, and metadatas
// have different lengths in Add or Upsert calls.
var ErrMismatchedArrayLengths = errors.New("ids, embeddings, and metadatas must have matching lengths")

// collectionPath returns the API path prefix for collections.
func (c *ChromaClient) collectionPath() string {
	return fmt.Sprintf("/api/v2/tenants/%s/databases/%s/collections", c.tenant, c.database)
}

// chromaCollectionResponse is the response from collection creation / get.
type chromaCollectionResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GetOrCreateCollection creates a collection if it does not exist, or returns
// the existing one. Returns the collection ID.
func (c *ChromaClient) GetOrCreateCollection(ctx context.Context, name string) (string, error) {
	reqBody := map[string]interface{}{
		"name":          name,
		"get_or_create": true,
		"configuration": nil,
		"metadata":      nil,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("ai.ChromaClient.GetOrCreateCollection: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+c.collectionPath(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ai.ChromaClient.GetOrCreateCollection: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ai.ChromaClient.GetOrCreateCollection: %w: %w", ErrChromaUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("ai.ChromaClient.GetOrCreateCollection: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result chromaCollectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("ai.ChromaClient.GetOrCreateCollection: decode: %w", err)
	}

	return result.ID, nil
}

// chromaAddRequest is the request body for adding documents.
type chromaAddRequest struct {
	IDs        []string            `json:"ids"`
	Embeddings [][]float64         `json:"embeddings"`
	Metadatas  []map[string]string `json:"metadatas,omitempty"`
}

// AddDocuments adds embeddings with IDs and optional metadata to a collection.
func (c *ChromaClient) AddDocuments(ctx context.Context, collectionID string, ids []string, embeddings [][]float64, metadatas []map[string]string) error {
	if len(ids) != len(embeddings) || (metadatas != nil && len(ids) != len(metadatas)) {
		return fmt.Errorf("ai.ChromaClient.AddDocuments: %w: ids=%d embeddings=%d metadatas=%d",
			ErrMismatchedArrayLengths, len(ids), len(embeddings), len(metadatas))
	}
	reqBody := chromaAddRequest{
		IDs:        ids,
		Embeddings: embeddings,
		Metadatas:  metadatas,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("ai.ChromaClient.AddDocuments: marshal: %w", err)
	}

	url := fmt.Sprintf("%s%s/%s/add", c.baseURL, c.collectionPath(), collectionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ai.ChromaClient.AddDocuments: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ai.ChromaClient.AddDocuments: %w: %w", ErrChromaUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("ai.ChromaClient.AddDocuments: status %d: %s", resp.StatusCode, string(respBody))
	}

	// Drain response body for HTTP connection reuse.
	_, _ = io.Copy(io.Discard, resp.Body)

	return nil
}

// chromaQueryRequest is the request body for querying.
type chromaQueryRequest struct {
	QueryEmbeddings [][]float64            `json:"query_embeddings"`
	NResults        int                    `json:"n_results"`
	Include         []string               `json:"include"`
	Where           map[string]interface{} `json:"where,omitempty"`
}

// chromaQueryResponse is the response from a query.
type chromaQueryResponse struct {
	IDs       [][]string            `json:"ids"`
	Distances [][]float64           `json:"distances"`
	Metadatas [][]map[string]string `json:"metadatas"`
}

// Query finds the nearest neighbors to the given embedding in a collection.
func (c *ChromaClient) Query(ctx context.Context, collectionID string, embedding []float64, nResults int) ([]QueryResult, error) {
	reqBody := chromaQueryRequest{
		QueryEmbeddings: [][]float64{embedding},
		NResults:        nResults,
		Include:         []string{"distances", "metadatas"},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ai.ChromaClient.Query: marshal: %w", err)
	}

	url := fmt.Sprintf("%s%s/%s/query", c.baseURL, c.collectionPath(), collectionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ai.ChromaClient.Query: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai.ChromaClient.Query: %w: %w", ErrChromaUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("ai.ChromaClient.Query: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result chromaQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ai.ChromaClient.Query: decode: %w", err)
	}

	if len(result.IDs) == 0 || len(result.IDs[0]) == 0 {
		return []QueryResult{}, nil
	}

	var queryResults []QueryResult
	for i, id := range result.IDs[0] {
		qr := QueryResult{ID: id}
		if len(result.Distances) > 0 && i < len(result.Distances[0]) {
			qr.Distance = result.Distances[0][i]
		}
		if len(result.Metadatas) > 0 && i < len(result.Metadatas[0]) {
			qr.Metadata = result.Metadatas[0][i]
		}
		queryResults = append(queryResults, qr)
	}

	return queryResults, nil
}

// QueryWithFilter finds the nearest neighbors with a metadata where filter.
// The where map is passed directly to ChromaDB's where clause.
// Example: map[string]interface{}{"scope": "agent"} filters to agent-scoped docs.
func (c *ChromaClient) QueryWithFilter(ctx context.Context, collectionID string, embedding []float64, nResults int, where map[string]interface{}) ([]QueryResult, error) {
	reqBody := chromaQueryRequest{
		QueryEmbeddings: [][]float64{embedding},
		NResults:        nResults,
		Include:         []string{"distances", "metadatas"},
		Where:           where,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ai.ChromaClient.QueryWithFilter: marshal: %w", err)
	}

	url := fmt.Sprintf("%s%s/%s/query", c.baseURL, c.collectionPath(), collectionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ai.ChromaClient.QueryWithFilter: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai.ChromaClient.QueryWithFilter: %w: %w", ErrChromaUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("ai.ChromaClient.QueryWithFilter: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result chromaQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ai.ChromaClient.QueryWithFilter: decode: %w", err)
	}

	if len(result.IDs) == 0 || len(result.IDs[0]) == 0 {
		return []QueryResult{}, nil
	}

	var queryResults []QueryResult
	for i, id := range result.IDs[0] {
		qr := QueryResult{ID: id}
		if len(result.Distances) > 0 && i < len(result.Distances[0]) {
			qr.Distance = result.Distances[0][i]
		}
		if len(result.Metadatas) > 0 && i < len(result.Metadatas[0]) {
			qr.Metadata = result.Metadatas[0][i]
		}
		queryResults = append(queryResults, qr)
	}

	return queryResults, nil
}

// UpsertDocuments upserts (add or update) embeddings in a collection.
func (c *ChromaClient) UpsertDocuments(ctx context.Context, collectionID string, ids []string, embeddings [][]float64, metadatas []map[string]string) error {
	if len(ids) != len(embeddings) || (metadatas != nil && len(ids) != len(metadatas)) {
		return fmt.Errorf("ai.ChromaClient.UpsertDocuments: %w: ids=%d embeddings=%d metadatas=%d",
			ErrMismatchedArrayLengths, len(ids), len(embeddings), len(metadatas))
	}
	reqBody := chromaAddRequest{
		IDs:        ids,
		Embeddings: embeddings,
		Metadatas:  metadatas,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("ai.ChromaClient.UpsertDocuments: marshal: %w", err)
	}

	url := fmt.Sprintf("%s%s/%s/upsert", c.baseURL, c.collectionPath(), collectionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ai.ChromaClient.UpsertDocuments: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ai.ChromaClient.UpsertDocuments: %w: %w", ErrChromaUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("ai.ChromaClient.UpsertDocuments: status %d: %s", resp.StatusCode, string(respBody))
	}

	// Drain response body for HTTP connection reuse.
	_, _ = io.Copy(io.Discard, resp.Body)

	return nil
}

// chromaDeleteRequest is the request body for deleting documents.
type chromaDeleteRequest struct {
	IDs   []string               `json:"ids,omitempty"`
	Where map[string]interface{} `json:"where,omitempty"`
}

// DeleteDocuments removes documents by ID from a collection.
func (c *ChromaClient) DeleteDocuments(ctx context.Context, collectionID string, ids []string) error {
	reqBody := chromaDeleteRequest{
		IDs: ids,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("ai.ChromaClient.DeleteDocuments: marshal: %w", err)
	}

	url := fmt.Sprintf("%s%s/%s/delete", c.baseURL, c.collectionPath(), collectionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ai.ChromaClient.DeleteDocuments: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ai.ChromaClient.DeleteDocuments: %w: %w", ErrChromaUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("ai.ChromaClient.DeleteDocuments: status %d: %s", resp.StatusCode, string(respBody))
	}

	// Drain response body for HTTP connection reuse.
	_, _ = io.Copy(io.Discard, resp.Body)

	return nil
}

// DeleteByMetadata removes documents matching a metadata filter from a collection.
// This is used for note deletion where we match by note_id metadata instead of
// generating chunk IDs (C-29).
func (c *ChromaClient) DeleteByMetadata(ctx context.Context, collectionID string, where map[string]interface{}) error {
	reqBody := chromaDeleteRequest{
		Where: where,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("ai.ChromaClient.DeleteByMetadata: marshal: %w", err)
	}

	url := fmt.Sprintf("%s%s/%s/delete", c.baseURL, c.collectionPath(), collectionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ai.ChromaClient.DeleteByMetadata: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ai.ChromaClient.DeleteByMetadata: %w: %w", ErrChromaUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("ai.ChromaClient.DeleteByMetadata: status %d: %s", resp.StatusCode, string(respBody))
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// CollectionName returns the standard collection name for a user.
// userID must be a valid ULID (alphanumeric). Invalid IDs return
// a safe fallback to prevent collection name injection.
func CollectionName(userID string) string {
	// Defense-in-depth: validate userID contains only ULID-safe characters.
	for _, r := range userID {
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
			return "user_invalid_notes"
		}
	}
	return "user_" + userID + "_notes"
}
