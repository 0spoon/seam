package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/agent"
	seamcp "github.com/katata/seam/internal/mcp"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/search"
)

// newServer creates a seamcp.Server with the given mock and a discard logger.
// Used by tests across the mcp_test package.
func newServer(mock *mockAgentService) *seamcp.Server {
	return seamcp.New(seamcp.Config{
		AgentService: mock,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// toolTestUser is the user ID injected into context for tool handler tests.
const toolTestUser = "tool-test-user"

// toolCtx returns a context with toolTestUser set.
func toolCtx() context.Context {
	return reqctx.WithUserID(context.Background(), toolTestUser)
}

// directCall looks up a tool by name, calls the handler directly (bypassing
// middleware), and returns the result. Tests use this for fast, isolated
// handler verification.
func directCall(t *testing.T, srv *seamcp.Server, toolName string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	return directCallWithCtx(t, srv, toolCtx(), toolName, args)
}

// directCallWithCtx is like directCall but accepts a custom context.
func directCallWithCtx(t *testing.T, srv *seamcp.Server, ctx context.Context, toolName string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	st := srv.MCPServer().GetTool(toolName)
	require.NotNil(t, st, "tool %q not registered", toolName)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: args,
		},
	}

	result, err := st.Handler(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

// textOf extracts the text from the first TextContent element.
func textOf(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, r.Content, "result has no content")
	tc, ok := r.Content[0].(mcp.TextContent)
	require.True(t, ok, "first content element is not TextContent")
	return tc.Text
}

// ---------------------------------------------------------------------------
// Session Start
// ---------------------------------------------------------------------------

func TestSessionStart_Success_ReturnsBriefingJSON(t *testing.T) {
	mock := &mockAgentService{
		sessionStartFn: func(_ context.Context, userID, name string, maxContextChars int) (*agent.Briefing, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "my-session", name)
			return &agent.Briefing{
				Session: &agent.Session{
					ID:     "sess-001",
					Name:   name,
					Status: agent.StatusActive,
				},
				Plan: "step 1: analyze",
			}, nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "session_start", map[string]any{
		"name": "my-session",
	})
	require.False(t, result.IsError)

	text := textOf(t, result)
	var briefing agent.Briefing
	require.NoError(t, json.Unmarshal([]byte(text), &briefing))
	require.Equal(t, "sess-001", briefing.Session.ID)
	require.Equal(t, "my-session", briefing.Session.Name)
	require.Equal(t, agent.StatusActive, briefing.Session.Status)
	require.Equal(t, "step 1: analyze", briefing.Plan)
}

func TestSessionStart_MissingName_ReturnsError(t *testing.T) {
	srv := newServer(&mockAgentService{})

	result := directCall(t, srv, "session_start", map[string]any{})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "name")
}

func TestSessionStart_ServiceError_ReturnsError(t *testing.T) {
	mock := &mockAgentService{
		sessionStartFn: func(context.Context, string, string, int) (*agent.Briefing, error) {
			return nil, errors.New("database connection lost")
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "session_start", map[string]any{
		"name": "fail-session",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "session_start: internal error")
}

func TestSessionStart_CustomMaxContextChars_PassedToService(t *testing.T) {
	var captured int
	mock := &mockAgentService{
		sessionStartFn: func(_ context.Context, _, _ string, maxContextChars int) (*agent.Briefing, error) {
			captured = maxContextChars
			return &agent.Briefing{Session: &agent.Session{ID: "s1", Name: "x", Status: agent.StatusActive}}, nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "session_start", map[string]any{
		"name":              "ctx-session",
		"max_context_chars": float64(2000),
	})
	require.False(t, result.IsError)
	require.Equal(t, 2000, captured)
}

func TestSessionStart_DefaultMaxContextChars_Uses4000(t *testing.T) {
	var captured int
	mock := &mockAgentService{
		sessionStartFn: func(_ context.Context, _, _ string, maxContextChars int) (*agent.Briefing, error) {
			captured = maxContextChars
			return &agent.Briefing{Session: &agent.Session{ID: "s2", Name: "y", Status: agent.StatusActive}}, nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "session_start", map[string]any{
		"name": "default-ctx",
	})
	require.False(t, result.IsError)
	require.Equal(t, agent.DefaultMaxContextChars, captured)
}

// ---------------------------------------------------------------------------
// Session Plan Set
// ---------------------------------------------------------------------------

func TestSessionPlanSet_Success_ReturnsNoteID(t *testing.T) {
	mock := &mockAgentService{
		sessionPlanSetFn: func(_ context.Context, userID, sessionName, content string) (string, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "plan-session", sessionName)
			require.Equal(t, "## Plan\n- Step 1\n- Step 2", content)
			return "note-plan-001", nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "session_plan_set", map[string]any{
		"session_name": "plan-session",
		"content":      "## Plan\n- Step 1\n- Step 2",
	})
	require.False(t, result.IsError)

	text := textOf(t, result)
	var parsed map[string]string
	require.NoError(t, json.Unmarshal([]byte(text), &parsed))
	require.Equal(t, "note-plan-001", parsed["note_id"])
}

func TestSessionPlanSet_MissingSessionName_ReturnsError(t *testing.T) {
	srv := newServer(&mockAgentService{})

	result := directCall(t, srv, "session_plan_set", map[string]any{
		"content": "some plan",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "session_name")
}

func TestSessionPlanSet_MissingContent_ReturnsError(t *testing.T) {
	srv := newServer(&mockAgentService{})

	result := directCall(t, srv, "session_plan_set", map[string]any{
		"session_name": "my-session",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "content")
}

// ---------------------------------------------------------------------------
// Session Progress Update
// ---------------------------------------------------------------------------

func TestSessionProgressUpdate_Success_ReturnsNoteID(t *testing.T) {
	var capturedTask, capturedStatus, capturedNotes string
	mock := &mockAgentService{
		sessionProgressUpdateFn: func(_ context.Context, userID, sessionName, task, status, notes string) (string, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "progress-sess", sessionName)
			capturedTask = task
			capturedStatus = status
			capturedNotes = notes
			return "note-progress-001", nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "session_progress_update", map[string]any{
		"session_name": "progress-sess",
		"task":         "implement auth",
		"status":       "in_progress",
		"notes":        "halfway done",
	})
	require.False(t, result.IsError)

	text := textOf(t, result)
	require.Contains(t, text, "note-progress-001")
	require.Equal(t, "implement auth", capturedTask)
	require.Equal(t, "in_progress", capturedStatus)
	require.Equal(t, "halfway done", capturedNotes)
}

func TestSessionProgressUpdate_MissingTask_ReturnsError(t *testing.T) {
	srv := newServer(&mockAgentService{})

	result := directCall(t, srv, "session_progress_update", map[string]any{
		"session_name": "my-session",
		"status":       "completed",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "task")
}

func TestSessionProgressUpdate_OptionalNotes_PassesEmptyString(t *testing.T) {
	var capturedNotes string
	mock := &mockAgentService{
		sessionProgressUpdateFn: func(_ context.Context, _, _, _, _, notes string) (string, error) {
			capturedNotes = notes
			return "note-progress-002", nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "session_progress_update", map[string]any{
		"session_name": "my-session",
		"task":         "cleanup",
		"status":       "completed",
	})
	require.False(t, result.IsError)
	require.Equal(t, "", capturedNotes, "omitted notes should default to empty string")
}

// ---------------------------------------------------------------------------
// Session Context Set
// ---------------------------------------------------------------------------

func TestSessionContextSet_Success_ReturnsNoteID(t *testing.T) {
	mock := &mockAgentService{
		sessionContextSetFn: func(_ context.Context, userID, sessionName, content string) (string, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "ctx-sess", sessionName)
			require.Equal(t, "relevant context here", content)
			return "note-ctx-001", nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "session_context_set", map[string]any{
		"session_name": "ctx-sess",
		"content":      "relevant context here",
	})
	require.False(t, result.IsError)

	text := textOf(t, result)
	var parsed map[string]string
	require.NoError(t, json.Unmarshal([]byte(text), &parsed))
	require.Equal(t, "note-ctx-001", parsed["note_id"])
}

func TestSessionContextSet_MissingContent_ReturnsError(t *testing.T) {
	srv := newServer(&mockAgentService{})

	result := directCall(t, srv, "session_context_set", map[string]any{
		"session_name": "my-session",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "content")
}

// ---------------------------------------------------------------------------
// Session End
// ---------------------------------------------------------------------------

func TestSessionEnd_Success_ReturnsCompleted(t *testing.T) {
	var capturedFindings string
	mock := &mockAgentService{
		sessionEndFn: func(_ context.Context, userID, sessionName, findings string) error {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "end-sess", sessionName)
			capturedFindings = findings
			return nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "session_end", map[string]any{
		"session_name": "end-sess",
		"findings":     "discovered 3 bugs in auth module",
	})
	require.False(t, result.IsError)

	text := textOf(t, result)
	var parsed map[string]string
	require.NoError(t, json.Unmarshal([]byte(text), &parsed))
	require.Equal(t, "completed", parsed["status"])
	require.Equal(t, "discovered 3 bugs in auth module", capturedFindings)
}

func TestSessionEnd_MissingFindings_ReturnsError(t *testing.T) {
	srv := newServer(&mockAgentService{})

	result := directCall(t, srv, "session_end", map[string]any{
		"session_name": "my-session",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "findings")
}

func TestSessionEnd_ServiceError_ReturnsError(t *testing.T) {
	mock := &mockAgentService{
		sessionEndFn: func(context.Context, string, string, string) error {
			return agent.ErrFindingsTooLong
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "session_end", map[string]any{
		"session_name": "my-session",
		"findings":     "too long",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "session_end: findings exceed maximum length")
}

// ---------------------------------------------------------------------------
// Session List
// ---------------------------------------------------------------------------

func TestSessionList_DefaultStatus_UsesActive(t *testing.T) {
	var capturedStatus string
	var capturedLimit int
	mock := &mockAgentService{
		sessionListFn: func(_ context.Context, _, status string, limit int) ([]*agent.Session, error) {
			capturedStatus = status
			capturedLimit = limit
			return []*agent.Session{}, nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "session_list", map[string]any{})
	require.False(t, result.IsError)
	require.Equal(t, "active", capturedStatus)
	require.Equal(t, 20, capturedLimit)
}

func TestSessionList_AllStatus_PassesEmptyString(t *testing.T) {
	var capturedStatus string
	mock := &mockAgentService{
		sessionListFn: func(_ context.Context, _, status string, limit int) ([]*agent.Session, error) {
			capturedStatus = status
			return []*agent.Session{}, nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "session_list", map[string]any{
		"status": "all",
	})
	require.False(t, result.IsError)
	require.Equal(t, "", capturedStatus, "status 'all' should map to empty string")
}

func TestSessionList_CustomLimit_PassedToService(t *testing.T) {
	var capturedLimit int
	mock := &mockAgentService{
		sessionListFn: func(_ context.Context, _, _ string, limit int) ([]*agent.Session, error) {
			capturedLimit = limit
			return []*agent.Session{
				{ID: "s1", Name: "a", Status: agent.StatusActive},
			}, nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "session_list", map[string]any{
		"limit": float64(5),
	})
	require.False(t, result.IsError)
	require.Equal(t, 5, capturedLimit)

	text := textOf(t, result)
	var sessions []*agent.Session
	require.NoError(t, json.Unmarshal([]byte(text), &sessions))
	require.Len(t, sessions, 1)
	require.Equal(t, "a", sessions[0].Name)
}

func TestSessionList_DefaultLimit_Uses20(t *testing.T) {
	var capturedLimit int
	mock := &mockAgentService{
		sessionListFn: func(_ context.Context, _, _ string, limit int) ([]*agent.Session, error) {
			capturedLimit = limit
			return []*agent.Session{}, nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "session_list", map[string]any{})
	require.False(t, result.IsError)
	require.Equal(t, 20, capturedLimit)
}

// ---------------------------------------------------------------------------
// Memory Read
// ---------------------------------------------------------------------------

func TestMemoryRead_Success_ReturnsTitleAndBody(t *testing.T) {
	mock := &mockAgentService{
		memoryReadFn: func(_ context.Context, userID, category, name string) (string, string, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "architecture", category)
			require.Equal(t, "overview", name)
			return "Architecture Overview", "# Arch\nService-oriented design.", nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "memory_read", map[string]any{
		"category": "architecture",
		"name":     "overview",
	})
	require.False(t, result.IsError)

	text := textOf(t, result)
	var parsed map[string]string
	require.NoError(t, json.Unmarshal([]byte(text), &parsed))
	require.Equal(t, "Architecture Overview", parsed["title"])
	require.Equal(t, "# Arch\nService-oriented design.", parsed["body"])
}

func TestMemoryRead_MissingCategory_ReturnsError(t *testing.T) {
	srv := newServer(&mockAgentService{})

	result := directCall(t, srv, "memory_read", map[string]any{
		"name": "overview",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "category")
}

func TestMemoryRead_NotFound_ReturnsError(t *testing.T) {
	mock := &mockAgentService{
		memoryReadFn: func(context.Context, string, string, string) (string, string, error) {
			return "", "", agent.ErrNotFound
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "memory_read", map[string]any{
		"category": "missing",
		"name":     "gone",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "memory_read: not found")
}

// ---------------------------------------------------------------------------
// Memory Write
// ---------------------------------------------------------------------------

func TestMemoryWrite_Success_ReturnsNoteID(t *testing.T) {
	var capturedCategory, capturedName, capturedContent string
	mock := &mockAgentService{
		memoryWriteFn: func(_ context.Context, userID, category, name, content string) (string, error) {
			require.Equal(t, toolTestUser, userID)
			capturedCategory = category
			capturedName = name
			capturedContent = content
			return "note-mem-001", nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "memory_write", map[string]any{
		"category": "conventions",
		"name":     "go-style",
		"content":  "Use gofmt. No exceptions.",
	})
	require.False(t, result.IsError)

	text := textOf(t, result)
	var parsed map[string]string
	require.NoError(t, json.Unmarshal([]byte(text), &parsed))
	require.Equal(t, "note-mem-001", parsed["note_id"])
	require.Equal(t, "conventions", capturedCategory)
	require.Equal(t, "go-style", capturedName)
	require.Equal(t, "Use gofmt. No exceptions.", capturedContent)
}

func TestMemoryWrite_MissingContent_ReturnsError(t *testing.T) {
	srv := newServer(&mockAgentService{})

	result := directCall(t, srv, "memory_write", map[string]any{
		"category": "conventions",
		"name":     "go-style",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "content")
}

// ---------------------------------------------------------------------------
// Memory Append
// ---------------------------------------------------------------------------

func TestMemoryAppend_Success_ReturnsAppendedStatus(t *testing.T) {
	var capturedContent string
	mock := &mockAgentService{
		memoryAppendFn: func(_ context.Context, userID, category, name, content string) error {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "conventions", category)
			require.Equal(t, "go-style", name)
			capturedContent = content
			return nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "memory_append", map[string]any{
		"category": "conventions",
		"name":     "go-style",
		"content":  "\n- Always use testify/require.",
	})
	require.False(t, result.IsError)

	text := textOf(t, result)
	var parsed map[string]string
	require.NoError(t, json.Unmarshal([]byte(text), &parsed))
	require.Equal(t, "appended", parsed["status"])
	require.Equal(t, "\n- Always use testify/require.", capturedContent)
}

func TestMemoryAppend_MissingName_ReturnsError(t *testing.T) {
	srv := newServer(&mockAgentService{})

	result := directCall(t, srv, "memory_append", map[string]any{
		"category": "conventions",
		"content":  "extra data",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "name")
}

func TestMemoryAppend_ServiceError_ReturnsError(t *testing.T) {
	mock := &mockAgentService{
		memoryAppendFn: func(context.Context, string, string, string, string) error {
			return errors.New("disk full")
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "memory_append", map[string]any{
		"category": "conventions",
		"name":     "go-style",
		"content":  "more data",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "memory_append: internal error")
}

// ---------------------------------------------------------------------------
// Memory List
// ---------------------------------------------------------------------------

func TestMemoryList_AllCategories_PassesEmptyString(t *testing.T) {
	var capturedCategory string
	now := time.Now()
	mock := &mockAgentService{
		memoryListFn: func(_ context.Context, userID, category string) ([]agent.MemoryItem, error) {
			require.Equal(t, toolTestUser, userID)
			capturedCategory = category
			return []agent.MemoryItem{
				{Category: "arch", Name: "overview", Title: "Arch Overview", UpdatedAt: now},
				{Category: "decisions", Name: "ulid", Title: "Use ULID", UpdatedAt: now},
			}, nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "memory_list", map[string]any{})
	require.False(t, result.IsError)
	require.Equal(t, "", capturedCategory)

	text := textOf(t, result)
	var items []agent.MemoryItem
	require.NoError(t, json.Unmarshal([]byte(text), &items))
	require.Len(t, items, 2)
	require.Equal(t, "arch", items[0].Category)
	require.Equal(t, "decisions", items[1].Category)
}

func TestMemoryList_FilteredCategory_PassedToService(t *testing.T) {
	var capturedCategory string
	mock := &mockAgentService{
		memoryListFn: func(_ context.Context, _, category string) ([]agent.MemoryItem, error) {
			capturedCategory = category
			return []agent.MemoryItem{}, nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "memory_list", map[string]any{
		"category": "conventions",
	})
	require.False(t, result.IsError)
	require.Equal(t, "conventions", capturedCategory)
}

// ---------------------------------------------------------------------------
// Memory Delete
// ---------------------------------------------------------------------------

func TestMemoryDelete_Success_ReturnsDeletedStatus(t *testing.T) {
	var capturedCategory, capturedName string
	mock := &mockAgentService{
		memoryDeleteFn: func(_ context.Context, userID, category, name string) error {
			require.Equal(t, toolTestUser, userID)
			capturedCategory = category
			capturedName = name
			return nil
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "memory_delete", map[string]any{
		"category": "stale",
		"name":     "old-note",
	})
	require.False(t, result.IsError)

	text := textOf(t, result)
	var parsed map[string]string
	require.NoError(t, json.Unmarshal([]byte(text), &parsed))
	require.Equal(t, "deleted", parsed["status"])
	require.Equal(t, "stale", capturedCategory)
	require.Equal(t, "old-note", capturedName)
}

func TestMemoryDelete_NotFound_ReturnsError(t *testing.T) {
	mock := &mockAgentService{
		memoryDeleteFn: func(context.Context, string, string, string) error {
			return agent.ErrNotFound
		},
	}
	srv := newServer(mock)

	result := directCall(t, srv, "memory_delete", map[string]any{
		"category": "missing",
		"name":     "gone",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "memory_delete: not found")
}

// ---------------------------------------------------------------------------
// Context Gather
// ---------------------------------------------------------------------------

func TestContextGather_Success_ReturnsStubResult(t *testing.T) {
	srv := newServer(&mockAgentService{})

	result := directCall(t, srv, "context_gather", map[string]any{
		"query": "authentication flow",
	})
	require.False(t, result.IsError)

	text := textOf(t, result)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &parsed))
	require.Contains(t, parsed, "results")
}

func TestContextGather_MissingQuery_ReturnsError(t *testing.T) {
	srv := newServer(&mockAgentService{})

	result := directCall(t, srv, "context_gather", map[string]any{})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "query")
}

// ---------------------------------------------------------------------------
// Notes Search
// ---------------------------------------------------------------------------

func TestNotesSearch_Success_ReturnsResults(t *testing.T) {
	mock := &mockAgentService{
		notesSearchFn: func(_ context.Context, userID, query string, limit int) ([]search.FTSResult, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "middleware", query)
			require.Equal(t, 5, limit)
			return []search.FTSResult{
				{NoteID: "n1", Title: "Auth Middleware", Snippet: "handles auth", Rank: 1.5},
			}, nil
		},
	}
	srv := newServer(mock)
	result := directCall(t, srv, "notes_search", map[string]any{
		"query": "middleware",
		"limit": float64(5),
	})

	require.False(t, result.IsError)
	text := textOf(t, result)
	require.Contains(t, text, "Auth Middleware")
	require.Contains(t, text, "n1")
}

func TestNotesSearch_MissingQuery_ReturnsError(t *testing.T) {
	srv := newServer(&mockAgentService{})
	result := directCall(t, srv, "notes_search", map[string]any{})

	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "query")
}

// ---------------------------------------------------------------------------
// Notes Read
// ---------------------------------------------------------------------------

func TestNotesRead_Success_ReturnsNote(t *testing.T) {
	mock := &mockAgentService{
		notesReadFn: func(_ context.Context, userID, noteID string) (*note.Note, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "note-abc", noteID)
			return &note.Note{
				ID:    "note-abc",
				Title: "Test Note",
				Body:  "Hello world",
				Tags:  []string{"tag1", "tag2"},
			}, nil
		},
	}
	srv := newServer(mock)
	result := directCall(t, srv, "notes_read", map[string]any{
		"id": "note-abc",
	})

	require.False(t, result.IsError)
	text := textOf(t, result)
	require.Contains(t, text, "Test Note")
	require.Contains(t, text, "Hello world")
	require.Contains(t, text, "tag1")
}

func TestNotesRead_MissingID_ReturnsError(t *testing.T) {
	srv := newServer(&mockAgentService{})
	result := directCall(t, srv, "notes_read", map[string]any{})

	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "id")
}

func TestNotesRead_NotFound_ReturnsError(t *testing.T) {
	mock := &mockAgentService{
		notesReadFn: func(context.Context, string, string) (*note.Note, error) {
			return nil, fmt.Errorf("agent.Service.NotesRead: %w", note.ErrNotFound)
		},
	}
	srv := newServer(mock)
	result := directCall(t, srv, "notes_read", map[string]any{
		"id": "nonexistent",
	})

	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "not found")
}

// ---------------------------------------------------------------------------
// Notes List
// ---------------------------------------------------------------------------

func TestNotesList_Success_ReturnsSummaries(t *testing.T) {
	mock := &mockAgentService{
		notesListFn: func(_ context.Context, userID, projectSlug, tag string, limit int) ([]*note.Note, int, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "my-project", projectSlug)
			require.Equal(t, "important", tag)
			require.Equal(t, 10, limit)
			return []*note.Note{
				{ID: "n1", Title: "Note One", Tags: []string{"important"}},
				{ID: "n2", Title: "Note Two", Tags: []string{"important"}},
			}, 2, nil
		},
	}
	srv := newServer(mock)
	result := directCall(t, srv, "notes_list", map[string]any{
		"project": "my-project",
		"tag":     "important",
		"limit":   float64(10),
	})

	require.False(t, result.IsError)
	text := textOf(t, result)
	require.Contains(t, text, "Note One")
	require.Contains(t, text, "Note Two")
	require.Contains(t, text, `"total":2`)
}

func TestNotesList_DefaultParams_UsesDefaults(t *testing.T) {
	mock := &mockAgentService{
		notesListFn: func(_ context.Context, userID, projectSlug, tag string, limit int) ([]*note.Note, int, error) {
			require.Equal(t, "", projectSlug)
			require.Equal(t, "", tag)
			require.Equal(t, 20, limit)
			return []*note.Note{}, 0, nil
		},
	}
	srv := newServer(mock)
	result := directCall(t, srv, "notes_list", map[string]any{})

	require.False(t, result.IsError)
}

// ---------------------------------------------------------------------------
// Notes Create
// ---------------------------------------------------------------------------

func TestNotesCreate_Success_ReturnsNoteID(t *testing.T) {
	mock := &mockAgentService{
		notesCreateFn: func(_ context.Context, userID, title, body, projectSlug string, tags []string) (*note.Note, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "New Note", title)
			require.Equal(t, "# Content", body)
			require.Equal(t, "my-project", projectSlug)
			require.Equal(t, []string{"tag1", "tag2"}, tags)
			return &note.Note{ID: "new-note-id"}, nil
		},
	}
	srv := newServer(mock)
	result := directCall(t, srv, "notes_create", map[string]any{
		"title":   "New Note",
		"body":    "# Content",
		"project": "my-project",
		"tags":    "tag1, tag2",
	})

	require.False(t, result.IsError)
	require.Contains(t, textOf(t, result), "new-note-id")
}

func TestNotesCreate_MissingTitle_ReturnsError(t *testing.T) {
	srv := newServer(&mockAgentService{})
	result := directCall(t, srv, "notes_create", map[string]any{
		"body": "some body",
	})

	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "title")
}

func TestNotesCreate_MissingBody_ReturnsError(t *testing.T) {
	srv := newServer(&mockAgentService{})
	result := directCall(t, srv, "notes_create", map[string]any{
		"title": "some title",
	})

	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "body")
}

func TestNotesCreate_NoTags_PassesEmptySlice(t *testing.T) {
	mock := &mockAgentService{
		notesCreateFn: func(_ context.Context, _, _, _, _ string, tags []string) (*note.Note, error) {
			require.Empty(t, tags)
			return &note.Note{ID: "note-no-tags"}, nil
		},
	}
	srv := newServer(mock)
	result := directCall(t, srv, "notes_create", map[string]any{
		"title": "No Tags",
		"body":  "content",
	})

	require.False(t, result.IsError)
}

// ---------------------------------------------------------------------------
// Auth Integration (middleware via HandleMessage)
// ---------------------------------------------------------------------------

func TestToolHandler_NoUserID_ReturnsUnauthorized(t *testing.T) {
	// The auth middleware runs inside HandleMessage, not on direct handler
	// calls. We send a JSON-RPC tools/call message through HandleMessage
	// with a context that has no user ID to verify rejection.
	mock := &mockAgentService{
		sessionListFn: func(context.Context, string, string, int) ([]*agent.Session, error) {
			t.Fatal("service should not be called without authentication")
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
		ID: 99,
	}

	// Empty userID triggers no-user-ID path in handleMessageWithUserID.
	result := handleMessageWithUserID(t, srv, "", req)
	require.NotNil(t, result)

	respBytes, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(respBytes), "unauthorized",
		"calling tool without user ID should produce unauthorized error")
}
