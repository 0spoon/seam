package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/agent"
	seamcp "github.com/katata/seam/internal/mcp"
	"github.com/katata/seam/internal/reqctx"
)

// newTestServerWithBuffer creates a seamcp.Server backed by a buffer logger
// and returns the server and the log buffer. This is a logging-test-specific
// helper -- most tests use the simpler newTestServer from server_test.go.
func newTestServerWithBuffer(t *testing.T, mock *mockAgentService) (*seamcp.Server, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := seamcp.New(seamcp.Config{
		AgentService: mock,
		Logger:       logger,
	})
	return srv, &buf
}

// callToolViaHandleMessage invokes a tool through HandleMessage so the full
// middleware chain (auth check then logging) is exercised.
func callToolViaHandleMessage(t *testing.T, srv *seamcp.Server, ctx context.Context, toolName string, args map[string]any) []byte {
	t.Helper()
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: toolCallParams{
			Name:      toolName,
			Arguments: args,
		},
		ID: 1,
	}
	body, err := json.Marshal(req)
	require.NoError(t, err)

	result := srv.MCPServer().HandleMessage(ctx, body)
	respBytes, err := json.Marshal(result)
	require.NoError(t, err)
	return respBytes
}

func TestLoggingMiddleware_LogsSuccessfulCall(t *testing.T) {
	srv, buf := newTestServerWithBuffer(t, &mockAgentService{
		sessionListFn: func(context.Context, string, string, int) ([]*agent.Session, error) {
			return []*agent.Session{}, nil
		},
	})
	ctx := reqctx.WithUserID(context.Background(), "user-abc")

	respBytes := callToolViaHandleMessage(t, srv, ctx, "session_list", map[string]any{})

	// The response should be a successful result, not an unauthorized error.
	require.NotContains(t, string(respBytes), "unauthorized")

	log := buf.String()
	require.Contains(t, log, "mcp tool call")
	require.Contains(t, log, "session_list")
	require.Contains(t, log, "user_id")
	require.Contains(t, log, "duration_ms")
}

func TestLoggingMiddleware_LogsToolName(t *testing.T) {
	srv, buf := newTestServerWithBuffer(t, &mockAgentService{
		sessionListFn: func(context.Context, string, string, int) ([]*agent.Session, error) {
			return []*agent.Session{}, nil
		},
		memoryListFn: func(context.Context, string, string) ([]agent.MemoryItem, error) {
			return []agent.MemoryItem{}, nil
		},
	})
	ctx := reqctx.WithUserID(context.Background(), "user-123")

	// Call session_list.
	callToolViaHandleMessage(t, srv, ctx, "session_list", map[string]any{})
	logAfterFirst := buf.String()
	require.Contains(t, logAfterFirst, "tool=session_list")

	// Call memory_list.
	buf.Reset()
	callToolViaHandleMessage(t, srv, ctx, "memory_list", map[string]any{"category": "test"})
	logAfterSecond := buf.String()
	require.Contains(t, logAfterSecond, "tool=memory_list")
	// The second log entry should not contain the first tool name.
	require.NotContains(t, logAfterSecond, "tool=session_list")
}

func TestLoggingMiddleware_LogsUserID(t *testing.T) {
	srv, buf := newTestServerWithBuffer(t, &mockAgentService{
		sessionListFn: func(context.Context, string, string, int) ([]*agent.Session, error) {
			return []*agent.Session{}, nil
		},
	})

	const userID = "usr_01JQ7V8XYZABC"
	ctx := reqctx.WithUserID(context.Background(), userID)

	callToolViaHandleMessage(t, srv, ctx, "session_list", map[string]any{})

	log := buf.String()
	require.Contains(t, log, userID)
}

func TestLoggingMiddleware_LogsErrorFromService(t *testing.T) {
	mock := &mockAgentService{
		memoryReadFn: func(_ context.Context, _, _, _ string) (string, string, error) {
			return "", "", fmt.Errorf("memory_read: %w", agent.ErrNotFound)
		},
	}
	srv, buf := newTestServerWithBuffer(t, mock)
	ctx := reqctx.WithUserID(context.Background(), "user-err")

	respBytes := callToolViaHandleMessage(t, srv, ctx, "memory_read", map[string]any{
		"category": "decisions",
		"name":     "missing",
	})

	// The tool handler wraps errors into an error result (isError=true).
	require.Contains(t, string(respBytes), "isError")

	log := buf.String()
	require.Contains(t, log, "mcp tool call")
	require.Contains(t, log, "memory_read")
	require.Contains(t, log, "not found")
}

func TestLoggingMiddleware_LogsDuration(t *testing.T) {
	srv, buf := newTestServerWithBuffer(t, &mockAgentService{
		sessionListFn: func(context.Context, string, string, int) ([]*agent.Session, error) {
			return []*agent.Session{}, nil
		},
	})
	ctx := reqctx.WithUserID(context.Background(), "user-dur")

	callToolViaHandleMessage(t, srv, ctx, "session_list", map[string]any{})

	log := buf.String()
	require.Contains(t, log, "duration_ms=")
}

func TestLoggingMiddleware_ChainsWithAuthMiddleware(t *testing.T) {
	// Middleware registration order in New(): [authCheck, logging].
	// mcp-go applies in reverse: authCheck(logging(handler)).
	// Execution order: authCheck -> logging -> handler.
	// When authCheck rejects (no user ID), logging is never reached.
	srv, buf := newTestServerWithBuffer(t, &mockAgentService{})

	// Call without setting user ID in context.
	ctx := context.Background()
	respBytes := callToolViaHandleMessage(t, srv, ctx, "session_list", map[string]any{})

	// The auth middleware returns an error result with "unauthorized".
	require.Contains(t, string(respBytes), "unauthorized")

	// Since auth runs before logging in the chain, the logging middleware
	// is never invoked for unauthorized requests.
	log := buf.String()
	require.Empty(t, log, "logging middleware should not run when auth rejects")
}
