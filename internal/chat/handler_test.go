package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/reqctx"
)

// mockChatService implements ServiceInterface for handler tests.
type mockChatService struct {
	createConversation func(ctx context.Context, userID string) (*Conversation, error)
	listConversations  func(ctx context.Context, userID string, limit, offset int) ([]Conversation, int, error)
	getConversation    func(ctx context.Context, userID, conversationID string) (*Conversation, []Message, error)
	deleteConversation func(ctx context.Context, userID, conversationID string) error
	addMessage         func(ctx context.Context, userID string, msg Message) error
}

func (m *mockChatService) CreateConversation(ctx context.Context, userID string) (*Conversation, error) {
	if m.createConversation != nil {
		return m.createConversation(ctx, userID)
	}
	return &Conversation{ID: "conv1"}, nil
}

func (m *mockChatService) ListConversations(ctx context.Context, userID string, limit, offset int) ([]Conversation, int, error) {
	if m.listConversations != nil {
		return m.listConversations(ctx, userID, limit, offset)
	}
	return nil, 0, nil
}

func (m *mockChatService) GetConversation(ctx context.Context, userID, conversationID string) (*Conversation, []Message, error) {
	if m.getConversation != nil {
		return m.getConversation(ctx, userID, conversationID)
	}
	return nil, nil, ErrNotFound
}

func (m *mockChatService) DeleteConversation(ctx context.Context, userID, conversationID string) error {
	if m.deleteConversation != nil {
		return m.deleteConversation(ctx, userID, conversationID)
	}
	return nil
}

func (m *mockChatService) AddMessage(ctx context.Context, userID string, msg Message) error {
	if m.addMessage != nil {
		return m.addMessage(ctx, userID, msg)
	}
	return nil
}

func newTestChatRouter(svc ServiceInterface) *chi.Mux {
	h := NewHandler(svc, nil)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := reqctx.WithUserID(r.Context(), "test-user")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Mount("/chat", h.Routes())
	return r
}

func TestHandler_CreateConversation(t *testing.T) {
	r := newTestChatRouter(&mockChatService{})

	req := httptest.NewRequest(http.MethodPost, "/chat/conversations", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
}

func TestHandler_ListConversations(t *testing.T) {
	svc := &mockChatService{
		listConversations: func(_ context.Context, _ string, _, _ int) ([]Conversation, int, error) {
			return []Conversation{{ID: "c1", Title: "test"}}, 1, nil
		},
	}
	r := newTestChatRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/chat/conversations", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "1", rec.Header().Get("X-Total-Count"))
}

func TestHandler_GetConversation_NotFound(t *testing.T) {
	r := newTestChatRouter(&mockChatService{})

	req := httptest.NewRequest(http.MethodGet, "/chat/conversations/nonexistent", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandler_AddMessage_InvalidRole(t *testing.T) {
	svc := &mockChatService{
		addMessage: func(_ context.Context, _ string, _ Message) error {
			return ErrInvalidRole
		},
	}
	r := newTestChatRouter(svc)

	body, _ := json.Marshal(map[string]string{"role": "system", "content": "bad"})
	req := httptest.NewRequest(http.MethodPost, "/chat/conversations/conv1/messages", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_AddMessage_Valid(t *testing.T) {
	var capturedMsg Message
	svc := &mockChatService{
		addMessage: func(_ context.Context, _ string, msg Message) error {
			capturedMsg = msg
			return nil
		},
	}
	r := newTestChatRouter(svc)

	body, _ := json.Marshal(map[string]string{"role": "user", "content": "hello"})
	req := httptest.NewRequest(http.MethodPost, "/chat/conversations/conv1/messages", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, "user", capturedMsg.Role)
	require.Equal(t, "hello", capturedMsg.Content)
}

func TestHandler_AddMessage_AcceptsToolAndSystemRoles(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		verify  func(t *testing.T, msg Message)
	}{
		{
			name: "tool_role_with_result",
			payload: map[string]any{
				"role":         "tool",
				"content":      `{"results":[]}`,
				"tool_call_id": "call_1",
				"tool_name":    "search_notes",
				"iteration":    2,
			},
			verify: func(t *testing.T, msg Message) {
				require.Equal(t, "tool", msg.Role)
				require.Equal(t, `{"results":[]}`, msg.Content)
				require.Equal(t, "call_1", msg.ToolCallID)
				require.Equal(t, "search_notes", msg.ToolName)
				require.Equal(t, 2, msg.Iteration)
			},
		},
		{
			name: "tool_role_empty_content_allowed",
			payload: map[string]any{
				"role":         "tool",
				"content":      "",
				"tool_call_id": "call_2",
				"tool_name":    "get_current_time",
			},
			verify: func(t *testing.T, msg Message) {
				require.Equal(t, "tool", msg.Role)
				require.Equal(t, "", msg.Content)
				require.Equal(t, "call_2", msg.ToolCallID)
			},
		},
		{
			name: "system_role",
			payload: map[string]any{
				"role":    "system",
				"content": "pending confirmation",
			},
			verify: func(t *testing.T, msg Message) {
				require.Equal(t, "system", msg.Role)
				require.Equal(t, "pending confirmation", msg.Content)
			},
		},
		{
			name: "assistant_with_tool_calls",
			payload: map[string]any{
				"role":    "assistant",
				"content": "looking it up",
				"tool_calls": []map[string]string{
					{"id": "call_1", "name": "search_notes", "arguments": `{"q":"k8s"}`},
				},
				"iteration": 1,
			},
			verify: func(t *testing.T, msg Message) {
				require.Equal(t, "assistant", msg.Role)
				require.Equal(t, "looking it up", msg.Content)
				require.Len(t, msg.ToolCalls, 1)
				require.Equal(t, "call_1", msg.ToolCalls[0].ID)
				require.Equal(t, "search_notes", msg.ToolCalls[0].Name)
				require.Equal(t, `{"q":"k8s"}`, msg.ToolCalls[0].Arguments)
				require.Equal(t, 1, msg.Iteration)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var captured Message
			svc := &mockChatService{
				addMessage: func(_ context.Context, _ string, msg Message) error {
					captured = msg
					return nil
				},
			}
			r := newTestChatRouter(svc)

			body, err := json.Marshal(tc.payload)
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/chat/conversations/conv1/messages", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
			tc.verify(t, captured)
		})
	}
}

func TestHandler_AddMessage_RejectsUnknownRole(t *testing.T) {
	var addMessageCalled bool
	svc := &mockChatService{
		addMessage: func(_ context.Context, _ string, _ Message) error {
			addMessageCalled = true
			return nil
		},
	}
	r := newTestChatRouter(svc)

	body, err := json.Marshal(map[string]string{"role": "banana", "content": "weird"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/chat/conversations/conv1/messages", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.False(t, addMessageCalled, "service should not be reached for an invalid role")
}

func TestHandler_AddMessage_RejectsEmptyContentForNonTool(t *testing.T) {
	r := newTestChatRouter(&mockChatService{})

	body, err := json.Marshal(map[string]string{"role": "user", "content": ""})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/chat/conversations/conv1/messages", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_Unauthorized(t *testing.T) {
	h := NewHandler(&mockChatService{}, nil)
	r := chi.NewRouter()
	r.Mount("/chat", h.Routes())

	// No user ID middleware -- should get 401.
	req := httptest.NewRequest(http.MethodGet, "/chat/conversations", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
