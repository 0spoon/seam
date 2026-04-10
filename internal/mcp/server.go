// Package mcp implements the MCP (Model Context Protocol) server for Seam,
// exposing agent memory tools via Streamable HTTP transport.
package mcp

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"golang.org/x/time/rate"

	"crypto/subtle"

	"github.com/katata/seam/internal/agent"
	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/graph"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/review"
	"github.com/katata/seam/internal/search"
	"github.com/katata/seam/internal/task"
	"github.com/katata/seam/internal/template"
	"github.com/katata/seam/internal/userdb"
	"github.com/katata/seam/internal/webhook"
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

	ContextGather(ctx context.Context, userID, query, scope string, maxChars int, recencyBias float64) ([]agent.KnowledgeHit, error)

	// User note access tools.
	NotesSearch(ctx context.Context, userID, query string, limit int, recencyBias float64) ([]search.FTSResult, error)
	NotesRead(ctx context.Context, userID, noteID string) (*note.Note, error)
	NotesList(ctx context.Context, userID, projectSlug, tag string, limit int) ([]*note.Note, int, error)
	NotesCreate(ctx context.Context, userID, title, body, projectSlug string, tags []string) (*note.Note, error)

	// V2: Memory search and session metrics.
	MemorySearch(ctx context.Context, userID, query string, limit int) ([]agent.KnowledgeHit, error)
	SessionMetrics(ctx context.Context, userID, sessionName string) (*agent.SessionMetrics, error)

	// V3: Note update/delete, tags, daily, project management.
	NotesUpdate(ctx context.Context, userID, noteID string, title, body, projectSlug *string, tags *[]string) (*note.Note, error)
	NotesDelete(ctx context.Context, userID, noteID string) error
	NotesTags(ctx context.Context, userID string) ([]note.TagCount, error)
	NotesDaily(ctx context.Context, userID string, date time.Time) (*note.Note, error)
	ProjectList(ctx context.Context, userID string) ([]*project.Project, error)
	ProjectCreate(ctx context.Context, userID, name, description string) (*project.Project, error)

	// V4: Append, changelog, versions, backlinks.
	NotesAppend(ctx context.Context, userID, noteID, text string) (*note.Note, error)
	NotesChangelog(ctx context.Context, userID string, since, until time.Time, limit int) ([]*note.Note, int, error)
	NotesVersions(ctx context.Context, userID, noteID string, limit int) ([]*note.NoteVersion, int, error)
	NotesGetVersion(ctx context.Context, userID, noteID string, version int) (*note.NoteVersion, error)
	NotesBacklinks(ctx context.Context, userID, noteID string) ([]*note.Note, error)

	// V5: Research lab / experiment tracking.
	LabOpen(ctx context.Context, userID, name, problem, domain string, tags []string) (*agent.LabInfo, error)
	TrialRecord(ctx context.Context, userID, lab, title, changes, expected, actual, outcome, notes string) (*agent.TrialSummary, error)
	DecisionRecord(ctx context.Context, userID, lab, title, rationale, basedOn, nextSteps string) (*agent.DecisionInfo, error)
	TrialQuery(ctx context.Context, userID, lab, query, outcome string, limit int) ([]agent.TrialSummary, error)
}

// Default rate limit: 60 requests per minute per user with burst of 20.
const (
	defaultMCPRateLimit = rate.Limit(1) // 60 per minute = 1 per second
	defaultMCPRateBurst = 20            // allow short bursts
)

// TaskService defines the interface for the task service used by MCP tools.
type TaskService interface {
	List(ctx context.Context, userID string, filter task.TaskFilter) ([]*task.Task, int, error)
	Summary(ctx context.Context, userID string, filter task.TaskFilter) (*task.TaskSummary, error)
	ToggleDone(ctx context.Context, userID, taskID string, done bool) error
}

// WebhookService defines the interface for the webhook service used by MCP tools.
type WebhookService interface {
	Create(ctx context.Context, userID string, req webhook.CreateReq) (*webhook.Webhook, error)
	List(ctx context.Context, userID string, activeOnly bool) ([]*webhook.Webhook, error)
	Delete(ctx context.Context, userID, id string) error
}

// GraphService defines the interface for knowledge graph tools.
type GraphService interface {
	GetGraph(ctx context.Context, userID string, filter graph.GraphFilter) (*graph.Graph, error)
	GetTwoHopBacklinks(ctx context.Context, userID, noteID string) ([]graph.TwoHopNode, error)
	GetOrphanNotes(ctx context.Context, userID string) ([]graph.Node, error)
}

// ReviewService defines the interface for knowledge gardening tools.
type ReviewService interface {
	GetQueue(ctx context.Context, userID string, limit int) ([]review.ReviewItem, error)
}

// TemplateService defines the interface for note template tools.
type TemplateService interface {
	List(ctx context.Context, userID string) ([]template.TemplateMeta, error)
	Apply(ctx context.Context, userID, name string, vars map[string]string) (string, error)
}

// Config holds dependencies for the MCP server.
type Config struct {
	AgentService    AgentService
	TaskService     TaskService     // optional: task tracking tools
	WebhookService  WebhookService  // optional: webhook management tools
	GraphService    GraphService    // optional: knowledge graph tools
	ReviewService   ReviewService   // optional: knowledge gardening tools
	TemplateService TemplateService // optional: note template tools
	ToolCallLogger  ToolCallLogger  // optional: persists tool call audits to DB
	Logger          *slog.Logger
}

// Server wraps an MCP server with agent tools.
type Server struct {
	mcp    *mcpserver.MCPServer
	cfg    Config
	logger *slog.Logger

	// Per-user rate limiters for MCP tool calls.
	limiterMu sync.Mutex
	limiters  map[string]*mcpUserLimiter

	// done is closed to signal the eviction goroutine to stop.
	done      chan struct{}
	closeOnce sync.Once
}

// mcpUserLimiter wraps a rate.Limiter with a last-seen timestamp for eviction.
type mcpUserLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// New creates the MCP server with all agent tools registered.
func New(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	s := &Server{
		cfg:      cfg,
		logger:   cfg.Logger,
		limiters: make(map[string]*mcpUserLimiter),
		done:     make(chan struct{}),
	}

	mcpSrv := mcpserver.NewMCPServer(
		"Seam",
		"1.0.0",
		mcpserver.WithToolCapabilities(false),
		mcpserver.WithRecovery(),
		mcpserver.WithToolHandlerMiddleware(authCheckMiddleware),
		mcpserver.WithToolHandlerMiddleware(s.rateLimitMiddleware()),
		mcpserver.WithToolHandlerMiddleware(s.loggingMiddleware()),
	)

	s.mcp = mcpSrv
	s.registerTools()

	// Start limiter eviction goroutine.
	go s.evictStaleLimiters()

	return s
}

// Handler returns an http.Handler for mounting at /api/mcp on the chi router.
// Auth is handled via WithHTTPContextFunc which extracts the JWT from the
// Authorization header and injects user ID into the context.
//
// When apiKey is non-empty, the handler also accepts a static bearer token
// as an alternative to JWT. This is useful for AI coding agents that need
// long-lived access without token refresh.
func (s *Server) Handler(jwtMgr *auth.JWTManager, apiKey string) http.Handler {
	return mcpserver.NewStreamableHTTPServer(s.mcp,
		mcpserver.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			authHeader := r.Header.Get("Authorization")
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				return ctx
			}
			token := parts[1]

			// Check static API key first (constant-time comparison).
			if apiKey != "" && subtle.ConstantTimeCompare([]byte(token), []byte(apiKey)) == 1 {
				ctx = reqctx.WithUserID(ctx, userdb.DefaultUserID)
				ctx = reqctx.WithUsername(ctx, "mcp-api-key")
				return ctx
			}

			claims, err := jwtMgr.VerifyAccessToken(token)
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

// Close stops background goroutines. Safe to call multiple times.
func (s *Server) Close() {
	s.closeOnce.Do(func() { close(s.done) })
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

// rateLimitMiddleware returns a tool handler middleware that enforces per-user rate limits.
func (s *Server) rateLimitMiddleware() mcpserver.ToolHandlerMiddleware {
	return func(next mcpserver.ToolHandlerFunc) mcpserver.ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			userID := reqctx.UserIDFromContext(ctx)
			if userID == "" {
				return next(ctx, req)
			}
			lim := s.getLimiter(userID)
			if !lim.Allow() {
				s.logger.Warn("mcp rate limit exceeded",
					"user_id", userID,
					"tool", req.Params.Name,
				)
				return mcp.NewToolResultError("rate limit exceeded, try again later"), nil
			}
			return next(ctx, req)
		}
	}
}

// getLimiter returns the rate limiter for a user, creating one if needed.
func (s *Server) getLimiter(userID string) *rate.Limiter {
	s.limiterMu.Lock()
	defer s.limiterMu.Unlock()

	entry, ok := s.limiters[userID]
	if ok {
		entry.lastSeen = time.Now()
		return entry.limiter
	}

	lim := rate.NewLimiter(defaultMCPRateLimit, defaultMCPRateBurst)
	s.limiters[userID] = &mcpUserLimiter{limiter: lim, lastSeen: time.Now()}
	return lim
}

// evictStaleLimiters periodically removes limiters for users who have not
// made requests in the last 10 minutes.
func (s *Server) evictStaleLimiters() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-10 * time.Minute)
			s.limiterMu.Lock()
			for uid, entry := range s.limiters {
				if entry.lastSeen.Before(cutoff) {
					delete(s.limiters, uid)
				}
			}
			s.limiterMu.Unlock()
		}
	}
}
