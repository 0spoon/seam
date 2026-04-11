package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/agent"
	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/task"
	"github.com/katata/seam/internal/userdb"
)

// HookBriefingService is the subset of agent.Service the hook handler needs.
// Defined as an interface so tests can stub the call without spinning up a
// real Service.
type HookBriefingService interface {
	HookBriefing(ctx context.Context, userID string, payload agent.HookPayload, maxChars, openTaskCount int) (string, error)
}

// HookTaskCounter is the subset of task.Service we use to count open tasks
// for the briefing header. Optional — when nil, the handler skips the count
// and HookBriefing will omit it from the briefing entirely.
type HookTaskCounter interface {
	Summary(ctx context.Context, userID string, filter task.TaskFilter) (*task.TaskSummary, error)
}

// hookHandlerTimeout caps the time the handler spends assembling a briefing.
// We MUST return quickly: Claude Code waits for the hook to respond before
// the agent starts, and we never want to stall a cold launch on a slow
// briefing query.
const hookHandlerTimeout = 2 * time.Second

// HooksHandler serves Claude Code hook events. The only event implemented
// in the MVP is SessionStart, which returns a briefing wrapped in the shape
// Claude Code expects so it can inject our text as additionalContext.
type HooksHandler struct {
	agentService HookBriefingService
	taskService  HookTaskCounter // optional
	apiKey       string
	logger       *slog.Logger
	maxChars     int
}

// NewHooksHandler builds a HooksHandler. apiKey is the static MCP bearer
// token (typically cfg.MCP.APIKey). maxChars is the soft target for the
// briefing (typically cfg.Hooks.MaxBriefingChars). When apiKey is empty,
// every request is rejected with 401 — better to fail closed than to
// expose a briefing endpoint to the network unauthenticated.
func NewHooksHandler(
	agentService HookBriefingService,
	taskService HookTaskCounter,
	apiKey string,
	logger *slog.Logger,
	maxChars int,
) *HooksHandler {
	if logger == nil {
		logger = slog.Default()
	}
	if maxChars <= 0 {
		maxChars = 500
	}
	return &HooksHandler{
		agentService: agentService,
		taskService:  taskService,
		apiKey:       apiKey,
		logger:       logger,
		maxChars:     maxChars,
	}
}

// Routes returns the chi sub-router mounted at /api/hooks.
func (h *HooksHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/session-start", h.sessionStart)
	return r
}

// hookSessionStartResponse is the wire shape Claude Code expects from a
// SessionStart hook. The field names are case-sensitive and undocumented in
// the public schema in places — wrong field names silently break with no
// error, so the test in hooks_handler_test.go locks them in.
type hookSessionStartResponse struct {
	Continue           bool                       `json:"continue"`
	SuppressOutput     bool                       `json:"suppressOutput"`
	HookSpecificOutput hookSessionStartSpecificOp `json:"hookSpecificOutput"`
}

type hookSessionStartSpecificOp struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// sessionStart handles POST /api/hooks/session-start. The handler MUST never
// return a non-2xx status for an internal error: Claude Code surfaces hook
// failures to the user, and a malfunctioning briefing must never block the
// agent from starting. Auth failures (401) are the only non-success path.
func (h *HooksHandler) sessionStart(w http.ResponseWriter, r *http.Request) {
	if !auth.VerifyMCPAPIKey(r, h.apiKey) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload agent.HookPayload
	if r.Body != nil {
		dec := json.NewDecoder(r.Body)
		// Don't reject on unknown fields — Claude Code may add new fields
		// (e.g. project_id) at any time and we want to keep working. An
		// empty body comes back as io.EOF and is also fine; we fall
		// through with the zero-value payload.
		if err := dec.Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
			h.logger.Debug("hooks.session_start: ignoring malformed payload", "error", err)
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), hookHandlerTimeout)
	defer cancel()

	briefing, err := h.assembleBriefing(ctx, payload)
	if err != nil {
		h.logger.Warn("hooks.session_start: briefing assembly failed",
			"error", err, "source", payload.Source)
		briefing = ""
	}

	resp := hookSessionStartResponse{
		Continue:       true,
		SuppressOutput: true,
		HookSpecificOutput: hookSessionStartSpecificOp{
			HookEventName:     "SessionStart",
			AdditionalContext: briefing,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(&resp); err != nil {
		h.logger.Warn("hooks.session_start: encode response failed", "error", err)
	}
}

func (h *HooksHandler) assembleBriefing(ctx context.Context, payload agent.HookPayload) (string, error) {
	if h.agentService == nil {
		return "", errors.New("hooks: agent service not configured")
	}

	openTaskCount := -1
	if h.taskService != nil {
		// Best-effort: a task service error must not block the briefing.
		openOnly := false
		summary, err := h.taskService.Summary(ctx, userdb.DefaultUserID, task.TaskFilter{
			Done: &openOnly,
		})
		if err == nil && summary != nil {
			openTaskCount = summary.Open
		} else if err != nil {
			h.logger.Debug("hooks.session_start: task summary failed", "error", err)
		}
	}

	return h.agentService.HookBriefing(ctx, userdb.DefaultUserID, payload, h.maxChars, openTaskCount)
}
