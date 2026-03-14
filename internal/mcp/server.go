// Package mcp implements the MCP (Model Context Protocol) server for Seam,
// exposing agent memory tools via Streamable HTTP transport.
package mcp

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/katata/seam/internal/agent"
	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/reqctx"
)

// AgentService defines the interface for the agent service used by MCP tools.
// This allows mocking in tests.
type AgentService interface {
	SessionStart(ctx context.Context, userID, name string, maxContextChars int) (*agent.Briefing, error)
	SessionEnd(ctx context.Context, userID, sessionName, findings string) error
	SessionList(ctx context.Context, userID, status string, limit int) ([]*agent.Session, error)
	SessionPlanSet(ctx context.Context, userID, sessionName, content string) (string, error)
	SessionProgressUpdate(ctx context.Context, userID, sessionName, task, status, notes string) (string, error)
	SessionContextSet(ctx context.Context, userID, sessionName, content string) (string, error)

	MemoryRead(ctx context.Context, userID, category, name string) (string, string, error)
	MemoryWrite(ctx context.Context, userID, category, name, content string) (string, error)
	MemoryAppend(ctx context.Context, userID, category, name, content string) error
	MemoryList(ctx context.Context, userID, category string) ([]agent.MemoryItem, error)
	MemoryDelete(ctx context.Context, userID, category, name string) error

	ContextGather(ctx context.Context, userID, query string, maxChars int) ([]agent.KnowledgeHit, error)
}

// NoteReader defines the interface for reading notes, used by notes_read tool.
type NoteReader interface {
	Get(ctx context.Context, userID, noteID string) (interface{ GetTitle() string }, error)
}

// Config holds dependencies for the MCP server.
type Config struct {
	AgentService AgentService
	Logger       *slog.Logger
}

// Server wraps an MCP server with agent tools.
type Server struct {
	mcp    *mcpserver.MCPServer
	cfg    Config
	logger *slog.Logger
}

// New creates the MCP server with all agent tools registered.
func New(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	s := &Server{
		cfg:    cfg,
		logger: cfg.Logger,
	}

	mcpSrv := mcpserver.NewMCPServer(
		"Seam Agent Memory",
		"1.0.0",
		mcpserver.WithToolCapabilities(false),
		mcpserver.WithRecovery(),
		mcpserver.WithToolHandlerMiddleware(authCheckMiddleware),
		mcpserver.WithToolHandlerMiddleware(s.loggingMiddleware()),
	)

	s.mcp = mcpSrv
	s.registerTools()

	return s
}

// Handler returns an http.Handler for mounting at /api/mcp on the chi router.
// Auth is handled via WithHTTPContextFunc which extracts the JWT from the
// Authorization header and injects user ID into the context.
func (s *Server) Handler(jwtMgr *auth.JWTManager) http.Handler {
	return mcpserver.NewStreamableHTTPServer(s.mcp,
		mcpserver.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			authHeader := r.Header.Get("Authorization")
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				return ctx
			}
			claims, err := jwtMgr.VerifyAccessToken(parts[1])
			if err != nil {
				return ctx
			}
			ctx = reqctx.WithUserID(ctx, claims.UserID)
			ctx = reqctx.WithUsername(ctx, claims.Username)
			return ctx
		}),
	)
}

// MCPServer returns the underlying MCPServer for testing.
func (s *Server) MCPServer() *mcpserver.MCPServer {
	return s.mcp
}

// authCheckMiddleware rejects tool calls when no user ID is present in context.
func authCheckMiddleware(next mcpserver.ToolHandlerFunc) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if reqctx.UserIDFromContext(ctx) == "" {
			return mcp.NewToolResultError("unauthorized: valid JWT required"), nil
		}
		return next(ctx, req)
	}
}
