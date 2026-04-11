package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/katata/seam/internal/agent"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/task"
	"github.com/katata/seam/internal/template"
	"github.com/katata/seam/internal/validate"
	"github.com/katata/seam/internal/webhook"
)

// Input validation limits.
const (
	maxCategoryLen = 100
	maxNameLen     = 200
	maxContentLen  = 512 * 1024 // 512 KB
	maxQueryLen    = 10 * 1024  // 10 KB
	maxSessionList = 1000
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

		// User note access tools.
		mcpserver.ServerTool{Tool: notesSearchTool(), Handler: s.handleNotesSearch},
		mcpserver.ServerTool{Tool: notesReadTool(), Handler: s.handleNotesRead},
		mcpserver.ServerTool{Tool: notesListTool(), Handler: s.handleNotesList},
		mcpserver.ServerTool{Tool: notesCreateTool(), Handler: s.handleNotesCreate},
		mcpserver.ServerTool{Tool: notesUpdateTool(), Handler: s.handleNotesUpdate},
		mcpserver.ServerTool{Tool: notesDeleteTool(), Handler: s.handleNotesDelete},
		mcpserver.ServerTool{Tool: notesTagsTool(), Handler: s.handleNotesTags},
		mcpserver.ServerTool{Tool: notesDailyTool(), Handler: s.handleNotesDaily},
		mcpserver.ServerTool{Tool: notesAppendTool(), Handler: s.handleNotesAppend},
		mcpserver.ServerTool{Tool: notesChangelogTool(), Handler: s.handleNotesChangelog},
		mcpserver.ServerTool{Tool: notesVersionsTool(), Handler: s.handleNotesVersions},

		// Project management tools.
		mcpserver.ServerTool{Tool: projectListTool(), Handler: s.handleProjectList},
		mcpserver.ServerTool{Tool: projectCreateTool(), Handler: s.handleProjectCreate},

		// V2: Memory search and session metrics.
		mcpserver.ServerTool{Tool: memorySearchTool(), Handler: s.handleMemorySearch},
		mcpserver.ServerTool{Tool: sessionMetricsTool(), Handler: s.handleSessionMetrics},

		// V5: Research lab / experiment tracking.
		mcpserver.ServerTool{Tool: labOpenTool(), Handler: s.handleLabOpen},
		mcpserver.ServerTool{Tool: trialRecordTool(), Handler: s.handleTrialRecord},
		mcpserver.ServerTool{Tool: decisionRecordTool(), Handler: s.handleDecisionRecord},
		mcpserver.ServerTool{Tool: trialQueryTool(), Handler: s.handleTrialQuery},
	)

	// Task tracking tools (registered only if TaskService is configured).
	if s.cfg.TaskService != nil {
		s.mcp.AddTools(
			mcpserver.ServerTool{Tool: tasksListTool(), Handler: s.handleTasksList},
			mcpserver.ServerTool{Tool: tasksSummaryTool(), Handler: s.handleTasksSummary},
			mcpserver.ServerTool{Tool: tasksToggleTool(), Handler: s.handleTasksToggle},
		)
	}

	// Graph tools (registered only if GraphService is configured).
	if s.cfg.GraphService != nil {
		s.mcp.AddTools(
			mcpserver.ServerTool{Tool: graphNeighborsTool(), Handler: s.handleGraphNeighbors},
		)
	}

	// Review tools (registered only if ReviewService is configured).
	if s.cfg.ReviewService != nil {
		s.mcp.AddTools(
			mcpserver.ServerTool{Tool: reviewQueueTool(), Handler: s.handleReviewQueue},
		)
	}

	// Template tools (registered only if TemplateService is configured).
	if s.cfg.TemplateService != nil {
		s.mcp.AddTools(
			mcpserver.ServerTool{Tool: notesFromTemplateTool(), Handler: s.handleNotesFromTemplate},
		)
	}

	// Webhook tools (registered only if WebhookService is configured).
	if s.cfg.WebhookService != nil {
		s.mcp.AddTools(
			mcpserver.ServerTool{Tool: webhookRegisterTool(), Handler: s.handleWebhookRegister},
			mcpserver.ServerTool{Tool: webhookListTool(), Handler: s.handleWebhookList},
			mcpserver.ServerTool{Tool: webhookDeleteTool(), Handler: s.handleWebhookDelete},
		)
	}
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
		mcp.WithNumber("recency_bias", mcp.Description("Recency bias (0.0-1.0). Higher values boost recent notes. Default: 0.0")),
	)
}

func notesSearchTool() mcp.Tool {
	return mcp.NewTool("notes_search",
		mcp.WithDescription("Full-text search across user notes."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of results (default: 10)")),
		mcp.WithNumber("recency_bias", mcp.Description("Recency bias (0.0-1.0). Higher values boost recent notes. Default: 0.0")),
	)
}

func notesReadTool() mcp.Tool {
	return mcp.NewTool("notes_read",
		mcp.WithDescription("Read a note by ID. Returns full title, body, and tags."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Note ID")),
	)
}

func notesListTool() mcp.Tool {
	return mcp.NewTool("notes_list",
		mcp.WithDescription("List notes with optional project and tag filtering."),
		mcp.WithString("project", mcp.Description("Project slug to filter by")),
		mcp.WithString("tag", mcp.Description("Tag to filter by")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of results (default: 20)")),
	)
}

func notesCreateTool() mcp.Tool {
	return mcp.NewTool("notes_create",
		mcp.WithDescription("Create a user note. Auto-tagged with 'created-by:agent'."),
		mcp.WithString("title", mcp.Required(), mcp.Description("Note title")),
		mcp.WithString("body", mcp.Required(), mcp.Description("Note body (markdown)")),
		mcp.WithString("project", mcp.Description("Project slug (optional)")),
		mcp.WithString("tags", mcp.Description("Comma-separated tags (optional)")),
	)
}

func memorySearchTool() mcp.Tool {
	return mcp.NewTool("memory_search",
		mcp.WithDescription("Search agent knowledge notes using FTS and semantic search. Returns results scoped to agent memory only."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of results (default: 10)")),
	)
}

func sessionMetricsTool() mcp.Tool {
	return mcp.NewTool("session_metrics",
		mcp.WithDescription("Get aggregate statistics for a session: tool calls, durations, notes created, errors."),
		mcp.WithString("session_name", mcp.Required(), mcp.Description("Session name")),
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

	data, _ := json.Marshal(map[string]string{"note_id": noteID})
	return mcp.NewToolResultText(string(data)), nil
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

	data, _ := json.Marshal(map[string]string{"note_id": noteID})
	return mcp.NewToolResultText(string(data)), nil
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

	data, _ := json.Marshal(map[string]string{"note_id": noteID})
	return mcp.NewToolResultText(string(data)), nil
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

	data, _ := json.Marshal(map[string]string{"note_id": noteID})
	return mcp.NewToolResultText(string(data)), nil
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
	scope := req.GetString("scope", "all")
	recencyBias := clampRecencyBias(req.GetFloat("recency_bias", 0.0))

	results, err := s.cfg.AgentService.ContextGather(ctx, userID, query, scope, maxChars, recencyBias)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("context_gather", err)), nil
	}

	data, jsonErr := json.Marshal(map[string]any{"results": results})
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal results"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleNotesSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	query, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: query"), nil
	}
	if len(query) > maxQueryLen {
		return mcp.NewToolResultError(fmt.Sprintf("query too long: %d bytes exceeds limit of %d", len(query), maxQueryLen)), nil
	}
	limit := req.GetInt("limit", 10)
	if limit > maxSessionList {
		limit = maxSessionList
	}
	recencyBias := clampRecencyBias(req.GetFloat("recency_bias", 0.0))

	results, err := s.cfg.AgentService.NotesSearch(ctx, userID, query, limit, recencyBias)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("notes_search", err)), nil
	}

	data, jsonErr := json.Marshal(results)
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal results"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleNotesRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	noteID, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: id"), nil
	}

	n, err := s.cfg.AgentService.NotesRead(ctx, userID, noteID)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("notes_read", err)), nil
	}

	result := map[string]interface{}{
		"id":    n.ID,
		"title": n.Title,
		"body":  n.Body,
		"tags":  n.Tags,
	}
	data, jsonErr := json.Marshal(result)
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal result"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleNotesList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	projectSlug := req.GetString("project", "")
	tag := req.GetString("tag", "")
	limit := req.GetInt("limit", 20)
	if limit > maxSessionList {
		limit = maxSessionList
	}

	notes, total, err := s.cfg.AgentService.NotesList(ctx, userID, projectSlug, tag, limit)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("notes_list", err)), nil
	}

	// Return summaries (not full bodies) to keep response compact.
	type noteSummary struct {
		ID        string   `json:"id"`
		Title     string   `json:"title"`
		Tags      []string `json:"tags,omitempty"`
		UpdatedAt string   `json:"updated_at"`
	}
	summaries := make([]noteSummary, 0, len(notes))
	for _, n := range notes {
		summaries = append(summaries, noteSummary{
			ID:        n.ID,
			Title:     n.Title,
			Tags:      n.Tags,
			UpdatedAt: n.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	data, jsonErr := json.Marshal(map[string]interface{}{
		"notes": summaries,
		"total": total,
	})
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal results"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleNotesCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	title, err := req.RequireString("title")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: title"), nil
	}
	body, err := req.RequireString("body")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: body"), nil
	}
	if len(body) > maxContentLen {
		return mcp.NewToolResultError(fmt.Sprintf("body too long: %d bytes exceeds limit of %d", len(body), maxContentLen)), nil
	}

	projectSlug := req.GetString("project", "")
	tagsStr := req.GetString("tags", "")

	// Parse comma-separated tags.
	var tags []string
	if tagsStr != "" {
		for _, t := range strings.Split(tagsStr, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tags = append(tags, t)
			}
		}
	}

	n, err := s.cfg.AgentService.NotesCreate(ctx, userID, title, body, projectSlug, tags)
	if err != nil {
		s.logger.Warn("notes_create failed", "error", err, "title", title, "project", projectSlug)
		return mcp.NewToolResultError(sanitizeError("notes_create", err)), nil
	}

	data, _ := json.Marshal(map[string]string{"note_id": n.ID})
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleMemorySearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	query, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: query"), nil
	}
	if len(query) > maxQueryLen {
		return mcp.NewToolResultError(fmt.Sprintf("query too long: %d bytes exceeds limit of %d", len(query), maxQueryLen)), nil
	}
	limit := req.GetInt("limit", 10)
	if limit > maxSessionList {
		limit = maxSessionList
	}

	results, err := s.cfg.AgentService.MemorySearch(ctx, userID, query, limit)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("memory_search", err)), nil
	}

	data, jsonErr := json.Marshal(map[string]interface{}{"results": results})
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal results"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleSessionMetrics(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	sessionName, err := req.RequireString("session_name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: session_name"), nil
	}

	metrics, err := s.cfg.AgentService.SessionMetrics(ctx, userID, sessionName)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("session_metrics", err)), nil
	}

	data, jsonErr := json.Marshal(metrics)
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal metrics"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// --- Notes Update/Delete/Tags/Daily Tool Definitions ---

func notesUpdateTool() mcp.Tool {
	return mcp.NewTool("notes_update",
		mcp.WithDescription("Update an existing note. Only provided fields are changed; omitted fields are left as-is."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Note ID")),
		mcp.WithString("title", mcp.Description("New title (omit to keep current)")),
		mcp.WithString("body", mcp.Description("New body content in markdown (omit to keep current)")),
		mcp.WithString("project", mcp.Description("Project slug to move note to (empty string = inbox, omit to keep current)")),
		mcp.WithString("tags", mcp.Description("Comma-separated tags, replaces all existing tags (omit to keep current)")),
	)
}

func notesDeleteTool() mcp.Tool {
	return mcp.NewTool("notes_delete",
		mcp.WithDescription("Delete a note by ID. This is permanent."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Note ID")),
	)
}

func notesTagsTool() mcp.Tool {
	return mcp.NewTool("notes_tags",
		mcp.WithDescription("List all tags in use across notes, with usage counts."),
	)
}

func notesDailyTool() mcp.Tool {
	return mcp.NewTool("notes_daily",
		mcp.WithDescription("Get or create today's daily note. Returns the full note."),
		mcp.WithString("date", mcp.Description("Date in YYYY-MM-DD format (default: today)")),
	)
}

// --- Project Tool Definitions ---

func projectListTool() mcp.Tool {
	return mcp.NewTool("project_list",
		mcp.WithDescription("List all projects."),
	)
}

func projectCreateTool() mcp.Tool {
	return mcp.NewTool("project_create",
		mcp.WithDescription("Create a new project."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Project name")),
		mcp.WithString("description", mcp.Description("Project description (optional)")),
	)
}

// --- Tasks Toggle Tool Definition ---

func tasksToggleTool() mcp.Tool {
	return mcp.NewTool("tasks_toggle",
		mcp.WithDescription("Toggle a task's done status."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Task ID")),
		mcp.WithString("done", mcp.Required(), mcp.Enum("true", "false"), mcp.Description("Set done status")),
	)
}

// --- Notes Update/Delete/Tags/Daily Handlers ---

func (s *Server) handleNotesUpdate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	noteID, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: id"), nil
	}

	// Build update params: only set fields that were explicitly provided.
	args := req.GetArguments()
	var title, body, projectSlug *string
	var tags *[]string

	if v, ok := args["title"]; ok {
		if s, ok := v.(string); ok {
			title = &s
		}
	}
	if v, ok := args["body"]; ok {
		if s, ok := v.(string); ok {
			if len(s) > maxContentLen {
				return mcp.NewToolResultError(fmt.Sprintf("body too long: %d bytes exceeds limit of %d", len(s), maxContentLen)), nil
			}
			body = &s
		}
	}
	if v, ok := args["project"]; ok {
		if s, ok := v.(string); ok {
			projectSlug = &s
		}
	}
	if v, ok := args["tags"]; ok {
		if s, ok := v.(string); ok {
			parsed := parseTags(s)
			tags = &parsed
		}
	}

	// At least one field must be provided.
	if title == nil && body == nil && projectSlug == nil && tags == nil {
		return mcp.NewToolResultError("at least one field (title, body, project, tags) must be provided"), nil
	}

	n, err := s.cfg.AgentService.NotesUpdate(ctx, userID, noteID, title, body, projectSlug, tags)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("notes_update", err)), nil
	}

	data, _ := json.Marshal(map[string]string{"note_id": n.ID, "title": n.Title})
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleNotesDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	noteID, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: id"), nil
	}

	if err := s.cfg.AgentService.NotesDelete(ctx, userID, noteID); err != nil {
		return mcp.NewToolResultError(sanitizeError("notes_delete", err)), nil
	}

	return mcp.NewToolResultText(`{"status":"deleted"}`), nil
}

func (s *Server) handleNotesTags(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)

	tags, err := s.cfg.AgentService.NotesTags(ctx, userID)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("notes_tags", err)), nil
	}

	data, jsonErr := json.Marshal(map[string]interface{}{"tags": tags})
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal tags"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleNotesDaily(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)

	dateStr := req.GetString("date", "")
	var date time.Time
	if dateStr != "" {
		var parseErr error
		date, parseErr = time.Parse("2006-01-02", dateStr)
		if parseErr != nil {
			return mcp.NewToolResultError("invalid date format, expected YYYY-MM-DD"), nil
		}
	} else {
		date = time.Now()
	}

	n, err := s.cfg.AgentService.NotesDaily(ctx, userID, date)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("notes_daily", err)), nil
	}

	result := map[string]interface{}{
		"id":    n.ID,
		"title": n.Title,
		"body":  n.Body,
		"tags":  n.Tags,
	}
	data, jsonErr := json.Marshal(result)
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal result"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// --- Project Handlers ---

func (s *Server) handleProjectList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)

	projects, err := s.cfg.AgentService.ProjectList(ctx, userID)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("project_list", err)), nil
	}

	type projectSummary struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description,omitempty"`
	}
	summaries := make([]projectSummary, 0, len(projects))
	for _, p := range projects {
		summaries = append(summaries, projectSummary{
			ID:          p.ID,
			Name:        p.Name,
			Slug:        p.Slug,
			Description: p.Description,
		})
	}

	data, jsonErr := json.Marshal(map[string]interface{}{"projects": summaries})
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal projects"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleProjectCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: name"), nil
	}
	description := req.GetString("description", "")

	p, err := s.cfg.AgentService.ProjectCreate(ctx, userID, name, description)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("project_create", err)), nil
	}

	data, _ := json.Marshal(map[string]string{"project_id": p.ID, "name": p.Name, "slug": p.Slug})
	return mcp.NewToolResultText(string(data)), nil
}

// --- Tasks Toggle Handler ---

func (s *Server) handleTasksToggle(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	taskID, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: id"), nil
	}
	doneStr, err := req.RequireString("done")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: done"), nil
	}
	done := doneStr == "true"

	if err := s.cfg.TaskService.ToggleDone(ctx, userID, taskID, done); err != nil {
		return mcp.NewToolResultError(sanitizeError("tasks_toggle", err)), nil
	}

	data, _ := json.Marshal(map[string]interface{}{"task_id": taskID, "done": done})
	return mcp.NewToolResultText(string(data)), nil
}

// --- V4 Tool Definitions: Append, Changelog, Versions ---

func notesAppendTool() mcp.Tool {
	return mcp.NewTool("notes_append",
		mcp.WithDescription("Append a timestamped line to a note's body. Ideal for building research logs, debug journals, or activity feeds without replacing existing content."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Note ID")),
		mcp.WithString("text", mcp.Required(), mcp.Description("Text to append")),
	)
}

func notesChangelogTool() mcp.Tool {
	return mcp.NewTool("notes_changelog",
		mcp.WithDescription("List notes created or modified within a date range. Returns compact summaries sorted by most recent first. Use this to understand what changed recently."),
		mcp.WithString("since", mcp.Description("Start date in YYYY-MM-DD format (default: 7 days ago)")),
		mcp.WithString("until", mcp.Description("End date in YYYY-MM-DD format (default: now)")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of results (default: 20)")),
	)
}

func notesVersionsTool() mcp.Tool {
	return mcp.NewTool("notes_versions",
		mcp.WithDescription("List version history for a note, or retrieve a specific past version. Use without 'version' to list all versions; provide 'version' to retrieve that snapshot."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Note ID")),
		mcp.WithNumber("version", mcp.Description("Specific version number to retrieve (omit to list all versions)")),
		mcp.WithNumber("limit", mcp.Description("Maximum versions to list (default: 10, ignored when retrieving specific version)")),
	)
}

// --- Graph Tool Definition ---

func graphNeighborsTool() mcp.Tool {
	return mcp.NewTool("graph_neighbors",
		mcp.WithDescription("Explore a note's neighborhood in the knowledge graph. Returns backlinks (notes that link to it), two-hop connections (notes linked through intermediaries), and optionally the note's direct outgoing links. Use this for structural discovery beyond text search."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Note ID to explore")),
		mcp.WithString("include_two_hop", mcp.Enum("true", "false"), mcp.Description("Include two-hop connections (default: true)")),
	)
}

// --- Review Queue Tool Definition ---

func reviewQueueTool() mcp.Tool {
	return mcp.NewTool("review_queue",
		mcp.WithDescription("Pull items from the knowledge gardening queue. Returns notes that need attention: orphans (no links), untagged notes, and unsorted inbox items. Each comes with actionable suggestions. Use this for autonomous knowledge maintenance."),
		mcp.WithNumber("limit", mcp.Description("Maximum items to return (default: 10)")),
	)
}

// --- Template Tool Definition ---

func notesFromTemplateTool() mcp.Tool {
	return mcp.NewTool("notes_from_template",
		mcp.WithDescription("Create a note from a named template with variable substitution. Built-in vars: {{date}}, {{datetime}}, {{year}}, {{month}}, {{day}}, {{time}}. Call with just 'list: true' to see available templates."),
		mcp.WithString("template", mcp.Description("Template name (e.g. 'meeting-notes', 'research-summary')")),
		mcp.WithString("title", mcp.Description("Note title")),
		mcp.WithString("project", mcp.Description("Project slug (optional)")),
		mcp.WithString("tags", mcp.Description("Comma-separated tags (optional)")),
		mcp.WithString("vars", mcp.Description("JSON object of template variables (e.g. '{\"topic\":\"auth\",\"attendees\":\"Alice, Bob\"}')")),
		mcp.WithString("list", mcp.Enum("true", "false"), mcp.Description("Set to 'true' to list available templates instead of creating a note")),
	)
}

// --- V4 Handlers: Append, Changelog, Versions ---

func (s *Server) handleNotesAppend(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	noteID, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: id"), nil
	}
	text, err := req.RequireString("text")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: text"), nil
	}
	if len(text) > maxContentLen {
		return mcp.NewToolResultError(fmt.Sprintf("text too long: %d bytes exceeds limit of %d", len(text), maxContentLen)), nil
	}

	n, err := s.cfg.AgentService.NotesAppend(ctx, userID, noteID, text)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("notes_append", err)), nil
	}

	data, _ := json.Marshal(map[string]string{"note_id": n.ID, "title": n.Title})
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleNotesChangelog(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)

	sinceStr := req.GetString("since", "")
	untilStr := req.GetString("until", "")

	var since, until time.Time
	if sinceStr != "" {
		var parseErr error
		since, parseErr = time.Parse("2006-01-02", sinceStr)
		if parseErr != nil {
			return mcp.NewToolResultError("invalid since date, expected YYYY-MM-DD"), nil
		}
	} else {
		since = time.Now().AddDate(0, 0, -7)
	}
	if untilStr != "" {
		var parseErr error
		until, parseErr = time.Parse("2006-01-02", untilStr)
		if parseErr != nil {
			return mcp.NewToolResultError("invalid until date, expected YYYY-MM-DD"), nil
		}
		// Set to end of day.
		until = until.Add(24*time.Hour - time.Second)
	} else {
		until = time.Now()
	}

	limit := req.GetInt("limit", 20)
	if limit > maxSessionList {
		limit = maxSessionList
	}

	notes, total, err := s.cfg.AgentService.NotesChangelog(ctx, userID, since, until, limit)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("notes_changelog", err)), nil
	}

	type changelogEntry struct {
		ID        string   `json:"id"`
		Title     string   `json:"title"`
		Tags      []string `json:"tags,omitempty"`
		CreatedAt string   `json:"created_at"`
		UpdatedAt string   `json:"updated_at"`
	}
	entries := make([]changelogEntry, 0, len(notes))
	for _, n := range notes {
		entries = append(entries, changelogEntry{
			ID:        n.ID,
			Title:     n.Title,
			Tags:      n.Tags,
			CreatedAt: n.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt: n.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	data, jsonErr := json.Marshal(map[string]interface{}{
		"changes": entries,
		"total":   total,
		"since":   since.Format("2006-01-02"),
		"until":   until.Format("2006-01-02"),
	})
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal results"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleNotesVersions(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	noteID, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: id"), nil
	}

	// If a specific version is requested, return that snapshot.
	versionNum := req.GetInt("version", 0)
	if versionNum > 0 {
		v, err := s.cfg.AgentService.NotesGetVersion(ctx, userID, noteID, versionNum)
		if err != nil {
			return mcp.NewToolResultError(sanitizeError("notes_versions", err)), nil
		}
		data, jsonErr := json.Marshal(map[string]interface{}{
			"note_id":   v.NoteID,
			"version":   v.Version,
			"title":     v.Title,
			"body":      v.Body,
			"created_at": v.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
		if jsonErr != nil {
			return mcp.NewToolResultError("failed to marshal version"), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	}

	// List versions.
	limit := req.GetInt("limit", 10)
	if limit > maxSessionList {
		limit = maxSessionList
	}

	versions, total, err := s.cfg.AgentService.NotesVersions(ctx, userID, noteID, limit)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("notes_versions", err)), nil
	}

	type versionSummary struct {
		Version   int    `json:"version"`
		Title     string `json:"title"`
		CreatedAt string `json:"created_at"`
	}
	summaries := make([]versionSummary, 0, len(versions))
	for _, v := range versions {
		summaries = append(summaries, versionSummary{
			Version:   v.Version,
			Title:     v.Title,
			CreatedAt: v.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	data, jsonErr := json.Marshal(map[string]interface{}{
		"note_id":  noteID,
		"versions": summaries,
		"total":    total,
	})
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal versions"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// --- Graph Handler ---

func (s *Server) handleGraphNeighbors(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	noteID, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: id"), nil
	}

	includeTwoHop := req.GetString("include_two_hop", "true") != "false"

	// Get backlinks (notes that link to this note).
	backlinks, err := s.cfg.AgentService.NotesBacklinks(ctx, userID, noteID)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("graph_neighbors", err)), nil
	}

	type linkEntry struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}

	backlinkEntries := make([]linkEntry, 0, len(backlinks))
	for _, n := range backlinks {
		backlinkEntries = append(backlinkEntries, linkEntry{ID: n.ID, Title: n.Title})
	}

	result := map[string]interface{}{
		"note_id":   noteID,
		"backlinks": backlinkEntries,
	}

	// Two-hop connections.
	if includeTwoHop {
		twoHop, twoErr := s.cfg.GraphService.GetTwoHopBacklinks(ctx, userID, noteID)
		if twoErr == nil {
			result["two_hop"] = twoHop
		}
	}

	data, jsonErr := json.Marshal(result)
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal graph data"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// --- Review Queue Handler ---

func (s *Server) handleReviewQueue(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	limit := req.GetInt("limit", 10)
	if limit > maxSessionList {
		limit = maxSessionList
	}

	items, err := s.cfg.ReviewService.GetQueue(ctx, userID, limit)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("review_queue", err)), nil
	}

	data, jsonErr := json.Marshal(map[string]interface{}{
		"items": items,
		"count": len(items),
	})
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal review queue"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// --- Template Handler ---

func (s *Server) handleNotesFromTemplate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)

	// List mode: return available templates.
	if req.GetString("list", "") == "true" {
		templates, err := s.cfg.TemplateService.List(ctx, userID)
		if err != nil {
			return mcp.NewToolResultError(sanitizeError("notes_from_template", err)), nil
		}
		data, jsonErr := json.Marshal(map[string]interface{}{"templates": templates})
		if jsonErr != nil {
			return mcp.NewToolResultError("failed to marshal templates"), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	}

	// Create mode: require template name and title.
	templateName, err := req.RequireString("template")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: template (or set list='true' to see available templates)"), nil
	}
	title, err := req.RequireString("title")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: title"), nil
	}

	// Parse template variables from JSON string.
	var vars map[string]string
	varsStr := req.GetString("vars", "")
	if varsStr != "" {
		if jsonErr := json.Unmarshal([]byte(varsStr), &vars); jsonErr != nil {
			return mcp.NewToolResultError("invalid vars: expected JSON object with string values"), nil
		}
	}

	// Apply template.
	body, err := s.cfg.TemplateService.Apply(ctx, userID, templateName, vars)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("notes_from_template", err)), nil
	}

	// Create the note using AgentService (which adds created-by:agent tag).
	projectSlug := req.GetString("project", "")
	tagsStr := req.GetString("tags", "")
	var tags []string
	if tagsStr != "" {
		tags = parseTags(tagsStr)
	}

	n, err := s.cfg.AgentService.NotesCreate(ctx, userID, title, body, projectSlug, tags)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("notes_from_template", err)), nil
	}

	data, _ := json.Marshal(map[string]string{"note_id": n.ID, "title": n.Title, "template": templateName})
	return mcp.NewToolResultText(string(data)), nil
}

// --- Error Sanitization ---

// sanitizeError maps domain errors to user-safe messages. Internal error details
// (file paths, DB errors, etc.) are not exposed to the client.
func sanitizeError(tool string, err error) string {
	switch {
	case errors.Is(err, agent.ErrNotFound):
		return tool + ": not found"
	case errors.Is(err, note.ErrNotFound):
		return tool + ": not found"
	case errors.Is(err, project.ErrNotFound):
		return tool + ": project not found"
	case errors.Is(err, agent.ErrSessionNotActive):
		return tool + ": session is not active"
	case errors.Is(err, agent.ErrFindingsTooLong):
		return tool + ": findings exceed maximum length (1500 chars)"
	case errors.Is(err, agent.ErrFindingsRequired):
		return tool + ": findings are required"
	case errors.Is(err, agent.ErrInvalidSessionName):
		return tool + ": invalid session name"
	case errors.Is(err, note.ErrVersionNotFound):
		return tool + ": version not found"
	case errors.Is(err, project.ErrSlugExists):
		return tool + ": project slug already exists"
	case errors.Is(err, template.ErrTemplateNotFound):
		return tool + ": template not found"
	case errors.Is(err, template.ErrInvalidName):
		return tool + ": invalid template name"
	case errors.Is(err, validate.ErrUnsafeName):
		return tool + ": name contains unsafe characters (backslash, .., or null bytes)"
	case errors.Is(err, task.ErrNotFound):
		return tool + ": task not found"
	case errors.Is(err, webhook.ErrNotFound):
		return tool + ": not found"
	case errors.Is(err, webhook.ErrInvalidURL):
		return tool + ": invalid webhook URL"
	case errors.Is(err, webhook.ErrInvalidEventType):
		return tool + ": invalid event type"
	case errors.Is(err, webhook.ErrNameRequired):
		return tool + ": name is required"
	case errors.Is(err, webhook.ErrURLRequired):
		return tool + ": url is required"
	case errors.Is(err, webhook.ErrEventsRequired):
		return tool + ": event_types is required"
	case errors.Is(err, agent.ErrInvalidLabName):
		return tool + ": invalid lab name (alphanumeric and hyphens only)"
	case errors.Is(err, agent.ErrInvalidOutcome):
		return tool + ": invalid outcome (must be success/failure/partial/inconclusive)"
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

// clampRecencyBias clamps a recency bias value to the valid range [0.0, 1.0].
func clampRecencyBias(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// parseTags splits a comma-separated tag string into a slice, trimming whitespace.
func parseTags(s string) []string {
	var tags []string
	for _, t := range strings.Split(s, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
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

// --- Webhook Tool Definitions ---

func webhookRegisterTool() mcp.Tool {
	return mcp.NewTool("webhook_register",
		mcp.WithDescription("Register a webhook to receive HTTP callbacks when specific events occur."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Webhook name")),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL to receive webhook POST requests")),
		mcp.WithString("event_types", mcp.Required(), mcp.Description("Comma-separated event types (e.g. 'note.created,note.modified')")),
		mcp.WithString("project", mcp.Description("Optional project slug filter")),
		mcp.WithString("tag", mcp.Description("Optional tag filter")),
	)
}

func webhookListTool() mcp.Tool {
	return mcp.NewTool("webhook_list",
		mcp.WithDescription("List registered webhooks."),
		mcp.WithString("active_only", mcp.Enum("true", "false"), mcp.Description("Only list active webhooks (default: true)")),
	)
}

func webhookDeleteTool() mcp.Tool {
	return mcp.NewTool("webhook_delete",
		mcp.WithDescription("Delete a webhook by ID."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Webhook ID")),
	)
}

// --- Webhook Tool Handlers ---

func (s *Server) handleWebhookRegister(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: name"), nil
	}
	rawURL, err := req.RequireString("url")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: url"), nil
	}
	eventTypesStr, err := req.RequireString("event_types")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: event_types"), nil
	}

	// Parse comma-separated event types.
	var eventTypes []string
	for _, et := range strings.Split(eventTypesStr, ",") {
		et = strings.TrimSpace(et)
		if et != "" {
			eventTypes = append(eventTypes, et)
		}
	}

	projectSlug := req.GetString("project", "")
	tag := req.GetString("tag", "")

	createReq := webhook.CreateReq{
		Name:       name,
		URL:        rawURL,
		EventTypes: eventTypes,
		Filter: webhook.Filter{
			ProjectSlug: projectSlug,
			Tag:         tag,
		},
	}

	wh, err := s.cfg.WebhookService.Create(ctx, userID, createReq)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("webhook_register", err)), nil
	}

	// Return secret to caller but mark result for redaction in audit log.
	data, _ := json.Marshal(map[string]string{"id": wh.ID, "name": wh.Name, "secret": wh.Secret}) //nolint:errcheck
	// Wrap the response so the logging middleware can see the result text,
	// but redact the secret from the audit trail.
	resultText := string(data)
	return mcp.NewToolResultText(resultText), nil
}

func (s *Server) handleWebhookList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	activeOnly := req.GetString("active_only", "true") != "false"

	webhooks, err := s.cfg.WebhookService.List(ctx, userID, activeOnly)
	if err != nil {
		return mcp.NewToolResultError("webhook_list: internal error"), nil
	}

	data, jsonErr := json.Marshal(webhooks)
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal webhooks"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleWebhookDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)
	id, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: id"), nil
	}

	if err := s.cfg.WebhookService.Delete(ctx, userID, id); err != nil {
		if errors.Is(err, webhook.ErrNotFound) {
			return mcp.NewToolResultError("webhook_delete: not found"), nil
		}
		return mcp.NewToolResultError("webhook_delete: internal error"), nil
	}

	return mcp.NewToolResultText(`{"status":"deleted"}`), nil
}

// --- Task Tool Definitions ---

func tasksListTool() mcp.Tool {
	return mcp.NewTool("tasks_list",
		mcp.WithDescription("List tasks (checkbox items) from notes. Filter by done status, project, or tag."),
		mcp.WithString("done", mcp.Enum("true", "false"), mcp.Description("Filter by done status (optional)")),
		mcp.WithString("project", mcp.Description("Project slug to filter by (optional)")),
		mcp.WithString("tag", mcp.Description("Tag to filter by (optional)")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of results (default: 20)")),
	)
}

func tasksSummaryTool() mcp.Tool {
	return mcp.NewTool("tasks_summary",
		mcp.WithDescription("Get aggregate task counts (total, done, open). Optionally filter by project or tag."),
		mcp.WithString("project", mcp.Description("Project slug to filter by (optional)")),
		mcp.WithString("tag", mcp.Description("Tag to filter by (optional)")),
	)
}

// --- Task Tool Handlers ---

func (s *Server) handleTasksList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)

	filter := task.TaskFilter{}
	doneStr := req.GetString("done", "")
	if doneStr == "true" {
		d := true
		filter.Done = &d
	} else if doneStr == "false" {
		d := false
		filter.Done = &d
	}

	filter.ProjectSlug = req.GetString("project", "")
	filter.Tag = req.GetString("tag", "")

	limit := req.GetInt("limit", 20)
	if limit > maxSessionList {
		limit = maxSessionList
	}
	filter.Limit = limit

	tasks, total, err := s.cfg.TaskService.List(ctx, userID, filter)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("tasks_list", err)), nil
	}

	data, jsonErr := json.Marshal(map[string]interface{}{
		"tasks": tasks,
		"total": total,
	})
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal results"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleTasksSummary(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)

	filter := task.TaskFilter{}
	filter.ProjectSlug = req.GetString("project", "")
	filter.Tag = req.GetString("tag", "")

	summary, err := s.cfg.TaskService.Summary(ctx, userID, filter)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("tasks_summary", err)), nil
	}

	data, jsonErr := json.Marshal(summary)
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal results"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// --- V5: Research Lab Tool Definitions ---

func labOpenTool() mcp.Tool {
	return mcp.NewTool("lab_open",
		mcp.WithDescription("Open or resume a research lab for systematic debugging. Returns lab notebook, briefing, and past trial summaries. Multiple agents can open the same lab to collaborate."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Lab name (alphanumeric and hyphens, e.g. 'ble-sampling')")),
		mcp.WithString("problem", mcp.Required(), mcp.Description("Problem statement (what you are investigating)")),
		mcp.WithString("domain", mcp.Required(), mcp.Description("Domain (e.g. 'firmware', 'bluetooth', 'ml')")),
		mcp.WithString("tags", mcp.Description("Comma-separated extra tags (optional)")),
	)
}

func trialRecordTool() mcp.Tool {
	return mcp.NewTool("trial_record",
		mcp.WithDescription("Record a trial in a research lab. Call with changes+expected to start, then update later with actual+outcome. When outcome is set, the trial session ends and findings become visible to collaborating agents."),
		mcp.WithString("lab", mcp.Required(), mcp.Description("Lab name")),
		mcp.WithString("title", mcp.Required(), mcp.Description("Trial title (e.g. 'Increase connection interval to 30ms')")),
		mcp.WithString("changes", mcp.Required(), mcp.Description("What was changed (code, config, firmware)")),
		mcp.WithString("expected", mcp.Required(), mcp.Description("What you expect to happen")),
		mcp.WithString("actual", mcp.Description("What actually happened (optional, can be added later)")),
		mcp.WithString("outcome", mcp.Description("Trial outcome (optional)"), mcp.Enum("success", "failure", "partial", "inconclusive")),
		mcp.WithString("notes", mcp.Description("Additional observations (optional)")),
	)
}

func decisionRecordTool() mcp.Tool {
	return mcp.NewTool("decision_record",
		mcp.WithDescription("Record a decision based on accumulated evidence from lab trials."),
		mcp.WithString("lab", mcp.Required(), mcp.Description("Lab name")),
		mcp.WithString("title", mcp.Required(), mcp.Description("Decision title (e.g. 'Ship priority-based coex')")),
		mcp.WithString("rationale", mcp.Required(), mcp.Description("Why this decision was made")),
		mcp.WithString("based_on", mcp.Description("Comma-separated trial titles this decision is based on (optional)")),
		mcp.WithString("next_steps", mcp.Description("What to do next (optional)")),
	)
}

func trialQueryTool() mcp.Tool {
	return mcp.NewTool("trial_query",
		mcp.WithDescription("Search and list trials in a research lab, optionally filtered by outcome. Returns structured trial data with changes, expected, actual, and outcome."),
		mcp.WithString("lab", mcp.Required(), mcp.Description("Lab name")),
		mcp.WithString("query", mcp.Description("Text search within trials (optional)")),
		mcp.WithString("outcome", mcp.Description("Filter by outcome (optional)"), mcp.Enum("success", "failure", "partial", "inconclusive", "pending")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 20)")),
	)
}

// --- V5: Research Lab Handlers ---

func (s *Server) handleLabOpen(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)

	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: name"), nil
	}

	problem, err := req.RequireString("problem")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: problem"), nil
	}

	domain, err := req.RequireString("domain")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: domain"), nil
	}

	if len(problem) > maxContentLen {
		return mcp.NewToolResultError(fmt.Sprintf("problem too long: %d bytes exceeds limit of %d", len(problem), maxContentLen)), nil
	}

	var tags []string
	if tagsStr := req.GetString("tags", ""); tagsStr != "" {
		tags = parseTags(tagsStr)
	}

	info, err := s.cfg.AgentService.LabOpen(ctx, userID, name, problem, domain, tags)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("lab_open", err)), nil
	}

	data, jsonErr := json.Marshal(info)
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal results"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleTrialRecord(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)

	lab, err := req.RequireString("lab")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: lab"), nil
	}

	title, err := req.RequireString("title")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: title"), nil
	}

	changes, err := req.RequireString("changes")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: changes"), nil
	}

	expected, err := req.RequireString("expected")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: expected"), nil
	}

	actual := req.GetString("actual", "")
	outcome := req.GetString("outcome", "")
	notes := req.GetString("notes", "")

	if len(changes) > maxContentLen || len(expected) > maxContentLen || len(actual) > maxContentLen {
		return mcp.NewToolResultError("content too long"), nil
	}

	summary, err := s.cfg.AgentService.TrialRecord(ctx, userID, lab, title, changes, expected, actual, outcome, notes)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("trial_record", err)), nil
	}

	data, jsonErr := json.Marshal(summary)
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal results"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleDecisionRecord(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)

	lab, err := req.RequireString("lab")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: lab"), nil
	}

	title, err := req.RequireString("title")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: title"), nil
	}

	rationale, err := req.RequireString("rationale")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: rationale"), nil
	}

	basedOn := req.GetString("based_on", "")
	nextSteps := req.GetString("next_steps", "")

	if len(rationale) > maxContentLen {
		return mcp.NewToolResultError(fmt.Sprintf("rationale too long: %d bytes exceeds limit of %d", len(rationale), maxContentLen)), nil
	}

	info, err := s.cfg.AgentService.DecisionRecord(ctx, userID, lab, title, rationale, basedOn, nextSteps)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("decision_record", err)), nil
	}

	data, jsonErr := json.Marshal(info)
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal results"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleTrialQuery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := reqctx.UserIDFromContext(ctx)

	lab, err := req.RequireString("lab")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: lab"), nil
	}

	query := req.GetString("query", "")
	outcome := req.GetString("outcome", "")
	limit := req.GetInt("limit", 20)
	if limit > maxSessionList {
		limit = maxSessionList
	}

	trials, err := s.cfg.AgentService.TrialQuery(ctx, userID, lab, query, outcome, limit)
	if err != nil {
		return mcp.NewToolResultError(sanitizeError("trial_query", err)), nil
	}

	data, jsonErr := json.Marshal(map[string]any{
		"trials": trials,
		"total":  len(trials),
	})
	if jsonErr != nil {
		return mcp.NewToolResultError("failed to marshal results"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
