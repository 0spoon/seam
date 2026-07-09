package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
)

// SessionDetail is a session plus the IDs of its plan/progress/context notes.
type SessionDetail struct {
	Session        *Session `json:"session"`
	PlanNoteID     string   `json:"plan_note_id,omitempty"`
	ProgressNoteID string   `json:"progress_note_id,omitempty"`
	ContextNoteID  string   `json:"context_note_id,omitempty"`
}

// APIService is the read-only agent surface the HTTP handler needs. Satisfied
// by *Service; declared as an interface for testability.
type APIService interface {
	ListSessionsForAPI(ctx context.Context, userID, status, project string, limit int) ([]*Session, error)
	SessionDetail(ctx context.Context, userID, name string) (*SessionDetail, error)
	ListMemoriesForAPI(ctx context.Context, userID, project, category string) ([]MemoryItem, error)
}

// Handler serves the read-only agent-visibility HTTP API (mounted at
// /api/agent). Mutations happen through the note editor (memories are notes)
// and MCP tools, so this handler only reads.
type Handler struct {
	service APIService
	logger  *slog.Logger
}

// NewHandler creates a new agent HTTP Handler.
func NewHandler(service APIService, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// Routes returns a chi router mounted at /api/agent.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/sessions", h.listSessions)
	r.Get("/sessions/{name}", h.getSession)
	r.Get("/memories", h.listMemories)
	return r
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSONError(w, http.StatusUnauthorized, "missing user identity")
		return
	}
	status := r.URL.Query().Get("status")
	if status == "all" {
		status = ""
	}
	project := r.URL.Query().Get("project")
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := parsePositiveInt(v); err == nil {
			limit = n
		}
	}

	sessions, err := h.service.ListSessionsForAPI(r.Context(), userID, status, project, limit)
	if err != nil {
		h.logger.Error("agent.Handler.listSessions", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if sessions == nil {
		sessions = []*Session{}
	}
	writeJSONResp(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSONError(w, http.StatusUnauthorized, "missing user identity")
		return
	}
	name := chi.URLParam(r, "name")
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "session name is required")
		return
	}

	detail, err := h.service.SessionDetail(r.Context(), userID, name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		h.logger.Error("agent.Handler.getSession", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSONResp(w, http.StatusOK, detail)
}

func (h *Handler) listMemories(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSONError(w, http.StatusUnauthorized, "missing user identity")
		return
	}
	project := r.URL.Query().Get("project")
	category := r.URL.Query().Get("category")

	items, err := h.service.ListMemoriesForAPI(r.Context(), userID, project, category)
	if err != nil {
		h.logger.Error("agent.Handler.listMemories", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if items == nil {
		items = []MemoryItem{}
	}
	writeJSONResp(w, http.StatusOK, map[string]any{"memories": items})
}

// --- Service methods backing the read-only API ---

// ListSessionsForAPI lists sessions filtered by status and (optionally) project.
func (s *Service) ListSessionsForAPI(ctx context.Context, userID, status, project string, limit int) ([]*Session, error) {
	if limit <= 0 {
		limit = 50
	}
	db, err := s.cfg.UserDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("agent.Service.ListSessionsForAPI: open db: %w", err)
	}
	if project != "" {
		return s.cfg.Store.ListSessionsByProject(ctx, db, status, project, limit)
	}
	return s.cfg.Store.ListSessions(ctx, db, status, limit, 0)
}

// SessionDetail returns a session plus its plan/progress/context note IDs.
func (s *Service) SessionDetail(ctx context.Context, userID, name string) (*SessionDetail, error) {
	db, err := s.cfg.UserDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("agent.Service.SessionDetail: open db: %w", err)
	}
	sess, err := s.cfg.Store.GetSessionByName(ctx, db, name)
	if err != nil {
		return nil, err // ErrNotFound propagates
	}
	d := &SessionDetail{Session: sess}
	if id, err := s.findSessionNote(ctx, userID, name, "plan"); err == nil {
		d.PlanNoteID = id
	}
	if id, err := s.findSessionNote(ctx, userID, name, "progress"); err == nil {
		d.ProgressNoteID = id
	}
	if id, err := s.findSessionNote(ctx, userID, name, "context"); err == nil {
		d.ContextNoteID = id
	}
	return d, nil
}

// ListMemoriesForAPI lists memories, optionally filtered by project and category.
func (s *Service) ListMemoriesForAPI(ctx context.Context, userID, project, category string) ([]MemoryItem, error) {
	items, err := s.MemoryList(ctx, userID, category)
	if err != nil {
		return nil, err
	}
	if project == "" {
		return items, nil
	}
	filtered := make([]MemoryItem, 0, len(items))
	for _, it := range items {
		if it.Project == project {
			filtered = append(filtered, it)
		}
	}
	return filtered, nil
}

func parsePositiveInt(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int(r-'0')
		if n > 1000 {
			return 1000, nil
		}
	}
	if n == 0 {
		return 0, errors.New("zero")
	}
	return n, nil
}

func writeJSONResp(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("agent.writeJSONResp: encode error", "error", err)
	}
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		slog.Warn("agent.writeJSONError: encode error", "error", err)
	}
}
