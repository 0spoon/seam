package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/agent"
	"github.com/katata/seam/internal/auth"
	seamcp "github.com/katata/seam/internal/mcp"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/search"
)

// mockAgentService implements seamcp.AgentService with pluggable function fields.
// Each method delegates to a corresponding Fn field when non-nil, otherwise panics
// to catch unexpected calls in tests.
type mockAgentService struct {
	sessionStartFn          func(ctx context.Context, userID, name string, maxContextChars int) (*agent.Briefing, error)
	sessionEndFn            func(ctx context.Context, userID, sessionName, findings string) error
	sessionListFn           func(ctx context.Context, userID, status string, limit int) ([]*agent.Session, error)
	sessionPlanSetFn        func(ctx context.Context, userID, sessionName, content string) (string, error)
	sessionProgressUpdateFn func(ctx context.Context, userID, sessionName, task, status, notes string) (string, error)
	sessionContextSetFn     func(ctx context.Context, userID, sessionName, content string) (string, error)
	memoryReadFn            func(ctx context.Context, userID, category, name string) (string, string, error)
	memoryWriteFn           func(ctx context.Context, userID, category, name, content string) (string, error)
	memoryAppendFn          func(ctx context.Context, userID, category, name, content string) error
	memoryListFn            func(ctx context.Context, userID, category string) ([]agent.MemoryItem, error)
	memoryDeleteFn          func(ctx context.Context, userID, category, name string) error
	contextGatherFn         func(ctx context.Context, userID, query, scope string, maxChars int, recencyBias float64) ([]agent.KnowledgeHit, error)
	notesSearchFn           func(ctx context.Context, userID, query string, limit int, recencyBias float64) ([]search.FTSResult, error)
	notesReadFn             func(ctx context.Context, userID, noteID string) (*note.Note, error)
	notesListFn             func(ctx context.Context, userID, projectSlug, tag string, limit int) ([]*note.Note, int, error)
	notesCreateFn           func(ctx context.Context, userID, title, body, projectSlug string, tags []string) (*note.Note, error)
	memorySearchFn          func(ctx context.Context, userID, query string, limit int) ([]agent.KnowledgeHit, error)
	sessionMetricsFn        func(ctx context.Context, userID, sessionName string) (*agent.SessionMetrics, error)
	notesUpdateFn           func(ctx context.Context, userID, noteID string, title, body, projectSlug *string, tags *[]string) (*note.Note, error)
	notesDeleteFn           func(ctx context.Context, userID, noteID string) error
	notesTagsFn             func(ctx context.Context, userID string) ([]note.TagCount, error)
	notesDailyFn            func(ctx context.Context, userID string, date time.Time) (*note.Note, error)
	projectListFn           func(ctx context.Context, userID string) ([]*project.Project, error)
	projectCreateFn         func(ctx context.Context, userID, name, description string) (*project.Project, error)
	notesAppendFn           func(ctx context.Context, userID, noteID, text string) (*note.Note, error)
	notesChangelogFn        func(ctx context.Context, userID string, since, until time.Time, limit int) ([]*note.Note, int, error)
	notesVersionsFn         func(ctx context.Context, userID, noteID string, limit int) ([]*note.NoteVersion, int, error)
	notesGetVersionFn       func(ctx context.Context, userID, noteID string, version int) (*note.NoteVersion, error)
	notesBacklinksFn        func(ctx context.Context, userID, noteID string) ([]*note.Note, error)
	labOpenFn               func(ctx context.Context, userID, name, problem, domain string, tags []string) (*agent.LabInfo, error)
	trialRecordFn           func(ctx context.Context, userID, lab, title, changes, expected, actual, outcome, notes string) (*agent.TrialSummary, error)
	decisionRecordFn        func(ctx context.Context, userID, lab, title, rationale, basedOn, nextSteps string) (*agent.DecisionInfo, error)
	trialQueryFn            func(ctx context.Context, userID, lab, query, outcome string, limit int) ([]agent.TrialSummary, error)
}

func (m *mockAgentService) SessionStart(ctx context.Context, userID, name string, maxContextChars int) (*agent.Briefing, error) {
	if m.sessionStartFn == nil {
		panic("mockAgentService.SessionStart not implemented")
	}
	return m.sessionStartFn(ctx, userID, name, maxContextChars)
}

func (m *mockAgentService) SessionEnd(ctx context.Context, userID, sessionName, findings string) error {
	if m.sessionEndFn == nil {
		panic("mockAgentService.SessionEnd not implemented")
	}
	return m.sessionEndFn(ctx, userID, sessionName, findings)
}

func (m *mockAgentService) SessionList(ctx context.Context, userID, status string, limit int) ([]*agent.Session, error) {
	if m.sessionListFn == nil {
		panic("mockAgentService.SessionList not implemented")
	}
	return m.sessionListFn(ctx, userID, status, limit)
}

func (m *mockAgentService) SessionPlanSet(ctx context.Context, userID, sessionName, content string) (string, error) {
	if m.sessionPlanSetFn == nil {
		panic("mockAgentService.SessionPlanSet not implemented")
	}
	return m.sessionPlanSetFn(ctx, userID, sessionName, content)
}

func (m *mockAgentService) SessionProgressUpdate(ctx context.Context, userID, sessionName, task, status, notes string) (string, error) {
	if m.sessionProgressUpdateFn == nil {
		panic("mockAgentService.SessionProgressUpdate not implemented")
	}
	return m.sessionProgressUpdateFn(ctx, userID, sessionName, task, status, notes)
}

func (m *mockAgentService) SessionContextSet(ctx context.Context, userID, sessionName, content string) (string, error) {
	if m.sessionContextSetFn == nil {
		panic("mockAgentService.SessionContextSet not implemented")
	}
	return m.sessionContextSetFn(ctx, userID, sessionName, content)
}

func (m *mockAgentService) MemoryRead(ctx context.Context, userID, category, name string) (string, string, error) {
	if m.memoryReadFn == nil {
		panic("mockAgentService.MemoryRead not implemented")
	}
	return m.memoryReadFn(ctx, userID, category, name)
}

func (m *mockAgentService) MemoryWrite(ctx context.Context, userID, category, name, content string) (string, error) {
	if m.memoryWriteFn == nil {
		panic("mockAgentService.MemoryWrite not implemented")
	}
	return m.memoryWriteFn(ctx, userID, category, name, content)
}

func (m *mockAgentService) MemoryAppend(ctx context.Context, userID, category, name, content string) error {
	if m.memoryAppendFn == nil {
		panic("mockAgentService.MemoryAppend not implemented")
	}
	return m.memoryAppendFn(ctx, userID, category, name, content)
}

func (m *mockAgentService) MemoryList(ctx context.Context, userID, category string) ([]agent.MemoryItem, error) {
	if m.memoryListFn == nil {
		panic("mockAgentService.MemoryList not implemented")
	}
	return m.memoryListFn(ctx, userID, category)
}

func (m *mockAgentService) MemoryDelete(ctx context.Context, userID, category, name string) error {
	if m.memoryDeleteFn == nil {
		panic("mockAgentService.MemoryDelete not implemented")
	}
	return m.memoryDeleteFn(ctx, userID, category, name)
}

func (m *mockAgentService) ContextGather(ctx context.Context, userID, query, scope string, maxChars int, recencyBias float64) ([]agent.KnowledgeHit, error) {
	if m.contextGatherFn == nil {
		return []agent.KnowledgeHit{}, nil
	}
	return m.contextGatherFn(ctx, userID, query, scope, maxChars, recencyBias)
}

func (m *mockAgentService) NotesSearch(ctx context.Context, userID, query string, limit int, recencyBias float64) ([]search.FTSResult, error) {
	if m.notesSearchFn == nil {
		return []search.FTSResult{}, nil
	}
	return m.notesSearchFn(ctx, userID, query, limit, recencyBias)
}

func (m *mockAgentService) NotesRead(ctx context.Context, userID, noteID string) (*note.Note, error) {
	if m.notesReadFn == nil {
		panic("mockAgentService.NotesRead not implemented")
	}
	return m.notesReadFn(ctx, userID, noteID)
}

func (m *mockAgentService) NotesList(ctx context.Context, userID, projectSlug, tag string, limit int) ([]*note.Note, int, error) {
	if m.notesListFn == nil {
		return []*note.Note{}, 0, nil
	}
	return m.notesListFn(ctx, userID, projectSlug, tag, limit)
}

func (m *mockAgentService) NotesCreate(ctx context.Context, userID, title, body, projectSlug string, tags []string) (*note.Note, error) {
	if m.notesCreateFn == nil {
		panic("mockAgentService.NotesCreate not implemented")
	}
	return m.notesCreateFn(ctx, userID, title, body, projectSlug, tags)
}

func (m *mockAgentService) MemorySearch(ctx context.Context, userID, query string, limit int) ([]agent.KnowledgeHit, error) {
	if m.memorySearchFn == nil {
		return []agent.KnowledgeHit{}, nil
	}
	return m.memorySearchFn(ctx, userID, query, limit)
}

func (m *mockAgentService) SessionMetrics(ctx context.Context, userID, sessionName string) (*agent.SessionMetrics, error) {
	if m.sessionMetricsFn == nil {
		panic("mockAgentService.SessionMetrics not implemented")
	}
	return m.sessionMetricsFn(ctx, userID, sessionName)
}

func (m *mockAgentService) NotesUpdate(ctx context.Context, userID, noteID string, title, body, projectSlug *string, tags *[]string) (*note.Note, error) {
	if m.notesUpdateFn == nil {
		panic("mockAgentService.NotesUpdate not implemented")
	}
	return m.notesUpdateFn(ctx, userID, noteID, title, body, projectSlug, tags)
}

func (m *mockAgentService) NotesDelete(ctx context.Context, userID, noteID string) error {
	if m.notesDeleteFn == nil {
		panic("mockAgentService.NotesDelete not implemented")
	}
	return m.notesDeleteFn(ctx, userID, noteID)
}

func (m *mockAgentService) NotesTags(ctx context.Context, userID string) ([]note.TagCount, error) {
	if m.notesTagsFn == nil {
		return []note.TagCount{}, nil
	}
	return m.notesTagsFn(ctx, userID)
}

func (m *mockAgentService) NotesDaily(ctx context.Context, userID string, date time.Time) (*note.Note, error) {
	if m.notesDailyFn == nil {
		panic("mockAgentService.NotesDaily not implemented")
	}
	return m.notesDailyFn(ctx, userID, date)
}

func (m *mockAgentService) ProjectList(ctx context.Context, userID string) ([]*project.Project, error) {
	if m.projectListFn == nil {
		return []*project.Project{}, nil
	}
	return m.projectListFn(ctx, userID)
}

func (m *mockAgentService) ProjectCreate(ctx context.Context, userID, name, description string) (*project.Project, error) {
	if m.projectCreateFn == nil {
		panic("mockAgentService.ProjectCreate not implemented")
	}
	return m.projectCreateFn(ctx, userID, name, description)
}

func (m *mockAgentService) NotesAppend(ctx context.Context, userID, noteID, text string) (*note.Note, error) {
	if m.notesAppendFn == nil {
		panic("mockAgentService.NotesAppend not implemented")
	}
	return m.notesAppendFn(ctx, userID, noteID, text)
}

func (m *mockAgentService) NotesChangelog(ctx context.Context, userID string, since, until time.Time, limit int) ([]*note.Note, int, error) {
	if m.notesChangelogFn == nil {
		return []*note.Note{}, 0, nil
	}
	return m.notesChangelogFn(ctx, userID, since, until, limit)
}

func (m *mockAgentService) NotesVersions(ctx context.Context, userID, noteID string, limit int) ([]*note.NoteVersion, int, error) {
	if m.notesVersionsFn == nil {
		return []*note.NoteVersion{}, 0, nil
	}
	return m.notesVersionsFn(ctx, userID, noteID, limit)
}

func (m *mockAgentService) NotesGetVersion(ctx context.Context, userID, noteID string, version int) (*note.NoteVersion, error) {
	if m.notesGetVersionFn == nil {
		panic("mockAgentService.NotesGetVersion not implemented")
	}
	return m.notesGetVersionFn(ctx, userID, noteID, version)
}

func (m *mockAgentService) NotesBacklinks(ctx context.Context, userID, noteID string) ([]*note.Note, error) {
	if m.notesBacklinksFn == nil {
		return []*note.Note{}, nil
	}
	return m.notesBacklinksFn(ctx, userID, noteID)
}

func (m *mockAgentService) LabOpen(ctx context.Context, userID, name, problem, domain string, tags []string) (*agent.LabInfo, error) {
	if m.labOpenFn == nil {
		panic("mockAgentService.LabOpen not implemented")
	}
	return m.labOpenFn(ctx, userID, name, problem, domain, tags)
}

func (m *mockAgentService) TrialRecord(ctx context.Context, userID, lab, title, changes, expected, actual, outcome, notes string) (*agent.TrialSummary, error) {
	if m.trialRecordFn == nil {
		panic("mockAgentService.TrialRecord not implemented")
	}
	return m.trialRecordFn(ctx, userID, lab, title, changes, expected, actual, outcome, notes)
}

func (m *mockAgentService) DecisionRecord(ctx context.Context, userID, lab, title, rationale, basedOn, nextSteps string) (*agent.DecisionInfo, error) {
	if m.decisionRecordFn == nil {
		panic("mockAgentService.DecisionRecord not implemented")
	}
	return m.decisionRecordFn(ctx, userID, lab, title, rationale, basedOn, nextSteps)
}

func (m *mockAgentService) TrialQuery(ctx context.Context, userID, lab, query, outcome string, limit int) ([]agent.TrialSummary, error) {
	if m.trialQueryFn == nil {
		panic("mockAgentService.TrialQuery not implemented")
	}
	return m.trialQueryFn(ctx, userID, lab, query, outcome, limit)
}

// newJWTManager creates a JWTManager for testing.
func newJWTManager() *auth.JWTManager {
	return auth.NewJWTManager("test-secret-key-for-mcp-tests", 1*time.Hour)
}

// jsonrpcRequest is a minimal JSON-RPC 2.0 request structure for building test payloads.
type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	ID      int         `json:"id"`
}

// toolCallParams matches the MCP tools/call params structure.
type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// handleMessageWithUserID sends a JSON-RPC message through HandleMessage with a user ID context.
func handleMessageWithUserID(t *testing.T, srv *seamcp.Server, userID string, req jsonrpcRequest) mcp.JSONRPCMessage {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)

	ctx := context.Background()
	if userID != "" {
		ctx = reqctx.WithUserID(ctx, userID)
	}

	return srv.MCPServer().HandleMessage(ctx, body)
}

// initMCPSession performs the MCP initialize handshake via HTTP and returns
// the Mcp-Session-Id for subsequent requests.
func initMCPSession(t *testing.T, handler http.Handler, authHeader string) string {
	t.Helper()
	initReq := jsonrpcRequest{
		JSONRPC: "2.0",
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
		ID: 1,
	}
	body, err := json.Marshal(initReq)
	require.NoError(t, err)

	httpReq := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		httpReq.Header.Set("Authorization", authHeader)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httpReq)

	resp := rec.Result()
	defer resp.Body.Close()

	sessionID := resp.Header.Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID, "initialize should return Mcp-Session-Id header")
	return sessionID
}

// httpToolCall sends a tools/call JSON-RPC request via HTTP and returns the raw response body.
func httpToolCall(t *testing.T, handler http.Handler, sessionID, authHeader, toolName string, args map[string]any) string {
	t.Helper()
	rpcReq := jsonrpcRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: toolCallParams{
			Name:      toolName,
			Arguments: args,
		},
		ID: 2,
	}
	body, err := json.Marshal(rpcReq)
	require.NoError(t, err)

	httpReq := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Mcp-Session-Id", sessionID)
	if authHeader != "" {
		httpReq.Header.Set("Authorization", authHeader)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httpReq)

	resp := rec.Result()
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(respBody)
}

// --- Server construction tests ---

func TestNew_RegistersAllTools(t *testing.T) {
	srv := newServer(&mockAgentService{})

	tools := srv.MCPServer().ListTools()

	expectedTools := []string{
		"session_start",
		"session_plan_set",
		"session_progress_update",
		"session_context_set",
		"session_end",
		"session_list",
		"memory_read",
		"memory_write",
		"memory_append",
		"memory_list",
		"memory_delete",
		"context_gather",
		"notes_search",
		"notes_read",
		"notes_list",
		"notes_create",
		"notes_update",
		"notes_delete",
		"notes_tags",
		"notes_daily",
		"notes_append",
		"notes_changelog",
		"notes_versions",
		"project_list",
		"project_create",
		"memory_search",
		"session_metrics",
		"lab_open",
		"trial_record",
		"decision_record",
		"trial_query",
	}

	require.Len(t, tools, len(expectedTools), "expected %d tools registered", len(expectedTools))

	for _, name := range expectedTools {
		_, exists := tools[name]
		require.True(t, exists, "tool %q should be registered", name)
	}
}

func TestNew_DefaultLogger_NoPanic(t *testing.T) {
	require.NotPanics(t, func() {
		srv := seamcp.New(seamcp.Config{
			AgentService: &mockAgentService{},
			Logger:       nil,
		})
		require.NotNil(t, srv)
	})
}

func TestNew_WithLogger_UsesProvidedLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mock := &mockAgentService{
		sessionListFn: func(context.Context, string, string, int) ([]*agent.Session, error) {
			return []*agent.Session{}, nil
		},
	}

	srv := seamcp.New(seamcp.Config{
		AgentService: mock,
		Logger:       logger,
	})
	require.NotNil(t, srv)

	// Call a tool via HandleMessage to trigger logging middleware.
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: toolCallParams{
			Name:      "session_list",
			Arguments: map[string]any{},
		},
		ID: 1,
	}
	_ = handleMessageWithUserID(t, srv, "user-log-test", req)

	require.Contains(t, buf.String(), "mcp tool call", "logger should capture tool call log")
}

// --- Handler tests ---

func TestHandler_ReturnsHTTPHandler(t *testing.T) {
	srv := newServer(&mockAgentService{})
	jwtMgr := newJWTManager()

	handler := srv.Handler(jwtMgr, "")
	require.NotNil(t, handler)

	// Send a POST with invalid JSON to verify the handler processes it
	// without blocking (unlike GET which uses SSE long-polling).
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte("not-json")))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	require.NotNil(t, resp)
	// Invalid JSON on a new session should produce a client error.
	require.GreaterOrEqual(t, resp.StatusCode, 400)
}

func TestHandler_AuthExtraction_ValidJWT(t *testing.T) {
	mock := &mockAgentService{
		sessionListFn: func(_ context.Context, userID, _ string, _ int) ([]*agent.Session, error) {
			return []*agent.Session{
				{ID: "s1", Name: "test-session", Status: "active"},
			}, nil
		},
	}
	srv := newServer(mock)
	jwtMgr := newJWTManager()
	handler := srv.Handler(jwtMgr, "")

	token, err := jwtMgr.GenerateAccessToken("user-abc", "testuser")
	require.NoError(t, err)

	authHeader := "Bearer " + token

	// Initialize session with valid JWT.
	sessionID := initMCPSession(t, handler, authHeader)

	// Make a tool call with the session ID and valid JWT.
	respBody := httpToolCall(t, handler, sessionID, authHeader, "session_list", map[string]any{})

	// A successful tool call with valid auth should not contain "unauthorized".
	require.NotContains(t, respBody, "unauthorized",
		"valid JWT should not produce unauthorized error, got: %s", respBody)
}

func TestHandler_AuthExtraction_NoHeader_Unauthorized(t *testing.T) {
	mock := &mockAgentService{
		sessionListFn: func(context.Context, string, string, int) ([]*agent.Session, error) {
			t.Fatal("service should not be called without authentication")
			return nil, nil
		},
	}
	srv := newServer(mock)
	jwtMgr := newJWTManager()
	handler := srv.Handler(jwtMgr, "")

	// Initialize session without auth (auth extraction sets no user ID).
	sessionID := initMCPSession(t, handler, "")

	// Make a tool call without Authorization header.
	respBody := httpToolCall(t, handler, sessionID, "", "session_list", map[string]any{})

	require.Contains(t, respBody, "unauthorized",
		"missing auth header should produce unauthorized error, got: %s", respBody)
}

func TestHandler_AuthExtraction_InvalidToken_Unauthorized(t *testing.T) {
	mock := &mockAgentService{
		sessionListFn: func(context.Context, string, string, int) ([]*agent.Session, error) {
			t.Fatal("service should not be called with invalid token")
			return nil, nil
		},
	}
	srv := newServer(mock)
	jwtMgr := newJWTManager()
	handler := srv.Handler(jwtMgr, "")

	authHeader := "Bearer invalid-token-garbage"

	// Initialize session (auth failure at initialize is fine -- the server still creates a session).
	sessionID := initMCPSession(t, handler, authHeader)

	// Make a tool call with invalid JWT.
	respBody := httpToolCall(t, handler, sessionID, authHeader, "session_list", map[string]any{})

	require.Contains(t, respBody, "unauthorized",
		"invalid JWT should produce unauthorized error, got: %s", respBody)
}

func TestHandler_AuthExtraction_WrongScheme_Unauthorized(t *testing.T) {
	mock := &mockAgentService{
		sessionListFn: func(context.Context, string, string, int) ([]*agent.Session, error) {
			t.Fatal("service should not be called with wrong auth scheme")
			return nil, nil
		},
	}
	srv := newServer(mock)
	jwtMgr := newJWTManager()
	handler := srv.Handler(jwtMgr, "")

	authHeader := "Basic dXNlcjpwYXNz"

	// Initialize session with wrong scheme.
	sessionID := initMCPSession(t, handler, authHeader)

	// Make a tool call with Basic auth (should fail).
	respBody := httpToolCall(t, handler, sessionID, authHeader, "session_list", map[string]any{})

	require.Contains(t, respBody, "unauthorized",
		"Basic auth scheme should produce unauthorized error, got: %s", respBody)
}

func TestHandler_AuthExtraction_StaticAPIKey(t *testing.T) {
	mock := &mockAgentService{
		sessionListFn: func(_ context.Context, userID, _ string, _ int) ([]*agent.Session, error) {
			return []*agent.Session{
				{ID: "s1", Name: "test-session", Status: "active"},
			}, nil
		},
	}
	srv := newServer(mock)
	jwtMgr := newJWTManager()
	apiKey := "test-static-api-key-1234567890abcdef"
	handler := srv.Handler(jwtMgr, apiKey)

	authHeader := "Bearer " + apiKey

	sessionID := initMCPSession(t, handler, authHeader)

	respBody := httpToolCall(t, handler, sessionID, authHeader, "session_list", map[string]any{})

	require.NotContains(t, respBody, "unauthorized",
		"valid API key should not produce unauthorized error, got: %s", respBody)
}

func TestHandler_AuthExtraction_WrongAPIKey_FallsThrough(t *testing.T) {
	mock := &mockAgentService{
		sessionListFn: func(context.Context, string, string, int) ([]*agent.Session, error) {
			t.Fatal("service should not be called with wrong API key and no valid JWT")
			return nil, nil
		},
	}
	srv := newServer(mock)
	jwtMgr := newJWTManager()
	handler := srv.Handler(jwtMgr, "correct-key")

	authHeader := "Bearer wrong-key"

	sessionID := initMCPSession(t, handler, authHeader)

	respBody := httpToolCall(t, handler, sessionID, authHeader, "session_list", map[string]any{})

	require.Contains(t, respBody, "unauthorized",
		"wrong API key (and no valid JWT) should produce unauthorized error, got: %s", respBody)
}

// --- Auth middleware tests via HandleMessage ---

func TestAuthCheckMiddleware_WithUserID_Passes(t *testing.T) {
	mock := &mockAgentService{
		sessionListFn: func(_ context.Context, userID, _ string, _ int) ([]*agent.Session, error) {
			require.Equal(t, "user-ok", userID)
			return []*agent.Session{}, nil
		},
	}
	srv := newServer(mock)

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: toolCallParams{
			Name:      "session_list",
			Arguments: map[string]any{},
		},
		ID: 1,
	}

	result := handleMessageWithUserID(t, srv, "user-ok", req)
	require.NotNil(t, result)

	respBytes, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(respBytes), "unauthorized",
		"authenticated context should not produce unauthorized error")
}

func TestAuthCheckMiddleware_WithoutUserID_ReturnsUnauthorized(t *testing.T) {
	mock := &mockAgentService{
		sessionListFn: func(context.Context, string, string, int) ([]*agent.Session, error) {
			t.Fatal("service should not be called without user ID")
			return nil, nil
		},
	}
	srv := newServer(mock)

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: toolCallParams{
			Name:      "session_list",
			Arguments: map[string]any{},
		},
		ID: 1,
	}

	result := handleMessageWithUserID(t, srv, "", req)
	require.NotNil(t, result)

	respBytes, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(respBytes), "unauthorized",
		"empty user ID context should produce unauthorized error")
}

func TestAuthCheckMiddleware_AllTools_Unauthorized(t *testing.T) {
	// Verify that every registered tool rejects calls without user ID.
	srv := newServer(&mockAgentService{})

	tests := []struct {
		name      string
		toolName  string
		arguments map[string]any
	}{
		{"session_start", "session_start", map[string]any{"name": "x"}},
		{"session_end", "session_end", map[string]any{"session_name": "x", "findings": "x"}},
		{"session_list", "session_list", map[string]any{}},
		{"session_plan_set", "session_plan_set", map[string]any{"session_name": "x", "content": "x"}},
		{"session_progress_update", "session_progress_update", map[string]any{"session_name": "x", "task": "x", "status": "pending"}},
		{"session_context_set", "session_context_set", map[string]any{"session_name": "x", "content": "x"}},
		{"memory_read", "memory_read", map[string]any{"category": "x", "name": "x"}},
		{"memory_write", "memory_write", map[string]any{"category": "x", "name": "x", "content": "x"}},
		{"memory_append", "memory_append", map[string]any{"category": "x", "name": "x", "content": "x"}},
		{"memory_list", "memory_list", map[string]any{}},
		{"memory_delete", "memory_delete", map[string]any{"category": "x", "name": "x"}},
		{"context_gather", "context_gather", map[string]any{"query": "x"}},
		{"notes_search", "notes_search", map[string]any{"query": "x"}},
		{"notes_read", "notes_read", map[string]any{"id": "x"}},
		{"notes_list", "notes_list", map[string]any{}},
		{"notes_create", "notes_create", map[string]any{"title": "x", "body": "x"}},
		{"notes_update", "notes_update", map[string]any{"id": "x", "title": "new"}},
		{"notes_delete", "notes_delete", map[string]any{"id": "x"}},
		{"notes_tags", "notes_tags", map[string]any{}},
		{"notes_daily", "notes_daily", map[string]any{}},
		{"notes_append", "notes_append", map[string]any{"id": "x", "text": "x"}},
		{"notes_changelog", "notes_changelog", map[string]any{}},
		{"notes_versions", "notes_versions", map[string]any{"id": "x"}},
		{"project_list", "project_list", map[string]any{}},
		{"project_create", "project_create", map[string]any{"name": "x"}},
		{"memory_search", "memory_search", map[string]any{"query": "x"}},
		{"session_metrics", "session_metrics", map[string]any{"session_name": "x"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := jsonrpcRequest{
				JSONRPC: "2.0",
				Method:  "tools/call",
				Params: toolCallParams{
					Name:      tc.toolName,
					Arguments: tc.arguments,
				},
				ID: 1,
			}

			result := handleMessageWithUserID(t, srv, "", req)

			respBytes, err := json.Marshal(result)
			require.NoError(t, err)
			require.Contains(t, string(respBytes), "unauthorized",
				"tool %q should return unauthorized without user ID", tc.toolName)
		})
	}
}

// --- MCPServer accessor test ---

func TestMCPServer_Accessor_ReturnsSameInstance(t *testing.T) {
	srv := newServer(&mockAgentService{})

	mcpSrv := srv.MCPServer()
	require.NotNil(t, mcpSrv)

	// Multiple calls return the same instance.
	require.Equal(t, mcpSrv, srv.MCPServer(),
		"MCPServer() should return the same instance on repeated calls")
}
