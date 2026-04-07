package assistant

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/migrations"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// mockToolChatCompleter is a mock LLM that returns predefined responses.
type mockToolChatCompleter struct {
	responses []*ai.ToolChatResponse
	callCount int
	messages  [][]ai.ToolMessage // records messages from each call
}

func (m *mockToolChatCompleter) ChatCompletionWithTools(ctx context.Context, model string, messages []ai.ToolMessage, tools []ai.ToolDefinition) (*ai.ToolChatResponse, error) {
	m.messages = append(m.messages, messages)

	if m.callCount >= len(m.responses) {
		return &ai.ToolChatResponse{
			Content:      "max responses exceeded",
			FinishReason: "stop",
		}, nil
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

// mockUserDBManager is a simplified mock that returns an in-memory database.
type mockUserDBManager struct {
	db       *sql.DB
	notesDir string
}

func (m *mockUserDBManager) Open(ctx context.Context, userID string) (*sql.DB, error) {
	return m.db, nil
}

func (m *mockUserDBManager) Close(userID string) error { return nil }
func (m *mockUserDBManager) CloseAll() error           { return nil }
func (m *mockUserDBManager) UserNotesDir(userID string) string {
	return m.notesDir
}
func (m *mockUserDBManager) UserDataDir(userID string) string {
	return m.notesDir
}
func (m *mockUserDBManager) ListUsers(ctx context.Context) ([]string, error) {
	return []string{"default"}, nil
}
func (m *mockUserDBManager) EnsureUserDirs(userID string) error { return nil }

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	require.NoError(t, err)

	_, err = db.Exec("PRAGMA journal_mode=WAL")
	require.NoError(t, err)
	_, err = db.Exec("PRAGMA foreign_keys=ON")
	require.NoError(t, err)

	for _, m := range migrations.Migrations() {
		_, err = db.Exec(m.SQL)
		require.NoError(t, err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

// newTestService creates a service with no ws.Hub (nil is safe since the
// service only uses the hub for optional WebSocket notifications).
func newTestService(t *testing.T, db *sql.DB, mock *mockToolChatCompleter, registry *ToolRegistry, cfg ServiceConfig) *Service {
	t.Helper()
	return NewService(ServiceDeps{
		Store:         NewStore(),
		MemoryStore:   NewMemoryStore(),
		ProfileStore:  NewProfileStore(),
		Registry:      registry,
		LLM:           mock,
		ChatModel:     "test-model",
		UserDBManager: &mockUserDBManager{db: db},
		Hub:           nil,
		Logger:        slog.Default(),
		Config:        cfg,
	})
}

func TestService_Chat_SimpleResponse(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	mock := &mockToolChatCompleter{
		responses: []*ai.ToolChatResponse{
			{Content: "Hello! How can I help you?", FinishReason: "stop"},
		},
	}

	registry := NewToolRegistry()
	registry.Register(&Tool{
		Name:        "search_notes",
		Description: "Search notes",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"results":[]}`), nil
		},
		ReadOnly: true,
	})

	svc := newTestService(t, db, mock, registry, ServiceConfig{MaxIterations: 10})

	resp, err := svc.Chat(context.Background(), ChatRequest{
		UserID:         "default",
		ConversationID: "conv1",
		Message:        "Hello!",
	})

	require.NoError(t, err)
	require.Equal(t, "Hello! How can I help you?", resp.Response)
	require.Empty(t, resp.ToolsUsed)
	require.Equal(t, 1, resp.Iterations)
	require.Nil(t, resp.Confirmation)
}

func TestService_Chat_WithToolCalls(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	mock := &mockToolChatCompleter{
		responses: []*ai.ToolChatResponse{
			{
				Content:      "",
				FinishReason: "tool_calls",
				ToolCalls: []ai.ToolCall{
					{ID: "call_1", Name: "get_current_time", Arguments: `{}`},
				},
			},
			{Content: "The current date is 2026-03-16.", FinishReason: "stop"},
		},
	}

	registry := NewToolRegistry()
	registry.Register(&Tool{
		Name:        "get_current_time",
		Description: "Get the current time",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"datetime":"2026-03-16T10:00:00Z","day_of_week":"Monday"}`), nil
		},
		ReadOnly: true,
	})

	svc := newTestService(t, db, mock, registry, ServiceConfig{MaxIterations: 10})

	resp, err := svc.Chat(context.Background(), ChatRequest{
		UserID: "default", ConversationID: "conv1", Message: "What day is it?",
	})

	require.NoError(t, err)
	require.Equal(t, "The current date is 2026-03-16.", resp.Response)
	require.Len(t, resp.ToolsUsed, 1)
	require.Equal(t, "get_current_time", resp.ToolsUsed[0].ToolName)
	require.Empty(t, resp.ToolsUsed[0].Error)
	require.Equal(t, 2, resp.Iterations)

	// Verify tool result was sent back to LLM.
	require.Len(t, mock.messages, 2)
	var foundToolResult bool
	for _, m := range mock.messages[1] {
		if m.Role == "tool" && m.ToolCallID == "call_1" {
			foundToolResult = true
			require.Contains(t, m.Content, "2026-03-16")
		}
	}
	require.True(t, foundToolResult, "expected tool result message in second LLM call")
}

func TestService_Chat_MaxIterations(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	infiniteToolCalls := make([]*ai.ToolChatResponse, 5)
	for i := range infiniteToolCalls {
		infiniteToolCalls[i] = &ai.ToolChatResponse{
			Content:      "",
			FinishReason: "tool_calls",
			ToolCalls: []ai.ToolCall{
				{ID: fmt.Sprintf("call_%d", i), Name: "get_current_time", Arguments: `{}`},
			},
		}
	}

	mock := &mockToolChatCompleter{responses: infiniteToolCalls}

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

	svc := newTestService(t, db, mock, registry, ServiceConfig{MaxIterations: 3})

	_, err = svc.Chat(context.Background(), ChatRequest{
		UserID: "default", ConversationID: "conv1", Message: "loop forever",
	})

	require.ErrorIs(t, err, ErrMaxIterations)
}

func TestService_Chat_ToolNotFound(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	mock := &mockToolChatCompleter{
		responses: []*ai.ToolChatResponse{
			{
				Content:      "",
				FinishReason: "tool_calls",
				ToolCalls:    []ai.ToolCall{{ID: "call_1", Name: "nonexistent_tool", Arguments: `{}`}},
			},
			{Content: "I could not find that tool, but here is my answer.", FinishReason: "stop"},
		},
	}

	registry := NewToolRegistry() // Empty registry

	svc := newTestService(t, db, mock, registry, ServiceConfig{MaxIterations: 10})

	resp, err := svc.Chat(context.Background(), ChatRequest{
		UserID: "default", ConversationID: "conv1", Message: "Use some tool",
	})

	require.NoError(t, err)
	require.Equal(t, "I could not find that tool, but here is my answer.", resp.Response)
	require.Len(t, resp.ToolsUsed, 1)
	require.Contains(t, resp.ToolsUsed[0].Error, "tool not found")
}

func TestService_Chat_ConfirmationRequired(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	mock := &mockToolChatCompleter{
		responses: []*ai.ToolChatResponse{
			{
				Content:      "I will create a note for you.",
				FinishReason: "tool_calls",
				ToolCalls: []ai.ToolCall{
					{ID: "call_1", Name: "create_note", Arguments: `{"title":"Test","body":"hello"}`},
				},
			},
		},
	}

	registry := NewToolRegistry()
	registry.Register(&Tool{
		Name:        "create_note",
		Description: "Create a note",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"id":"note1","title":"Test"}`), nil
		},
		ReadOnly: false, // write operation
	})

	svc := newTestService(t, db, mock, registry, ServiceConfig{
		MaxIterations:        10,
		ConfirmationRequired: []string{"create_note"},
	})

	resp, err := svc.Chat(context.Background(), ChatRequest{
		UserID: "default", ConversationID: "conv1", Message: "Create a note",
	})

	require.NoError(t, err)
	// Should return a confirmation prompt instead of executing.
	require.NotNil(t, resp.Confirmation, "expected confirmation prompt")
	require.Equal(t, "create_note", resp.Confirmation.ToolName)
	require.NotEmpty(t, resp.Confirmation.ActionID)
	require.Contains(t, resp.Confirmation.Message, "create_note")
	// The tool should NOT have been executed (no tools used).
	require.Empty(t, resp.ToolsUsed)
}

func TestService_ApproveAction(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	// First get a pending action by triggering confirmation.
	mock := &mockToolChatCompleter{
		responses: []*ai.ToolChatResponse{
			{
				Content:      "",
				FinishReason: "tool_calls",
				ToolCalls:    []ai.ToolCall{{ID: "call_1", Name: "create_note", Arguments: `{"title":"Test","body":"hello"}`}},
			},
		},
	}

	var toolExecuted bool
	registry := NewToolRegistry()
	registry.Register(&Tool{
		Name:        "create_note",
		Description: "Create a note",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			toolExecuted = true
			return json.RawMessage(`{"id":"note1","title":"Test"}`), nil
		},
		ReadOnly: false,
	})

	svc := newTestService(t, db, mock, registry, ServiceConfig{
		MaxIterations:        10,
		ConfirmationRequired: []string{"create_note"},
	})

	// Trigger confirmation.
	resp, err := svc.Chat(context.Background(), ChatRequest{
		UserID: "default", ConversationID: "conv1", Message: "Create a note",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Confirmation)
	require.False(t, toolExecuted, "tool should not have been executed yet")

	actionID := resp.Confirmation.ActionID

	// Now approve the action.
	result, err := svc.ApproveAction(context.Background(), "default", actionID)
	require.NoError(t, err)
	require.True(t, toolExecuted, "tool should have been executed after approval")
	require.Empty(t, result.Error)
	require.Equal(t, "create_note", result.ToolName)
}

func TestService_RejectAction(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	mock := &mockToolChatCompleter{
		responses: []*ai.ToolChatResponse{
			{
				Content:      "",
				FinishReason: "tool_calls",
				ToolCalls:    []ai.ToolCall{{ID: "call_1", Name: "create_note", Arguments: `{}`}},
			},
		},
	}

	registry := NewToolRegistry()
	registry.Register(&Tool{
		Name:        "create_note",
		Description: "Create a note",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			t.Fatal("tool should not be executed after rejection")
			return nil, nil
		},
		ReadOnly: false,
	})

	svc := newTestService(t, db, mock, registry, ServiceConfig{
		MaxIterations:        10,
		ConfirmationRequired: []string{"create_note"},
	})

	resp, err := svc.Chat(context.Background(), ChatRequest{
		UserID: "default", ConversationID: "conv1", Message: "Create a note",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Confirmation)

	// Reject the action.
	err = svc.RejectAction(context.Background(), "default", resp.Confirmation.ActionID)
	require.NoError(t, err)
}

func TestService_Chat_WriteWithoutConfirmation(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	mock := &mockToolChatCompleter{
		responses: []*ai.ToolChatResponse{
			{
				Content:      "",
				FinishReason: "tool_calls",
				ToolCalls:    []ai.ToolCall{{ID: "call_1", Name: "toggle_task", Arguments: `{"task_id":"t1","done":true}`}},
			},
			{Content: "Done!", FinishReason: "stop"},
		},
	}

	var toolExecuted bool
	registry := NewToolRegistry()
	registry.Register(&Tool{
		Name:        "toggle_task",
		Description: "Toggle task",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			toolExecuted = true
			return json.RawMessage(`{"ok":true}`), nil
		},
		ReadOnly: false, // write operation, but NOT in confirmation list
	})

	svc := newTestService(t, db, mock, registry, ServiceConfig{
		MaxIterations:        10,
		ConfirmationRequired: []string{"create_note"}, // toggle_task not listed
	})

	resp, err := svc.Chat(context.Background(), ChatRequest{
		UserID: "default", ConversationID: "conv1", Message: "mark task done",
	})

	require.NoError(t, err)
	require.Nil(t, resp.Confirmation, "should not need confirmation")
	require.True(t, toolExecuted, "tool should execute without confirmation")
	require.Equal(t, "Done!", resp.Response)
}

func TestService_ChatStream_EmitsToolEventsInline(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	mock := &mockToolChatCompleter{
		responses: []*ai.ToolChatResponse{
			{
				Content:      "",
				FinishReason: "tool_calls",
				ToolCalls:    []ai.ToolCall{{ID: "call_1", Name: "get_current_time", Arguments: `{}`}},
			},
			{Content: "It is Monday.", FinishReason: "stop"},
		},
	}

	registry := NewToolRegistry()
	registry.Register(&Tool{
		Name:        "get_current_time",
		Description: "Get the current time",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"day":"Monday"}`), nil
		},
		ReadOnly: true,
	})

	svc := newTestService(t, db, mock, registry, ServiceConfig{MaxIterations: 10})

	eventCh, err := svc.ChatStream(context.Background(), ChatRequest{
		UserID: "default", ConversationID: "conv1", Message: "What day?",
	})
	require.NoError(t, err)

	var events []StreamEvent
	for event := range eventCh {
		events = append(events, event)
	}

	// Expect: tool_use, text, done (in order).
	require.GreaterOrEqual(t, len(events), 3)

	require.Equal(t, StreamEventToolUse, events[0].Type)
	require.Equal(t, "get_current_time", events[0].ToolName)

	require.Equal(t, StreamEventText, events[1].Type)
	require.Equal(t, "It is Monday.", events[1].Content)

	require.Equal(t, StreamEventDone, events[2].Type)
	require.Equal(t, 2, events[2].Iterations)
}

func TestService_ChatStream_SimpleResponse(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	mock := &mockToolChatCompleter{
		responses: []*ai.ToolChatResponse{
			{Content: "Streamed response.", FinishReason: "stop"},
		},
	}

	registry := NewToolRegistry()
	svc := newTestService(t, db, mock, registry, ServiceConfig{MaxIterations: 10})

	eventCh, err := svc.ChatStream(context.Background(), ChatRequest{
		UserID: "default", ConversationID: "conv1", Message: "Hello!",
	})
	require.NoError(t, err)

	var events []StreamEvent
	for event := range eventCh {
		events = append(events, event)
	}

	require.NotEmpty(t, events)
	var hasText, hasDone bool
	for _, e := range events {
		if e.Type == StreamEventText {
			hasText = true
			require.Equal(t, "Streamed response.", e.Content)
		}
		if e.Type == StreamEventDone {
			hasDone = true
		}
	}
	require.True(t, hasText, "expected text event")
	require.True(t, hasDone, "expected done event")
}

func TestService_ChatStream_Confirmation(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	mock := &mockToolChatCompleter{
		responses: []*ai.ToolChatResponse{
			{
				Content:      "",
				FinishReason: "tool_calls",
				ToolCalls:    []ai.ToolCall{{ID: "call_1", Name: "create_note", Arguments: `{}`}},
			},
		},
	}

	registry := NewToolRegistry()
	registry.Register(&Tool{
		Name: "create_note", Description: "Create note",
		Parameters: json.RawMessage(`{"type":"object","properties":{}}`),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"ok":true}`), nil
		},
		ReadOnly: false,
	})

	svc := newTestService(t, db, mock, registry, ServiceConfig{
		MaxIterations:        10,
		ConfirmationRequired: []string{"create_note"},
	})

	eventCh, err := svc.ChatStream(context.Background(), ChatRequest{
		UserID: "default", ConversationID: "conv1", Message: "create a note",
	})
	require.NoError(t, err)

	var events []StreamEvent
	for event := range eventCh {
		events = append(events, event)
	}

	// Should have confirmation + done events.
	var hasConfirmation, hasDone bool
	for _, e := range events {
		if e.Type == StreamEventConfirmation {
			hasConfirmation = true
			require.Equal(t, "create_note", e.ToolName)
			require.NotEmpty(t, e.Content) // action ID
		}
		if e.Type == StreamEventDone {
			hasDone = true
		}
	}
	require.True(t, hasConfirmation, "expected confirmation event")
	require.True(t, hasDone, "expected done event")
}

// mockChatCompleter is a tiny mock for ai.ChatCompleter used to verify
// the summarization path. It records the messages it received and
// returns a canned response.
type mockChatCompleter struct {
	calls    int
	received []ai.ChatMessage
	response string
}

func (m *mockChatCompleter) ChatCompletion(ctx context.Context, model string, messages []ai.ChatMessage) (*ai.ChatResponse, error) {
	m.calls++
	m.received = messages
	return &ai.ChatResponse{Content: m.response}, nil
}

func (m *mockChatCompleter) ChatCompletionStream(ctx context.Context, model string, messages []ai.ChatMessage) (<-chan string, <-chan error) {
	tokenCh := make(chan string)
	errCh := make(chan error, 1)
	close(tokenCh)
	close(errCh)
	return tokenCh, errCh
}

func TestService_ApplyConversationSummary_ShortHistoryNoSlice(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	svc := newTestService(t, db, &mockToolChatCompleter{}, NewToolRegistry(), ServiceConfig{MaxIterations: 10})

	history := make([]ai.ToolMessage, 5)
	for i := range history {
		history[i] = ai.ToolMessage{Role: "user", Content: fmt.Sprintf("msg %d", i)}
	}

	summary, recent := svc.applyConversationSummary(context.Background(), db, "conv1", history)
	require.Equal(t, "", summary)
	require.Equal(t, history, recent, "short history should pass through unchanged")
}

func TestService_ApplyConversationSummary_LongHistorySlicesAndLoadsSummary(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	memStore := NewMemoryStore()
	require.NoError(t, memStore.SaveConversationSummary(context.Background(), db, "conv1",
		"User has been planning a trip to Iceland; decided to go in July."))

	svc := newTestService(t, db, &mockToolChatCompleter{}, NewToolRegistry(), ServiceConfig{MaxIterations: 10})

	history := make([]ai.ToolMessage, maxAssistantRecentMessages+15)
	for i := range history {
		history[i] = ai.ToolMessage{Role: "user", Content: fmt.Sprintf("msg %d", i)}
	}

	summary, recent := svc.applyConversationSummary(context.Background(), db, "conv1", history)
	require.Contains(t, summary, "Iceland")
	require.Len(t, recent, maxAssistantRecentMessages)
	// recent should be the tail of history.
	require.Equal(t, history[len(history)-maxAssistantRecentMessages].Content, recent[0].Content)
	require.Equal(t, history[len(history)-1].Content, recent[len(recent)-1].Content)
}

func TestService_ApplyConversationSummary_LongHistoryNoExistingSummary(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	svc := newTestService(t, db, &mockToolChatCompleter{}, NewToolRegistry(), ServiceConfig{MaxIterations: 10})

	history := make([]ai.ToolMessage, maxAssistantRecentMessages+5)
	for i := range history {
		history[i] = ai.ToolMessage{Role: "assistant", Content: fmt.Sprintf("a %d", i)}
	}

	summary, recent := svc.applyConversationSummary(context.Background(), db, "conv1", history)
	require.Equal(t, "", summary, "no stored summary should yield empty string")
	require.Len(t, recent, maxAssistantRecentMessages)
}

func TestService_SummarizeMessages_BuildsExpectedPrompt(t *testing.T) {
	mc := &mockChatCompleter{response: "  User asked about goroutines and channels.  "}
	svc := &Service{
		summarizer: mc,
		chatModel:  "summary-model",
		logger:     slog.Default(),
	}

	older := []ai.ToolMessage{
		{Role: "user", Content: "Tell me about goroutines."},
		{Role: "assistant", Content: "Lightweight threads."},
		{Role: "tool", Content: "this should be skipped", ToolCallID: "x"},
		{Role: "user", Content: "And channels?"},
		{Role: "assistant", Content: "They synchronize goroutines."},
	}
	got, err := svc.summarizeMessages(context.Background(), older, "Earlier: user is learning Go.")
	require.NoError(t, err)
	require.Equal(t, "User asked about goroutines and channels.", got)

	require.Equal(t, 1, mc.calls)
	require.Len(t, mc.received, 2)
	require.Equal(t, "system", mc.received[0].Role)
	require.Contains(t, mc.received[0].Content, "compress conversations")
	require.Equal(t, "user", mc.received[1].Role)
	require.Contains(t, mc.received[1].Content, "Earlier: user is learning Go.")
	require.Contains(t, mc.received[1].Content, "goroutines")
	require.Contains(t, mc.received[1].Content, "channels")
	// Tool messages must NOT leak into the summarization transcript.
	require.NotContains(t, mc.received[1].Content, "this should be skipped")
}

func TestService_SummarizeMessages_NoSummarizerErrors(t *testing.T) {
	svc := &Service{logger: slog.Default()}
	_, err := svc.summarizeMessages(context.Background(),
		[]ai.ToolMessage{{Role: "user", Content: "x"}}, "")
	require.Error(t, err)
}

func TestService_SummarizeMessages_OnlyToolMessagesReturnsExisting(t *testing.T) {
	mc := &mockChatCompleter{response: "should not be called"}
	svc := &Service{summarizer: mc, chatModel: "m", logger: slog.Default()}

	got, err := svc.summarizeMessages(context.Background(),
		[]ai.ToolMessage{{Role: "tool", Content: "x", ToolCallID: "1"}},
		"  prior summary  ")
	require.NoError(t, err)
	require.Equal(t, "prior summary", got)
	require.Equal(t, 0, mc.calls, "LLM should not be called when transcript is empty")
}

func TestService_Chat_LongHistoryEmbedsSummaryInSystemPrompt(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Exec(`INSERT INTO conversations (id, title) VALUES ('conv1', 'Test')`)
	require.NoError(t, err)

	memStore := NewMemoryStore()
	require.NoError(t, memStore.SaveConversationSummary(context.Background(), db, "conv1",
		"Earlier: user has been debugging a flaky test in the chat package."))

	mock := &mockToolChatCompleter{
		responses: []*ai.ToolChatResponse{
			{Content: "Got it.", FinishReason: "stop"},
		},
	}

	svc := newTestService(t, db, mock, NewToolRegistry(), ServiceConfig{MaxIterations: 10})

	history := make([]ai.ToolMessage, maxAssistantRecentMessages+5)
	for i := range history {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		history[i] = ai.ToolMessage{Role: role, Content: fmt.Sprintf("turn %d", i)}
	}

	resp, err := svc.Chat(context.Background(), ChatRequest{
		UserID:         "default",
		ConversationID: "conv1",
		Message:        "Where were we?",
		History:        history,
	})
	require.NoError(t, err)
	require.Equal(t, "Got it.", resp.Response)

	// First (and only) LLM call: system + recent window + final user msg.
	require.Len(t, mock.messages, 1)
	sent := mock.messages[0]
	require.Equal(t, "system", sent[0].Role)
	require.Contains(t, sent[0].Content, "Earlier Conversation Summary")
	require.Contains(t, sent[0].Content, "flaky test")

	// Verify only the recent window was sent verbatim, not the full history.
	// Total = 1 system + maxAssistantRecentMessages history + 1 final user.
	require.Equal(t, 1+maxAssistantRecentMessages+1, len(sent))
	// First non-system message must be the start of the recent window.
	require.Equal(t,
		history[len(history)-maxAssistantRecentMessages].Content,
		sent[1].Content)
}
