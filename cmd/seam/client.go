package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// APIClient wraps the Seam REST API for the TUI.
type APIClient struct {
	BaseURL      string
	AccessToken  string
	RefreshToken string
	HTTPClient   *http.Client
}

// NewAPIClient creates a new API client pointing at the given server URL.
func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// -- Request / Response types ------------------------------------------------

// AuthResponse is the server response for login and register.
type AuthResponse struct {
	User   UserInfo  `json:"user"`
	Tokens TokenPair `json:"tokens"`
}

// TokenPair holds an access and refresh token.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// UserInfo describes the authenticated user.
type UserInfo struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// Project is a note-grouping project returned by the server.
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// Note is a note returned by the server.
type Note struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	ProjectID string   `json:"project_id,omitempty"`
	FilePath  string   `json:"file_path"`
	Body      string   `json:"body"`
	Tags      []string `json:"tags"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// SearchResult is a single FTS search hit.
type SearchResult struct {
	NoteID  string  `json:"note_id"`
	Title   string  `json:"title"`
	Snippet string  `json:"snippet"`
	Rank    float64 `json:"rank"`
}

// APIError is a JSON error body returned by the server.
type APIError struct {
	Message string `json:"error"`
}

func (e *APIError) Error() string {
	return e.Message
}

// -- Auth methods ------------------------------------------------------------

// Login authenticates with username and password.
func (c *APIClient) Login(username, password string) (*AuthResponse, error) {
	body := map[string]string{
		"username": username,
		"password": password,
	}
	var resp AuthResponse
	if err := c.post("/api/auth/login", body, &resp); err != nil {
		return nil, err
	}
	c.AccessToken = resp.Tokens.AccessToken
	c.RefreshToken = resp.Tokens.RefreshToken
	return &resp, nil
}

// Register creates a new account.
func (c *APIClient) Register(username, email, password string) (*AuthResponse, error) {
	body := map[string]string{
		"username": username,
		"email":    email,
		"password": password,
	}
	var resp AuthResponse
	if err := c.post("/api/auth/register", body, &resp); err != nil {
		return nil, err
	}
	c.AccessToken = resp.Tokens.AccessToken
	c.RefreshToken = resp.Tokens.RefreshToken
	return &resp, nil
}

// Refresh obtains a new access token using the stored refresh token.
func (c *APIClient) Refresh() (*TokenPair, error) {
	body := map[string]string{
		"refresh_token": c.RefreshToken,
	}
	var resp TokenPair
	if err := c.post("/api/auth/refresh", body, &resp); err != nil {
		return nil, err
	}
	c.AccessToken = resp.AccessToken
	c.RefreshToken = resp.RefreshToken
	return &resp, nil
}

// -- Project methods ---------------------------------------------------------

// ListProjects returns all projects for the authenticated user.
func (c *APIClient) ListProjects() ([]*Project, error) {
	var projects []*Project
	if err := c.get("/api/projects", nil, &projects); err != nil {
		return nil, err
	}
	return projects, nil
}

// -- Note methods ------------------------------------------------------------

// ListNotes returns notes, optionally filtered by project ID.
// Pass "inbox" for projectID to list notes with no project.
// Returns the notes and the total count from X-Total-Count.
func (c *APIClient) ListNotes(projectID string) ([]*Note, int, error) {
	params := url.Values{}
	if projectID != "" {
		params.Set("project", projectID)
	}

	var notes []*Note
	total, err := c.getWithTotal("/api/notes", params, &notes)
	if err != nil {
		return nil, 0, err
	}
	if notes == nil {
		notes = []*Note{}
	}
	return notes, total, nil
}

// GetNote retrieves a single note by ID.
func (c *APIClient) GetNote(id string) (*Note, error) {
	var n Note
	if err := c.get("/api/notes/"+id, nil, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// CreateNote creates a new note.
func (c *APIClient) CreateNote(title, body, projectID string) (*Note, error) {
	req := map[string]interface{}{
		"title": title,
		"body":  body,
	}
	if projectID != "" {
		req["project_id"] = projectID
	}
	var n Note
	if err := c.post("/api/notes", req, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// UpdateNote updates an existing note. Only non-nil fields are sent.
func (c *APIClient) UpdateNote(id string, title, body *string) (*Note, error) {
	req := map[string]interface{}{}
	if title != nil {
		req["title"] = *title
	}
	if body != nil {
		req["body"] = *body
	}
	var n Note
	if err := c.put("/api/notes/"+id, req, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// DeleteNote deletes a note by ID.
func (c *APIClient) DeleteNote(id string) error {
	return c.delete("/api/notes/" + id)
}

// -- Search methods ----------------------------------------------------------

// Search performs a full-text search.
func (c *APIClient) Search(query string) ([]SearchResult, error) {
	params := url.Values{}
	params.Set("q", query)

	var results []SearchResult
	if err := c.get("/api/search", params, &results); err != nil {
		return nil, err
	}
	if results == nil {
		results = []SearchResult{}
	}
	return results, nil
}

// -- AI types ----------------------------------------------------------------

// SemanticSearchResult is a single semantic search hit.
type SemanticSearchResult struct {
	NoteID  string  `json:"note_id"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
}

// ChatResult is the response from the Ask Seam endpoint.
type ChatResult struct {
	Response  string   `json:"response"`
	Citations []string `json:"citations"`
}

// ChatMessage is a single message in a chat history.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// -- AI methods --------------------------------------------------------------

// SearchSemantic performs a semantic search.
func (c *APIClient) SearchSemantic(query string) ([]SemanticSearchResult, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("limit", "20")

	var results []SemanticSearchResult
	if err := c.get("/api/search/semantic", params, &results); err != nil {
		return nil, err
	}
	if results == nil {
		results = []SemanticSearchResult{}
	}
	return results, nil
}

// AskSeam sends a question to the Ask Seam RAG chat endpoint.
func (c *APIClient) AskSeam(query string, history []ChatMessage) (*ChatResult, error) {
	body := map[string]interface{}{
		"query": query,
	}
	if len(history) > 0 {
		body["history"] = history
	}
	var result ChatResult
	if err := c.post("/api/ai/ask", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// -- HTTP helpers ------------------------------------------------------------

func (c *APIClient) get(path string, params url.Values, out interface{}) error {
	_, err := c.doRequest("GET", path, params, nil, out)
	return err
}

func (c *APIClient) getWithTotal(path string, params url.Values, out interface{}) (int, error) {
	return c.doRequest("GET", path, params, nil, out)
}

func (c *APIClient) post(path string, body interface{}, out interface{}) error {
	_, err := c.doRequest("POST", path, nil, body, out)
	return err
}

func (c *APIClient) put(path string, body interface{}, out interface{}) error {
	_, err := c.doRequest("PUT", path, nil, body, out)
	return err
}

func (c *APIClient) delete(path string) error {
	_, err := c.doRequest("DELETE", path, nil, nil, nil)
	return err
}

func (c *APIClient) doRequest(method, path string, params url.Values, reqBody interface{}, out interface{}) (int, error) {
	u := c.BaseURL + path
	if params != nil && len(params) > 0 {
		u += "?" + params.Encode()
	}

	var bodyReader io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return 0, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, u, bodyReader)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}

	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiErr APIError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Message != "" {
			return 0, &apiErr
		}
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	total := 0
	if tc := resp.Header.Get("X-Total-Count"); tc != "" {
		total, _ = strconv.Atoi(tc)
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return 0, fmt.Errorf("decode response: %w", err)
		}
	}

	return total, nil
}
