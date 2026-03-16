package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// --- MemorySearch Tests ---

func TestService_MemorySearch_ReturnsResults(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Write some knowledge.
	_, err := svc.MemoryWrite(ctx, testUserID, "patterns", "auth", "JWT authentication patterns and best practices")
	require.NoError(t, err)

	_, err = svc.MemoryWrite(ctx, testUserID, "patterns", "caching", "Redis caching strategies and TTL patterns")
	require.NoError(t, err)

	// Search for auth-related knowledge via FTS.
	hits, err := svc.MemorySearch(ctx, testUserID, "authentication", 10)
	require.NoError(t, err)
	// FTS should find the auth note.
	require.NotEmpty(t, hits)
}

func TestService_MemorySearch_EmptyResults(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	hits, err := svc.MemorySearch(ctx, testUserID, "nonexistent-topic-xyz", 10)
	require.NoError(t, err)
	require.Empty(t, hits)
}

func TestService_MemorySearch_DefaultLimit(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	hits, err := svc.MemorySearch(ctx, testUserID, "anything", 0)
	require.NoError(t, err)
	require.NotNil(t, hits) // should not error, just empty
}

// --- SessionMetrics Tests ---

func TestService_SessionMetrics_ActiveSession(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Start a session.
	_, err := svc.SessionStart(ctx, testUserID, "metrics-test", DefaultMaxContextChars)
	require.NoError(t, err)

	// Set a plan (creates a note).
	_, err = svc.SessionPlanSet(ctx, testUserID, "metrics-test", "Test plan content")
	require.NoError(t, err)

	// Update progress (creates another note).
	_, err = svc.SessionProgressUpdate(ctx, testUserID, "metrics-test", "task-1", "completed", "done")
	require.NoError(t, err)

	// Get metrics.
	metrics, err := svc.SessionMetrics(ctx, testUserID, "metrics-test")
	require.NoError(t, err)
	require.Equal(t, "metrics-test", metrics.SessionName)
	require.Equal(t, StatusActive, metrics.Status)
	require.True(t, metrics.DurationSec >= 0)
	require.NotNil(t, metrics.ToolBreakdown)
}

func TestService_SessionMetrics_CompletedSession(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Start and end a session.
	_, err := svc.SessionStart(ctx, testUserID, "done-session", DefaultMaxContextChars)
	require.NoError(t, err)

	err = svc.SessionEnd(ctx, testUserID, "done-session", "Session complete.")
	require.NoError(t, err)

	metrics, err := svc.SessionMetrics(ctx, testUserID, "done-session")
	require.NoError(t, err)
	require.Equal(t, StatusCompleted, metrics.Status)
	require.True(t, metrics.DurationSec >= 0)
}

func TestService_SessionMetrics_NotFound(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.SessionMetrics(ctx, testUserID, "nonexistent-session")
	require.Error(t, err)
}

// --- ContextGather Scope Tests ---

func TestService_ContextGather_ScopeAll(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Write agent knowledge.
	_, err := svc.MemoryWrite(ctx, testUserID, "test", "data", "agent knowledge content about testing")
	require.NoError(t, err)

	// Gather with "all" scope should find agent knowledge.
	hits, err := svc.ContextGather(ctx, testUserID, "testing", "all", 3000, 0.0)
	require.NoError(t, err)
	// FTS should find it.
	require.NotEmpty(t, hits)
}

func TestService_ContextGather_ScopeAgent(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Write agent knowledge.
	_, err := svc.MemoryWrite(ctx, testUserID, "patterns", "scope-test", "agent-only searchable content xyz")
	require.NoError(t, err)

	// Gather with "agent" scope.
	hits, err := svc.ContextGather(ctx, testUserID, "searchable", "agent", 3000, 0.0)
	require.NoError(t, err)
	require.NotEmpty(t, hits)
}

func TestService_ContextGather_EmptyQuery(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	hits, err := svc.ContextGather(ctx, testUserID, "nonexistent-xyz-abc", "all", 3000, 0.0)
	require.NoError(t, err)
	require.Empty(t, hits)
}

// --- WSNotifier Tests ---

type mockWSNotifier struct {
	events []wsEvent
}

type wsEvent struct {
	userID    string
	eventType string
	payload   interface{}
}

func (m *mockWSNotifier) SendAgentEvent(userID, eventType string, payload interface{}) {
	m.events = append(m.events, wsEvent{userID: userID, eventType: eventType, payload: payload})
}

func TestService_SessionStart_EmitsWSEvent(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	notifier := &mockWSNotifier{}
	svc.cfg.WSNotifier = notifier

	_, err := svc.SessionStart(ctx, testUserID, "ws-test", DefaultMaxContextChars)
	require.NoError(t, err)

	require.Len(t, notifier.events, 1)
	require.Equal(t, "agent.session_started", notifier.events[0].eventType)
	require.Equal(t, testUserID, notifier.events[0].userID)
}

func TestService_SessionEnd_EmitsWSEvent(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	notifier := &mockWSNotifier{}
	svc.cfg.WSNotifier = notifier

	_, err := svc.SessionStart(ctx, testUserID, "ws-end-test", DefaultMaxContextChars)
	require.NoError(t, err)

	err = svc.SessionEnd(ctx, testUserID, "ws-end-test", "Findings here.")
	require.NoError(t, err)

	// Should have: session_started + session_ended
	require.Len(t, notifier.events, 2)
	require.Equal(t, "agent.session_ended", notifier.events[1].eventType)
}

func TestService_MemoryWrite_EmitsWSEvent(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	notifier := &mockWSNotifier{}
	svc.cfg.WSNotifier = notifier

	_, err := svc.MemoryWrite(ctx, testUserID, "test-cat", "test-name", "content")
	require.NoError(t, err)

	require.Len(t, notifier.events, 1)
	require.Equal(t, "agent.memory_changed", notifier.events[0].eventType)
}

func TestService_MemoryDelete_EmitsWSEvent(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Write first.
	_, err := svc.MemoryWrite(ctx, testUserID, "del-cat", "del-name", "to delete")
	require.NoError(t, err)

	notifier := &mockWSNotifier{}
	svc.cfg.WSNotifier = notifier

	err = svc.MemoryDelete(ctx, testUserID, "del-cat", "del-name")
	require.NoError(t, err)

	require.Len(t, notifier.events, 1)
	require.Equal(t, "agent.memory_changed", notifier.events[0].eventType)
}

// Ensure time import is used.
var _ = time.Now
