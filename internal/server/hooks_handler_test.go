package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/agent"
	"github.com/katata/seam/internal/task"
)

const testAPIKey = "secret-key-1234567890"

type mockBriefingService struct {
	hookBriefingFn func(ctx context.Context, userID string, payload agent.HookPayload, maxChars, openTaskCount int) (string, error)
}

func (m *mockBriefingService) HookBriefing(ctx context.Context, userID string, payload agent.HookPayload, maxChars, openTaskCount int) (string, error) {
	if m.hookBriefingFn == nil {
		return "<seam-briefing>stub</seam-briefing>", nil
	}
	return m.hookBriefingFn(ctx, userID, payload, maxChars, openTaskCount)
}

type mockTaskCounter struct {
	summaryFn func(ctx context.Context, userID string, filter task.TaskFilter) (*task.TaskSummary, error)
}

func (m *mockTaskCounter) Summary(ctx context.Context, userID string, filter task.TaskFilter) (*task.TaskSummary, error) {
	if m.summaryFn == nil {
		return &task.TaskSummary{Open: 7, Done: 0, Total: 7}, nil
	}
	return m.summaryFn(ctx, userID, filter)
}

func newTestHooksHandler(t *testing.T, agentSvc HookBriefingService, taskSvc HookTaskCounter) *httptest.Server {
	t.Helper()
	h := NewHooksHandler(agentSvc, taskSvc, testAPIKey, nil, 500)
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	return srv
}

func postSessionStart(t *testing.T, srv *httptest.Server, apiKey string, payload agent.HookPayload) (*http.Response, []byte) {
	t.Helper()

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/session-start", strings.NewReader(string(body)))
	require.NoError(t, err)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, respBody
}

func TestHooksHandler_MissingBearer(t *testing.T) {
	t.Parallel()

	srv := newTestHooksHandler(t, &mockBriefingService{}, nil)
	resp, _ := postSessionStart(t, srv, "", agent.HookPayload{Source: "startup"})
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestHooksHandler_WrongBearer(t *testing.T) {
	t.Parallel()

	srv := newTestHooksHandler(t, &mockBriefingService{}, nil)
	resp, _ := postSessionStart(t, srv, "wrong-key", agent.HookPayload{Source: "startup"})
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestHooksHandler_ValidBearerEmptyStore(t *testing.T) {
	t.Parallel()

	mock := &mockBriefingService{
		hookBriefingFn: func(ctx context.Context, userID string, payload agent.HookPayload, maxChars, openTaskCount int) (string, error) {
			return "<seam-briefing>\nSeam: no open sessions, no recent memories.\n</seam-briefing>", nil
		},
	}
	srv := newTestHooksHandler(t, mock, nil)
	resp, body := postSessionStart(t, srv, testAPIKey, agent.HookPayload{Source: "startup"})

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got hookSessionStartResponse
	require.NoError(t, json.Unmarshal(body, &got))
	require.True(t, got.Continue)
	require.True(t, got.SuppressOutput)
	require.Equal(t, "SessionStart", got.HookSpecificOutput.HookEventName)
	require.Contains(t, got.HookSpecificOutput.AdditionalContext, "<seam-briefing>")
	require.Contains(t, got.HookSpecificOutput.AdditionalContext, "no open sessions")
}

// TestHooksHandler_WireFormat is the canary that locks the JSON shape Claude
// Code expects. If a future refactor renames a field, we will know here
// instead of when the user discovers their hook silently doing nothing.
func TestHooksHandler_WireFormat(t *testing.T) {
	t.Parallel()

	srv := newTestHooksHandler(t, &mockBriefingService{}, nil)
	resp, body := postSessionStart(t, srv, testAPIKey, agent.HookPayload{Source: "startup"})
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Decode into a generic map so we can assert the exact field names.
	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))

	require.Contains(t, raw, "continue")
	require.Contains(t, raw, "suppressOutput")
	require.Contains(t, raw, "hookSpecificOutput")

	hso, ok := raw["hookSpecificOutput"].(map[string]any)
	require.True(t, ok, "hookSpecificOutput must be an object")
	require.Contains(t, hso, "hookEventName")
	require.Contains(t, hso, "additionalContext")
	require.Equal(t, "SessionStart", hso["hookEventName"])
}

func TestHooksHandler_BriefingErrorReturnsEmptyContext(t *testing.T) {
	t.Parallel()

	mock := &mockBriefingService{
		hookBriefingFn: func(ctx context.Context, userID string, payload agent.HookPayload, maxChars, openTaskCount int) (string, error) {
			return "", errors.New("boom")
		},
	}
	srv := newTestHooksHandler(t, mock, nil)
	resp, body := postSessionStart(t, srv, testAPIKey, agent.HookPayload{Source: "startup"})

	require.Equal(t, http.StatusOK, resp.StatusCode, "must never block the session on internal error")

	var got hookSessionStartResponse
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, "", got.HookSpecificOutput.AdditionalContext)
	require.True(t, got.Continue)
}

func TestHooksHandler_TimeoutReturnsBeforeBlocking(t *testing.T) {
	t.Parallel()

	mock := &mockBriefingService{
		hookBriefingFn: func(ctx context.Context, userID string, payload agent.HookPayload, maxChars, openTaskCount int) (string, error) {
			// Simulate a hung downstream by waiting on the context.
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
	srv := newTestHooksHandler(t, mock, nil)

	start := time.Now()
	resp, body := postSessionStart(t, srv, testAPIKey, agent.HookPayload{Source: "startup"})
	elapsed := time.Since(start)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Less(t, elapsed, hookHandlerTimeout+500*time.Millisecond, "handler must respect its own timeout")

	var got hookSessionStartResponse
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, "", got.HookSpecificOutput.AdditionalContext, "timed-out briefing should be empty")
}

func TestHooksHandler_CompactSourcePassedThrough(t *testing.T) {
	t.Parallel()

	var seenSource string
	mock := &mockBriefingService{
		hookBriefingFn: func(ctx context.Context, userID string, payload agent.HookPayload, maxChars, openTaskCount int) (string, error) {
			seenSource = payload.Source
			return "<seam-briefing>compact mode</seam-briefing>", nil
		},
	}
	srv := newTestHooksHandler(t, mock, nil)
	resp, body := postSessionStart(t, srv, testAPIKey, agent.HookPayload{Source: agent.HookSourceCompact})

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, agent.HookSourceCompact, seenSource)

	var got hookSessionStartResponse
	require.NoError(t, json.Unmarshal(body, &got))
	require.Contains(t, got.HookSpecificOutput.AdditionalContext, "compact mode")
}

func TestHooksHandler_TaskCounterUsedWhenAvailable(t *testing.T) {
	t.Parallel()

	var seenCount int = -100
	briefingMock := &mockBriefingService{
		hookBriefingFn: func(ctx context.Context, userID string, payload agent.HookPayload, maxChars, openTaskCount int) (string, error) {
			seenCount = openTaskCount
			return "<seam-briefing>ok</seam-briefing>", nil
		},
	}
	taskMock := &mockTaskCounter{
		summaryFn: func(ctx context.Context, userID string, filter task.TaskFilter) (*task.TaskSummary, error) {
			require.NotNil(t, filter.Done, "must filter to open tasks")
			require.False(t, *filter.Done)
			return &task.TaskSummary{Open: 11, Done: 4, Total: 15}, nil
		},
	}

	srv := newTestHooksHandler(t, briefingMock, taskMock)
	resp, _ := postSessionStart(t, srv, testAPIKey, agent.HookPayload{Source: "startup"})

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, 11, seenCount)
}

func TestHooksHandler_TaskCounterErrorPassesNegativeCount(t *testing.T) {
	t.Parallel()

	var seenCount int = -100
	briefingMock := &mockBriefingService{
		hookBriefingFn: func(ctx context.Context, userID string, payload agent.HookPayload, maxChars, openTaskCount int) (string, error) {
			seenCount = openTaskCount
			return "<seam-briefing>ok</seam-briefing>", nil
		},
	}
	taskMock := &mockTaskCounter{
		summaryFn: func(ctx context.Context, userID string, filter task.TaskFilter) (*task.TaskSummary, error) {
			return nil, errors.New("db is down")
		},
	}
	srv := newTestHooksHandler(t, briefingMock, taskMock)
	resp, _ := postSessionStart(t, srv, testAPIKey, agent.HookPayload{Source: "startup"})

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, -1, seenCount, "task counter failures must yield -1, not 0")
}

func TestHooksHandler_NoTaskCounterMeansNegativeCount(t *testing.T) {
	t.Parallel()

	var seenCount int = -100
	briefingMock := &mockBriefingService{
		hookBriefingFn: func(ctx context.Context, userID string, payload agent.HookPayload, maxChars, openTaskCount int) (string, error) {
			seenCount = openTaskCount
			return "<seam-briefing>ok</seam-briefing>", nil
		},
	}
	srv := newTestHooksHandler(t, briefingMock, nil)
	resp, _ := postSessionStart(t, srv, testAPIKey, agent.HookPayload{Source: "startup"})

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, -1, seenCount)
}

func TestHooksHandler_EmptyAPIKeyRejectsAll(t *testing.T) {
	t.Parallel()

	h := NewHooksHandler(&mockBriefingService{}, nil, "" /* apiKey */, nil, 500)
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)

	resp, _ := postSessionStart(t, srv, testAPIKey, agent.HookPayload{Source: "startup"})
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
