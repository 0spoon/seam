package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/internal/reqctx"
	"github.com/stretchr/testify/require"
)

func setupTestHandler(t *testing.T) (*Handler, *chi.Mux) {
	t.Helper()

	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	mock := &mockToolChatCompleter{
		responses: []*ai.ToolChatResponse{
			{Content: "Test response", FinishReason: "stop"},
		},
	}

	registry := NewToolRegistry()
	registry.Register(&Tool{
		Name:        "get_current_time",
		Description: "Get the current time",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"time":"now"}`), nil
		},
		ReadOnly: true,
	})

	svc := newTestService(t, db, mock, registry, ServiceConfig{MaxIterations: 10})
	handler := NewHandler(svc, slog.Default())

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := reqctx.WithUserID(r.Context(), "default")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Mount("/api/assistant", handler.Routes())

	return handler, r
}

func TestHandler_Chat_Success(t *testing.T) {
	_, router := setupTestHandler(t)

	body := chatRequest{ConversationID: "conv1", Message: "Hello!"}
	bodyJSON, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/assistant/chat", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp ChatResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "Test response", resp.Response)
}

func TestHandler_Chat_EmptyMessage(t *testing.T) {
	_, router := setupTestHandler(t)

	body := chatRequest{ConversationID: "conv1", Message: ""}
	bodyJSON, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/assistant/chat", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_Chat_MissingConversationID(t *testing.T) {
	_, router := setupTestHandler(t)

	body := chatRequest{Message: "Hello!"}
	bodyJSON, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/assistant/chat", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_Chat_Unauthorized(t *testing.T) {
	db := setupTestDB(t)

	mock := &mockToolChatCompleter{
		responses: []*ai.ToolChatResponse{
			{Content: "ok", FinishReason: "stop"},
		},
	}

	svc := newTestService(t, db, mock, NewToolRegistry(), ServiceConfig{MaxIterations: 10})
	handler := NewHandler(svc, slog.Default())

	// No auth middleware -- no user ID in context.
	r := chi.NewRouter()
	r.Mount("/api/assistant", handler.Routes())

	body := chatRequest{Message: "hello", ConversationID: "conv1"}
	bodyJSON, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/assistant/chat", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_ChatStream_Success(t *testing.T) {
	_, router := setupTestHandler(t)

	body := chatRequest{ConversationID: "conv1", Message: "Hello!"}
	bodyJSON, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/assistant/chat/stream", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Body.String(), "data:")
}

func TestHandler_ListActions_Success(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	// Insert a test action directly.
	_, err = db.Exec(
		`INSERT INTO assistant_actions (id, conversation_id, tool_name, tool_call_id, iteration, arguments, result, status, created_at)
		 VALUES ('act1', 'conv1', 'search_notes', 'call_1', 0, '{"query":"test"}', '{"results":[]}', 'executed', '2026-03-16T10:00:00Z')`,
	)
	require.NoError(t, err)

	mock := &mockToolChatCompleter{
		responses: []*ai.ToolChatResponse{
			{Content: "ok", FinishReason: "stop"},
		},
	}

	svc := newTestService(t, db, mock, NewToolRegistry(), ServiceConfig{MaxIterations: 10})
	handler := NewHandler(svc, slog.Default())

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := reqctx.WithUserID(r.Context(), "default")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Mount("/api/assistant", handler.Routes())

	req := httptest.NewRequest(http.MethodGet, "/api/assistant/conversations/conv1/actions", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Actions []*Action `json:"actions"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Actions, 1)
	require.Equal(t, "act1", resp.Actions[0].ID)
	require.Equal(t, "search_notes", resp.Actions[0].ToolName)
	require.Equal(t, ActionStatusExecuted, resp.Actions[0].Status)
}

func TestHandler_ListActions_Unauthorized(t *testing.T) {
	db := setupTestDB(t)

	mock := &mockToolChatCompleter{
		responses: []*ai.ToolChatResponse{
			{Content: "ok", FinishReason: "stop"},
		},
	}

	svc := newTestService(t, db, mock, NewToolRegistry(), ServiceConfig{MaxIterations: 10})
	handler := NewHandler(svc, slog.Default())

	r := chi.NewRouter()
	r.Mount("/api/assistant", handler.Routes())

	req := httptest.NewRequest(http.MethodGet, "/api/assistant/conversations/conv1/actions", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_ApproveAction(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	// Insert a pending action.
	_, err = db.Exec(
		`INSERT INTO assistant_actions (id, conversation_id, tool_name, tool_call_id, iteration, arguments, status, created_at)
		 VALUES ('act_pending', 'conv1', 'create_note', 'call_pending', 0, '{"title":"Test"}', 'pending', '2026-03-16T10:00:00Z')`,
	)
	require.NoError(t, err)

	mock := &mockToolChatCompleter{}

	registry := NewToolRegistry()
	registry.Register(&Tool{
		Name:        "create_note",
		Description: "Create a note",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"id":"note1"}`), nil
		},
		ReadOnly: false,
	})

	svc := newTestService(t, db, mock, registry, ServiceConfig{MaxIterations: 10})
	handler := NewHandler(svc, slog.Default())

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := reqctx.WithUserID(r.Context(), "default")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Mount("/api/assistant", handler.Routes())

	req := httptest.NewRequest(http.MethodPost, "/api/assistant/actions/act_pending/approve", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var result ToolResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Equal(t, "create_note", result.ToolName)
	require.Empty(t, result.Error)
}

func TestHandler_RejectAction(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	_, err = db.Exec(
		`INSERT INTO assistant_actions (id, conversation_id, tool_name, tool_call_id, iteration, arguments, status, created_at)
		 VALUES ('act_pending', 'conv1', 'create_note', 'call_pending', 0, '{}', 'pending', '2026-03-16T10:00:00Z')`,
	)
	require.NoError(t, err)

	mock := &mockToolChatCompleter{}
	svc := newTestService(t, db, mock, NewToolRegistry(), ServiceConfig{MaxIterations: 10})
	handler := NewHandler(svc, slog.Default())

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := reqctx.WithUserID(r.Context(), "default")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Mount("/api/assistant", handler.Routes())

	req := httptest.NewRequest(http.MethodPost, "/api/assistant/actions/act_pending/reject", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestHandler_ResumeAction_StreamsEvents(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	mock := &mockToolChatCompleter{
		responses: []*ai.ToolChatResponse{
			// Iteration 0: assistant envelope with the write tool call.
			{
				Content:      "I'll create the note.",
				FinishReason: "tool_calls",
				ToolCalls: []ai.ToolCall{
					{ID: "call_create", Name: "create_note", Arguments: `{"title":"X"}`},
				},
			},
			// Iteration 1 (after resume): final text.
			{Content: "Note created.", FinishReason: "stop"},
		},
	}

	registry := NewToolRegistry()
	registry.Register(&Tool{
		Name:        "create_note",
		Description: "Create a note",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"id":"n1"}`), nil
		},
		ReadOnly: false,
	})

	svc := newTestService(t, db, mock, registry, ServiceConfig{
		MaxIterations:        10,
		ConfirmationRequired: []string{"create_note"},
	})
	handler := NewHandler(svc, slog.Default())

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := reqctx.WithUserID(r.Context(), "default")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Mount("/api/assistant", handler.Routes())

	// First, drive ChatStream to a confirmation so an action exists.
	chatBody := chatRequest{ConversationID: "conv1", Message: "create a note"}
	chatJSON, err := json.Marshal(chatBody)
	require.NoError(t, err)
	chatReq := httptest.NewRequest(http.MethodPost, "/api/assistant/chat/stream", bytes.NewReader(chatJSON))
	chatReq.Header.Set("Content-Type", "application/json")
	chatRec := httptest.NewRecorder()
	r.ServeHTTP(chatRec, chatReq)
	require.Equal(t, http.StatusOK, chatRec.Code)

	// Read the action ID by listing actions.
	listReq := httptest.NewRequest(http.MethodGet, "/api/assistant/conversations/conv1/actions", nil)
	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, listReq)
	require.Equal(t, http.StatusOK, listRec.Code)
	var listResp struct {
		Actions []*Action `json:"actions"`
	}
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &listResp))
	require.Len(t, listResp.Actions, 1)
	actionID := listResp.Actions[0].ID
	require.Equal(t, ActionStatusPending, listResp.Actions[0].Status)

	// Now resume via the SSE endpoint.
	resumeReq := httptest.NewRequest(http.MethodPost, "/api/assistant/actions/"+actionID+"/resume", nil)
	resumeRec := httptest.NewRecorder()
	r.ServeHTTP(resumeRec, resumeReq)

	require.Equal(t, http.StatusOK, resumeRec.Code)
	require.Equal(t, "text/event-stream", resumeRec.Header().Get("Content-Type"))
	body := resumeRec.Body.String()
	require.Contains(t, body, "data:")
	require.Contains(t, body, "tool_use")
	require.Contains(t, body, "Note created.")
	require.Contains(t, body, "[DONE]")
}
