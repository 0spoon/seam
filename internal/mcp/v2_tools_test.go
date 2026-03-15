package mcp_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/agent"
)

// --- memory_search tool tests ---

func TestMemorySearch_Success(t *testing.T) {
	mock := &mockAgentService{
		memorySearchFn: func(_ context.Context, userID, query string, limit int) ([]agent.KnowledgeHit, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "auth patterns", query)
			require.Equal(t, 5, limit)
			return []agent.KnowledgeHit{
				{Title: "Knowledge: patterns - auth", Snippet: "JWT auth patterns...", Source: "fts", Score: 0.9},
			}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "memory_search", map[string]any{
		"query": "auth patterns",
		"limit": float64(5),
	})

	require.False(t, result.IsError)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(resultText(t, result)), &resp))
	results, ok := resp["results"].([]interface{})
	require.True(t, ok)
	require.Len(t, results, 1)
}

func TestMemorySearch_DefaultLimit(t *testing.T) {
	mock := &mockAgentService{
		memorySearchFn: func(_ context.Context, _, _ string, limit int) ([]agent.KnowledgeHit, error) {
			require.Equal(t, 10, limit)
			return []agent.KnowledgeHit{}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "memory_search", map[string]any{
		"query": "test",
	})
	require.False(t, result.IsError)
}

func TestMemorySearch_MissingQuery(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "memory_search", map[string]any{})
	require.True(t, result.IsError)
}

// --- session_metrics tool tests ---

func TestSessionMetrics_Success(t *testing.T) {
	mock := &mockAgentService{
		sessionMetricsFn: func(_ context.Context, userID, sessionName string) (*agent.SessionMetrics, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "my-session", sessionName)
			return &agent.SessionMetrics{
				SessionName:   "my-session",
				Status:        "active",
				DurationSec:   120,
				ToolCallCount: 15,
				ToolBreakdown: map[string]int{
					"memory_write":  5,
					"notes_search":  3,
					"session_start": 1,
				},
				NotesCreated:  2,
				NotesModified: 3,
				ErrorCount:    1,
				AvgDurationMs: 45,
			}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "session_metrics", map[string]any{
		"session_name": "my-session",
	})

	require.False(t, result.IsError)
	var metrics agent.SessionMetrics
	require.NoError(t, json.Unmarshal([]byte(resultText(t, result)), &metrics))
	require.Equal(t, "my-session", metrics.SessionName)
	require.Equal(t, 15, metrics.ToolCallCount)
	require.Equal(t, 5, metrics.ToolBreakdown["memory_write"])
	require.Equal(t, 1, metrics.ErrorCount)
}

func TestSessionMetrics_NotFound(t *testing.T) {
	mock := &mockAgentService{
		sessionMetricsFn: func(context.Context, string, string) (*agent.SessionMetrics, error) {
			return nil, agent.ErrNotFound
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "session_metrics", map[string]any{
		"session_name": "nonexistent",
	})
	require.True(t, result.IsError)
}

func TestSessionMetrics_MissingSessionName(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "session_metrics", map[string]any{})
	require.True(t, result.IsError)
}

// --- context_gather scope tests ---

func TestContextGather_WithScope(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]any
		wantScope string
	}{
		{"default scope", map[string]any{"query": "test"}, "all"},
		{"agent scope", map[string]any{"query": "test", "scope": "agent"}, "agent"},
		{"user scope", map[string]any{"query": "test", "scope": "user"}, "user"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockAgentService{
				contextGatherFn: func(_ context.Context, _, _, scope string, _ int) ([]agent.KnowledgeHit, error) {
					require.Equal(t, tc.wantScope, scope)
					return []agent.KnowledgeHit{}, nil
				},
			}

			srv := newTestServer(t, mock)
			result := directCall(t, srv, "context_gather", tc.args)
			require.False(t, result.IsError)
		})
	}
}

// --- Helper ---

// resultText extracts the first TextContent from a CallToolResult.
func resultText(t *testing.T, result interface{}) string {
	t.Helper()
	// Use type assertion on the concrete MCP type.
	type textContent struct {
		Text string `json:"text"`
	}
	// Marshal and re-parse to extract text.
	data, err := json.Marshal(result)
	require.NoError(t, err)
	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(data, &parsed))
	require.NotEmpty(t, parsed.Content, "expected at least one content block")
	return parsed.Content[0].Text
}

// Ensure time import is used.
var _ = time.Now
