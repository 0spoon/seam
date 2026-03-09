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

// ChromaClient is an HTTP client for the ChromaDB REST API v2.
type ChromaClient struct {
	baseURL    string
	tenant     string
	database   string
	httpClient *http.Client
}

// NewChromaClient creates a new ChromaDB API client.
func NewChromaClient(baseURL string) *ChromaClient {
	return &ChromaClient{
		baseURL:  baseURL,
		tenant:   "default_tenant",
		database: "default_database",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

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

	return nil
}

// chromaQueryRequest is the request body for querying.
type chromaQueryRequest struct {
	QueryEmbeddings [][]float64 `json:"query_embeddings"`
	NResults        int         `json:"n_results"`
	Include         []string    `json:"include"`
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
		return nil, nil
	}

	var queryResults []QueryResult
	for i, id := range result.IDs[0] {
		qr := QueryResult{ID: id}
		if i < len(result.Distances[0]) {
			qr.Distance = result.Distances[0][i]
		}
		if i < len(result.Metadatas[0]) {
			qr.Metadata = result.Metadatas[0][i]
		}
		queryResults = append(queryResults, qr)
	}

	return queryResults, nil
}

// UpsertDocuments upserts (add or update) embeddings in a collection.
func (c *ChromaClient) UpsertDocuments(ctx context.Context, collectionID string, ids []string, embeddings [][]float64, metadatas []map[string]string) error {
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

	return nil
}

// chromaDeleteRequest is the request body for deleting documents.
type chromaDeleteRequest struct {
	IDs []string `json:"ids"`
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

	return nil
}

// CollectionName returns the standard collection name for a user.
func CollectionName(userID string) string {
	return "user_" + userID + "_notes"
}
