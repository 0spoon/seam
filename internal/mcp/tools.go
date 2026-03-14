package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/katata/seam/internal/agent"
	"github.com/katata/seam/internal/reqctx"
)

// Input validation limits.
const (
	maxCategoryLen = 100
	maxNameLen     = 200
	maxContentLen  = 512 * 1024 // 512 KB
	maxQueryLen    = 10 * 1024  // 10 KB
	maxSessionList = 100
)

// registerTools registers all MCP tools on the server.
func (s *Server) registerTools() {
	s.mcp.AddTools(
		// Session management tools.
		mcpserver.ServerTool{Tool: sessionStartTool(), Handler: s.handleSessionStart},
		mcpserver.ServerTool{Tool: sessionPlanSetTool(), Handler: s.handleSessionPlanSet},
		mcpserver.ServerTool{Tool: sessionProgressUpdateTool(), Handler: s.handleSessionProgressUpdate},
		mcpserver.ServerTool{Tool: sessionContextSetTool(), Handler: s.handleSessionContextSet},
		mcpserver.ServerTool{Tool: sessionEndTool(), Handler: s.handleSessionEnd},
		mcpserver.ServerTool{Tool: sessionListTool(), Handler: s.handleSessionList},

		// Knowledge / long-term memory tools.
		mcpserver.ServerTool{Tool: memoryReadTool(), Handler: s.handleMemoryRead},
		mcpserver.ServerTool{Tool: memoryWriteTool(), Handler: s.handleMemoryWrite},
		mcpserver.ServerTool{Tool: memoryAppendTool(), Handler: s.handleMemoryAppend},
		mcpserver.ServerTool{Tool: memoryListTool(), Handler: s.handleMemoryList},
		mcpserver.ServerTool{Tool: memoryDeleteTool(), Handler: s.handleMemoryDelete},

		// Context gathering.
		mcpserver.ServerTool{Tool: contextGatherTool(), Handler: s.handleContextGather},
	)
}

// --- Tool Definitions ---

func sessionStartTool() mcp.Tool {
	return mcp.NewTool("session_start",
		mcp.WithDescription("Start or resume a named agent session. Returns a briefing with context."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Session name (e.g. 'refactor-auth' or 'refactor-auth/analyze')")),
		mcp.WithNumber("max_context_chars", mcp.Description("Maximum characters for briefing context (default: 4000)")),
	)
}

func sessionPlanSetTool() mcp.Tool {
	return mcp.NewTool("session_plan_set",
		mcp.WithDescription("Set or update the plan for a session."),
		mcp.WithString("session_name", mcp.Required(), mcp.Description("Session name")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Plan content (markdown)")),
	)
}

func sessionProgressUpdateTool() mcp.Tool {
	return mcp.NewTool("session_progress_update",
		mcp.WithDescription("Update progress for a task within a session."),
		mcp.WithString("session_name", mcp.Required(), mcp.Description("Session name")),
		mcp.WithString("task", mcp.Required(), mcp.Description("Task description")),
		mcp.WithString("status", mcp.Required(), mcp.Enum("pending", "in_progress", "completed", "blocked"), mcp.Description("Task status")),
		mcp.WithString("notes", mcp.Description("Optional notes about the progress")),
	)
}

func sessionContextSetTool() mcp.Tool {
	return mcp.NewTool("session_context_set",
		mcp.WithDescription("Set or update the context note for a session."),
		mcp.WithString("session_name", mcp.Required(), mcp.Description("Session name")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Context content")),
	)
}

func sessionEndTool() mcp.Tool {
	return mcp.NewTool("session_end",
		mcp.WithDescription("End a session with a compact summary of findings."),
		mcp.WithString("session_name", mcp.Required(), mcp.Description("Session name")),
		mcp.WithString("findings", mcp.Required(), mcp.Description("Compact summary of findings (max 1500 chars)")),
	)
}

func sessionListTool() mcp.Tool {
	return mcp.NewTool("session_list",
		mcp.WithDescription("List agent sessions."),
		mcp.WithString("status", mcp.Enum("active", "completed", "archived", "all"), mcp.Description("Filter by status (default: active)")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of sessions (default: 20)")),
	)
}

func memoryReadTool() mcp.Tool {
	return mcp.NewTool("memory_read",
		mcp.WithDescription("Read a knowledge note by category and name."),
		mcp.WithString("category", mcp.Required(), mcp.Description("Knowledge category")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Knowledge name")),
	)
}

func memoryWriteTool() mcp.Tool {
	return mcp.NewTool("memory_write",
		mcp.WithDescription("Create or update a knowledge note."),
		mcp.WithString("category", mcp.Required(), mcp.Description("Knowledge category")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Knowledge name")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Note content")),
	)
}

func memoryAppendTool() mcp.Tool {
	return mcp.NewTool("memory_append",
		mcp.WithDescription("Append content to an existing knowledge note."),
		mcp.WithString("category", mcp.Required(), mcp.Description("Knowledge category")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Knowledge name")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to append")),
	)
}

func memoryListTool() mcp.Tool {
	return mcp.NewTool("memory_list",
		mcp.WithDescription("List knowledge notes, optionally filtered by category."),
		mcp.WithString("category", mcp.Description("Optional category filter")),
	)
}

func memoryDeleteTool() mcp.Tool {
	return mcp.NewTool("memory_delete",
		mcp.WithDescription("Delete a knowledge note."),
		mcp.WithString("category", mcp.Required(), mcp.Description("Knowledge category")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Knowledge name")),
	)
}

func contextGatherTool() mcp.Tool {
	return mcp.NewTool("context_gather",
		mcp.WithDescription("Search for relevant context across notes, budgeted to a character limit."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
		mcp.WithNumber("max_context_chars", mcp.Description("Maximum characters for results (default: 3000)")),
		mcp.WithString("scope", mcp.Enum("agent", "user", "all"), mcp.Description("Search scope (default: all)")),
	)
}

// --- Tool Handlers ---

func (s *Server) handleSessionStart(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: name"), nil
	}

	maxChars := req.GetInt("max_context_chars", agent.DefaultMaxContextChars)

	briefing, err := s.cfg.AgentService.SessionStart(ctx, userID, name, maxChars)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("session_start", err)), nil
	}

	data, jsonErr := json.Marshal(briefing)
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal briefing"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleSessionPlanSet(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	sessionName, err := req.RequireString("session_name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: session_name"), nil
	}
	content, err := req.RequireString("content")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: content"), nil
	}
	if len(content) > maxContentLen {
		return mcp.NewToolResultError(fmt.Sprintf("content too long: %d bytes exceeds limit of %d", len(content), maxContentLen)), nil
	}

	noteID, err := s.cfg.AgentService.SessionPlanSet(ctx, userID, sessionName, content)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("session_plan_set", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf(`{"note_id":"%s"}`, noteID)), nil
}

func (s *Server) handleSessionProgressUpdate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	sessionName, err := req.RequireString("session_name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: session_name"), nil
	}
	task, err := req.RequireString("task")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: task"), nil
	}
	status, err := req.RequireString("status")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: status"), nil
	}
	notes := req.GetString("notes", "")

	noteID, err := s.cfg.AgentService.SessionProgressUpdate(ctx, userID, sessionName, task, status, notes)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("session_progress_update", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf(`{"note_id":"%s"}`, noteID)), nil
}

func (s *Server) handleSessionContextSet(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	sessionName, err := req.RequireString("session_name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: session_name"), nil
	}
	content, err := req.RequireString("content")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: content"), nil
	}
	if len(content) > maxContentLen {
		return mcp.NewToolResultError(fmt.Sprintf("content too long: %d bytes exceeds limit of %d", len(content), maxContentLen)), nil
	}

	noteID, err := s.cfg.AgentService.SessionContextSet(ctx, userID, sessionName, content)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("session_context_set", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf(`{"note_id":"%s"}`, noteID)), nil
}

func (s *Server) handleSessionEnd(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	sessionName, err := req.RequireString("session_name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: session_name"), nil
	}
	findings, err := req.RequireString("findings")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: findings"), nil
	}

	if err := s.cfg.AgentService.SessionEnd(ctx, userID, sessionName, findings); err != nil {
		return mcp.NewToolResultError(sanitizeError("session_end", err)), nil
	}

	return mcp.NewToolResultText(`{"status":"completed"}`), nil
}

func (s *Server) handleSessionList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	status := req.GetString("status", "active")
	if status == "all" {
		status = ""
	}
	limit := req.GetInt("limit", 20)
	if limit > maxSessionList {
		limit = maxSessionList
	}

	sessions, err := s.cfg.AgentService.SessionList(ctx, userID, status, limit)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("session_list", err)), nil
	}

	data, jsonErr := json.Marshal(sessions)
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal sessions"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleMemoryRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	category, err := req.RequireString("category")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: category"), nil
	}
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: name"), nil
	}
	if errMsg := validateCategoryName(category, name); errMsg != "" {
		return mcp.NewToolResultError(errMsg), nil
	}

	title, body, err := s.cfg.AgentService.MemoryRead(ctx, userID, category, name)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("memory_read", err)), nil
	}

	result := map[string]string{"title": title, "body": body}
	data, jsonErr := json.Marshal(result)
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal result"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleMemoryWrite(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	category, err := req.RequireString("category")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: category"), nil
	}
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: name"), nil
	}
	content, err := req.RequireString("content")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: content"), nil
	}
	if errMsg := validateCategoryName(category, name); errMsg != "" {
		return mcp.NewToolResultError(errMsg), nil
	}
	if len(content) > maxContentLen {
		return mcp.NewToolResultError(fmt.Sprintf("content too long: %d bytes exceeds limit of %d", len(content), maxContentLen)), nil
	}

	noteID, err := s.cfg.AgentService.MemoryWrite(ctx, userID, category, name, content)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("memory_write", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf(`{"note_id":"%s"}`, noteID)), nil
}

func (s *Server) handleMemoryAppend(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	category, err := req.RequireString("category")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: category"), nil
	}
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: name"), nil
	}
	content, err := req.RequireString("content")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: content"), nil
	}
	if errMsg := validateCategoryName(category, name); errMsg != "" {
		return mcp.NewToolResultError(errMsg), nil
	}
	if len(content) > maxContentLen {
		return mcp.NewToolResultError(fmt.Sprintf("content too long: %d bytes exceeds limit of %d", len(content), maxContentLen)), nil
	}

	if err := s.cfg.AgentService.MemoryAppend(ctx, userID, category, name, content); err != nil {
		return mcp.NewToolResultError(sanitizeError("memory_append", err)), nil
	}

	return mcp.NewToolResultText(`{"status":"appended"}`), nil
}

func (s *Server) handleMemoryList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	category := req.GetString("category", "")
	if len(category) > maxCategoryLen {
		return mcp.NewToolResultError(fmt.Sprintf("category too long: %d chars exceeds limit of %d", len(category), maxCategoryLen)), nil
	}

	items, err := s.cfg.AgentService.MemoryList(ctx, userID, category)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("memory_list", err)), nil
	}

	data, jsonErr := json.Marshal(items)
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal items"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleMemoryDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	category, err := req.RequireString("category")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: category"), nil
	}
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: name"), nil
	}
	if errMsg := validateCategoryName(category, name); errMsg != "" {
		return mcp.NewToolResultError(errMsg), nil
	}

	if err := s.cfg.AgentService.MemoryDelete(ctx, userID, category, name); err != nil {
		return mcp.NewToolResultError(sanitizeError("memory_delete", err)), nil
	}

	return mcp.NewToolResultText(`{"status":"deleted"}`), nil
}

func (s *Server) handleContextGather(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	query, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: query"), nil
	}
	if len(query) > maxQueryLen {
		return mcp.NewToolResultError(fmt.Sprintf("query too long: %d bytes exceeds limit of %d", len(query), maxQueryLen)), nil
	}
	maxChars := req.GetInt("max_context_chars", 3000)
	// scope is accepted but not used for filtering in v1 (requires ChromaDB metadata changes)
	_ = req.GetString("scope", "all")

	results, err := s.cfg.AgentService.ContextGather(ctx, userID, query, maxChars)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("context_gather", err)), nil
	}

	data, jsonErr := json.Marshal(map[string]any{"results": results})
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal results"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// --- Error Sanitization ---

// sanitizeError maps domain errors to user-safe messages. Internal error details
// (file paths, DB errors, etc.) are not exposed to the client.
func sanitizeError(tool string, err error) string {
	switch {
	case errors.Is(err, agent.ErrNotFound):
		return tool + ": not found"
	case errors.Is(err, agent.ErrSessionNotActive):
		return tool + ": session is not active"
	case errors.Is(err, agent.ErrFindingsTooLong):
		return tool + ": findings exceed maximum length (1500 chars)"
	case errors.Is(err, agent.ErrFindingsRequired):
		return tool + ": findings are required"
	case errors.Is(err, agent.ErrInvalidSessionName):
		return tool + ": invalid session name"
	default:
		return tool + ": internal error"
	}
}

// --- Input Validation ---

// validateCategoryName checks category and name parameters for length and content.
func validateCategoryName(category, name string) string {
	if len(category) > maxCategoryLen {
		return fmt.Sprintf("category too long: %d chars exceeds limit of %d", len(category), maxCategoryLen)
	}
	if len(name) > maxNameLen {
		return fmt.Sprintf("name too long: %d chars exceeds limit of %d", len(name), maxNameLen)
	}
	if hasControlChars(category) {
		return "category contains invalid control characters"
	}
	if hasControlChars(name) {
		return "name contains invalid control characters"
	}
	return ""
}

// hasControlChars returns true if s contains control characters (bytes 0x00-0x1F
// except \t, \n, \r).
func hasControlChars(s string) bool {
	for _, c := range s {
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			return true
		}
	}
	return false
}
