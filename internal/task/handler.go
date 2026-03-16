package task

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
)

// Handler handles HTTP requests for task endpoints.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

// NewHandler creates a new task Handler.
func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// Routes returns a chi router with all task routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)           // GET /api/tasks?done=false&project_id=X&tag=Y&limit=50
	r.Get("/summary", h.summary) // GET /api/tasks/summary?project_id=X
	r.Get("/{id}", h.get)        // GET /api/tasks/{id}
	r.Patch("/{id}", h.toggle)   // PATCH /api/tasks/{id} {"done": true}
	return r
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	filter := TaskFilter{}
	filter.NoteID = r.URL.Query().Get("note_id")
	filter.ProjectID = r.URL.Query().Get("project_id")
	filter.Tag = r.URL.Query().Get("tag")

	if doneParam := r.URL.Query().Get("done"); doneParam != "" {
		switch doneParam {
		case "true":
			d := true
			filter.Done = &d
		case "false":
			d := false
			filter.Done = &d
		default:
			writeError(w, http.StatusBadRequest, "done must be 'true' or 'false'")
			return
		}
	}

	filter.Limit = 50
	if limit := r.URL.Query().Get("limit"); limit != "" {
		if v, err := strconv.Atoi(limit); err == nil && v > 0 {
			filter.Limit = v
		}
	}
	if filter.Limit > 500 {
		filter.Limit = 500
	}

	if offset := r.URL.Query().Get("offset"); offset != "" {
		if v, err := strconv.Atoi(offset); err == nil && v >= 0 {
			filter.Offset = v
		}
	}

	tasks, total, err := h.service.List(r.Context(), userID, filter)
	if err != nil {
		h.logger.Error("list tasks failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if tasks == nil {
		tasks = []*Task{}
	}

	w.Header().Set("X-Total-Count", fmt.Sprintf("%d", total))
	writeJSON(w, http.StatusOK, tasks)
}

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	filter := TaskFilter{}
	filter.NoteID = r.URL.Query().Get("note_id")
	filter.ProjectID = r.URL.Query().Get("project_id")
	filter.Tag = r.URL.Query().Get("tag")

	summary, err := h.service.Summary(r.Context(), userID, filter)
	if err != nil {
		h.logger.Error("task summary failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	taskID := chi.URLParam(r, "id")
	t, err := h.service.Get(r.Context(), userID, taskID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		h.logger.Error("get task failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, t)
}

func (h *Handler) toggle(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	taskID := chi.URLParam(r, "id")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Done bool `json:"done"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.ToggleDone(r.Context(), userID, taskID, req.Done); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		h.logger.Error("toggle task failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// writeJSON encodes v as JSON and writes it to the response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("task.writeJSON: encode error", "error", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}
