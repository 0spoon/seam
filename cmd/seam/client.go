package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
			Timeout: 30 * time.Second,
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

// ListNotesPaged returns notes with pagination support.
func (c *APIClient) ListNotesPaged(projectID string, offset, limit int) ([]*Note, int, error) {
	params := url.Values{}
	if projectID != "" {
		params.Set("project", projectID)
	}
	if offset > 0 {
		params.Set("offset", strconv.Itoa(offset))
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
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

// ListNotesAll returns all notes with custom sort and limit parameters.
func (c *APIClient) ListNotesAll(sort string, limit int) ([]*Note, error) {
	params := url.Values{}
	if sort != "" {
		params.Set("sort", sort)
	}
	params.Set("sort_dir", "desc")
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}

	var notes []*Note
	if err := c.get("/api/notes", params, &notes); err != nil {
		return nil, err
	}
	if notes == nil {
		notes = []*Note{}
	}
	return notes, nil
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
	if err := c.postAI("/api/ai/ask", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// -- Capture types -----------------------------------------------------------

// CaptureURLRequest is the request body for URL capture.
type CaptureURLRequest struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// -- Capture methods ---------------------------------------------------------

// CaptureURL fetches a URL and creates a note from its content.
func (c *APIClient) CaptureURL(rawURL string) (*Note, error) {
	req := CaptureURLRequest{
		Type: "url",
		URL:  rawURL,
	}
	var n Note
	if err := c.post("/api/capture", req, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// -- Template types ----------------------------------------------------------

// TemplateMeta is the metadata for a template.
type TemplateMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// TemplateDetail is a full template with body.
type TemplateDetail struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

// TemplateApplyResult is the response from applying a template.
type TemplateApplyResult struct {
	Body string `json:"body"`
}

// -- Template methods --------------------------------------------------------

// ListTemplates returns all available templates.
func (c *APIClient) ListTemplates() ([]TemplateMeta, error) {
	var templates []TemplateMeta
	if err := c.get("/api/templates", nil, &templates); err != nil {
		return nil, err
	}
	if templates == nil {
		templates = []TemplateMeta{}
	}
	return templates, nil
}

// ApplyTemplate applies a template with variable substitution and returns the rendered body.
func (c *APIClient) ApplyTemplate(name string, vars map[string]string) (*TemplateApplyResult, error) {
	req := map[string]interface{}{
		"vars": vars,
	}
	var result TemplateApplyResult
	if err := c.post("/api/templates/"+name+"/apply", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// -- AI Assist types ---------------------------------------------------------

// AIAssistRequest is the request body for AI writing assist.
type AIAssistRequest struct {
	Action    string `json:"action"`
	Selection string `json:"selection,omitempty"`
}

// AIAssistResult is the response from AI writing assist.
type AIAssistResult struct {
	Result string `json:"result"`
}

// -- AI Assist methods -------------------------------------------------------

// Assist calls the AI writing assist endpoint.
func (c *APIClient) Assist(noteID, action, selection string) (*AIAssistResult, error) {
	req := AIAssistRequest{
		Action:    action,
		Selection: selection,
	}
	var result AIAssistResult
	if err := c.postAI("/api/ai/notes/"+noteID+"/assist", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// -- HTTP helpers ------------------------------------------------------------

// aiTimeout is used for AI endpoints that call Ollama and may take
// several minutes for local model inference.
const aiTimeout = 6 * time.Minute

func (c *APIClient) get(path string, params url.Values, out interface{}) error {
	_, err := c.doRequestCtx(context.Background(), "GET", path, params, nil, out, 0)
	return err
}

func (c *APIClient) getWithTotal(path string, params url.Values, out interface{}) (int, error) {
	return c.doRequestCtx(context.Background(), "GET", path, params, nil, out, 0)
}

func (c *APIClient) post(path string, body interface{}, out interface{}) error {
	_, err := c.doRequestCtx(context.Background(), "POST", path, nil, body, out, 0)
	return err
}

// postAI is like post but uses an extended timeout for AI inference endpoints.
// It uses a per-request context timeout to avoid mutating the shared HTTPClient.
func (c *APIClient) postAI(path string, body interface{}, out interface{}) error {
	_, err := c.doRequestCtx(context.Background(), "POST", path, nil, body, out, aiTimeout)
	return err
}

func (c *APIClient) put(path string, body interface{}, out interface{}) error {
	_, err := c.doRequestCtx(context.Background(), "PUT", path, nil, body, out, 0)
	return err
}

func (c *APIClient) delete(path string) error {
	_, err := c.doRequestCtx(context.Background(), "DELETE", path, nil, nil, nil, 0)
	return err
}

// Context-aware variants for long-running operations.

// GetCtx is like get but accepts a context for cancellation.
func (c *APIClient) GetCtx(ctx context.Context, path string, params url.Values, out interface{}) error {
	_, err := c.doRequestCtx(ctx, "GET", path, params, nil, out, 0)
	return err
}

// PostCtx is like post but accepts a context for cancellation.
func (c *APIClient) PostCtx(ctx context.Context, path string, body interface{}, out interface{}) error {
	_, err := c.doRequestCtx(ctx, "POST", path, nil, body, out, 0)
	return err
}

// PostAICtx is like postAI but accepts a context for cancellation.
func (c *APIClient) PostAICtx(ctx context.Context, path string, body interface{}, out interface{}) error {
	_, err := c.doRequestCtx(ctx, "POST", path, nil, body, out, aiTimeout)
	return err
}

// AssistCtx calls the AI writing assist endpoint with a context.
func (c *APIClient) AssistCtx(ctx context.Context, noteID, action, selection string) (*AIAssistResult, error) {
	req := AIAssistRequest{
		Action:    action,
		Selection: selection,
	}
	var result AIAssistResult
	if err := c.PostAICtx(ctx, "/api/ai/notes/"+noteID+"/assist", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// AskSeamCtx sends a question to the Ask Seam RAG chat endpoint with a context.
func (c *APIClient) AskSeamCtx(ctx context.Context, query string, history []ChatMessage) (*ChatResult, error) {
	body := map[string]interface{}{
		"query": query,
	}
	if len(history) > 0 {
		body["history"] = history
	}
	var result ChatResult
	if err := c.PostAICtx(ctx, "/api/ai/ask", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RefreshCtx obtains a new access token with a context for timeout control.
func (c *APIClient) RefreshCtx(ctx context.Context) (*TokenPair, error) {
	body := map[string]string{
		"refresh_token": c.RefreshToken,
	}
	var resp TokenPair
	if err := c.PostCtx(ctx, "/api/auth/refresh", body, &resp); err != nil {
		return nil, err
	}
	c.AccessToken = resp.AccessToken
	c.RefreshToken = resp.RefreshToken
	return &resp, nil
}

func (c *APIClient) doRequest(method, path string, params url.Values, reqBody interface{}, out interface{}) (int, error) {
	return c.doRequestCtx(context.Background(), method, path, params, reqBody, out, 0)
}

// doRequestWithTimeout performs an HTTP request. If timeout > 0, a per-request
// context with that deadline is used instead of mutating the shared client.
func (c *APIClient) doRequestWithTimeout(method, path string, params url.Values, reqBody interface{}, out interface{}, timeout time.Duration) (int, error) {
	return c.doRequestCtx(context.Background(), method, path, params, reqBody, out, timeout)
}

// doRequestCtx performs an HTTP request with the given context. If timeout > 0,
// a child context with that deadline is derived. On a 401 response, it
// automatically attempts a token refresh and retries the request once.
func (c *APIClient) doRequestCtx(ctx context.Context, method, path string, params url.Values, reqBody interface{}, out interface{}, timeout time.Duration) (int, error) {
	total, statusCode, err := c.doSingleRequest(ctx, method, path, params, reqBody, out, timeout)
	if err != nil && statusCode == http.StatusUnauthorized && c.RefreshToken != "" && path != "/api/auth/refresh" {
		// Attempt automatic token refresh and retry once.
		refreshBody := map[string]string{"refresh_token": c.RefreshToken}
		var tokens TokenPair
		_, _, refreshErr := c.doSingleRequest(ctx, "POST", "/api/auth/refresh", nil, refreshBody, &tokens, 0)
		if refreshErr == nil {
			c.AccessToken = tokens.AccessToken
			c.RefreshToken = tokens.RefreshToken
			total, _, err = c.doSingleRequest(ctx, method, path, params, reqBody, out, timeout)
		}
	}
	return total, err
}

// doSingleRequest performs a single HTTP request without retry logic.
// Returns (total, statusCode, error).
func (c *APIClient) doSingleRequest(ctx context.Context, method, path string, params url.Values, reqBody interface{}, out interface{}, timeout time.Duration) (int, int, error) {
	u := strings.TrimRight(c.BaseURL, "/") + path
	if params != nil && len(params) > 0 {
		u += "?" + params.Encode()
	}

	var bodyData []byte
	if reqBody != nil {
		var err error
		bodyData, err = json.Marshal(reqBody)
		if err != nil {
			return 0, 0, fmt.Errorf("marshal request: %w", err)
		}
	}

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	var bodyReader io.Reader
	if bodyData != nil {
		bodyReader = bytes.NewReader(bodyData)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return 0, 0, fmt.Errorf("create request: %w", err)
	}

	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiErr APIError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Message != "" {
			return 0, resp.StatusCode, &apiErr
		}
		return 0, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	total := 0
	if tc := resp.Header.Get("X-Total-Count"); tc != "" {
		total, _ = strconv.Atoi(tc)
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return 0, resp.StatusCode, fmt.Errorf("decode response: %w", err)
		}
	}

	return total, resp.StatusCode, nil
}
