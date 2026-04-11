package note

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/validate"
)

// TemplateApplier applies a template by name, returning the rendered body.
// This interface is implemented by template.Service and injected here
// to support the single-request template-based note creation flow
// (POST /api/notes {"template":"meeting-notes"}).
type TemplateApplier interface {
	Apply(ctx context.Context, userID, name string, vars map[string]string) (string, error)
}

// Handler handles HTTP requests for note endpoints.
type Handler struct {
	service         *Service
	templateMu      sync.RWMutex    // protects templateApplier
	templateApplier TemplateApplier // nil if templates not configured
	logger          *slog.Logger
}

// NewHandler creates a new note Handler.
func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// SetTemplateApplier sets the template applier, enabling single-request
// template-based note creation. Called during server startup after both
// note and template services are initialized.
func (h *Handler) SetTemplateApplier(applier TemplateApplier) {
	h.templateMu.Lock()
	defer h.templateMu.Unlock()
	h.templateApplier = applier
}

// getTemplateApplier returns the current template applier, or nil.
func (h *Handler) getTemplateApplier() TemplateApplier {
	h.templateMu.RLock()
	defer h.templateMu.RUnlock()
	return h.templateApplier
}

// BulkActionReq is the request payload for bulk note operations.
type BulkActionReq struct {
	NoteIDs []string         `json:"note_ids"`
	Action  string           `json:"action"`
	Params  BulkActionParams `json:"params"`
}

// BulkActionParams holds optional parameters for bulk actions.
type BulkActionParams struct {
	Tag       string `json:"tag,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
}

// BulkActionResult reports the outcome of a bulk operation.
type BulkActionResult struct {
	Success int      `json:"success"`
	Failed  int      `json:"failed"`
	Errors  []string `json:"errors,omitempty"`
}

// Routes returns a chi router with all note routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.create)
	r.Get("/", h.list)
	// /daily and /bulk must be registered before /{id} to avoid chi treating them as id params.
	r.Get("/daily", h.getDailyNote)
	r.Patch("/bulk", h.bulkAction)
	r.Get("/{id}", h.get)
	r.Put("/{id}", h.update)
	r.Delete("/{id}", h.delete)
	r.Get("/resolve", h.resolveWikilink)
	r.Post("/{id}/append", h.appendToNote)
	r.Get("/{id}/backlinks", h.backlinks)
	r.Route("/{id}/versions", func(r chi.Router) {
		r.Get("/", h.listVersions)
		r.Get("/{version}", h.getVersion)
		r.Post("/{version}/restore", h.restoreVersion)
	})
	return r
}

func (h *Handler) getDailyNote(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	dateStr := r.URL.Query().Get("date")
	if dateStr == "" || dateStr == "today" {
		dateStr = time.Now().Format("2006-01-02")
	}

	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid date format, expected YYYY-MM-DD")
		return
	}

	note, err := h.service.GetOrCreateDaily(r.Context(), userID, date)
	if err != nil {
		h.logger.Error("get daily note failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, note)
}

func (h *Handler) appendToNote(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	noteID := chi.URLParam(r, "id")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}

	note, err := h.service.AppendToNote(r.Context(), userID, noteID, req.Text)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		h.logger.Error("append to note failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, note)
}

func (h *Handler) bulkAction(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req BulkActionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.NoteIDs) == 0 {
		writeError(w, http.StatusBadRequest, "note_ids is required")
		return
	}
	if len(req.NoteIDs) > 100 {
		writeError(w, http.StatusBadRequest, "maximum 100 notes per bulk operation")
		return
	}

	switch req.Action {
	case "add_tag", "remove_tag":
		if req.Params.Tag == "" {
			writeError(w, http.StatusBadRequest, "params.tag is required for tag actions")
			return
		}
		if err := validate.Name(req.Params.Tag); err != nil {
			writeError(w, http.StatusBadRequest, "tag name contains unsafe characters")
			return
		}
	case "move":
		// project_id can be empty (= inbox)
	case "delete":
		// no params needed
	default:
		writeError(w, http.StatusBadRequest, "unsupported action")
		return
	}

	result, err := h.service.BulkAction(r.Context(), userID, req)
	if err != nil {
		h.logger.Error("bulk action failed", "action", req.Action, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req CreateNoteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	// A-5: Validate title for filesystem safety (no \, .., null bytes).
	if err := validate.Title(req.Title); err != nil {
		writeError(w, http.StatusBadRequest, "title contains unsafe characters")
		return
	}

	// If a template is specified, apply it to pre-fill the body.
	if req.Template != "" {
		applier := h.getTemplateApplier()
		if applier == nil {
			writeError(w, http.StatusBadRequest, "templates not configured")
			return
		}
		vars := map[string]string{}
		if req.Title != "" {
			vars["title"] = req.Title
		}
		body, tmplErr := applier.Apply(r.Context(), userID, req.Template, vars)
		if tmplErr != nil {
			h.logger.Warn("template apply failed, continuing without template",
				"template", req.Template, "error", tmplErr)
		} else if req.Body == "" {
			// Only apply template body if caller did not provide explicit body.
			req.Body = body
		}
	}

	n, err := h.service.Create(r.Context(), userID, req)
	if err != nil {
		h.logger.Error("create note failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, n)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	filter := NoteFilter{}

	// Parse query params.
	projectParam := r.URL.Query().Get("project")
	if projectParam == "inbox" {
		filter.InboxOnly = true
	} else if projectParam != "" {
		filter.ProjectID = projectParam
	}

	filter.Tag = r.URL.Query().Get("tag")
	filter.Sort = r.URL.Query().Get("sort")

	// Accept both "dir" and "sort_dir" for sort direction (frontend sends "sort_dir").
	sortDir := r.URL.Query().Get("sort_dir")
	if sortDir == "" {
		sortDir = r.URL.Query().Get("dir")
	}
	filter.SortDir = sortDir

	if since := r.URL.Query().Get("since"); since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid 'since' format, expected RFC3339")
			return
		}
		filter.Since = t
	}
	if until := r.URL.Query().Get("until"); until != "" {
		t, err := time.Parse(time.RFC3339, until)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid 'until' format, expected RFC3339")
			return
		}
		filter.Until = t
	}

	// Exclude body from list responses for performance -- callers that
	// need the body should use the single-note GET endpoint.
	filter.ExcludeBody = true

	// Default limit is 100, max is 500.
	filter.Limit = 100
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

	notes, total, err := h.service.List(r.Context(), userID, filter)
	if err != nil {
		h.logger.Error("list notes failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if notes == nil {
		notes = []*Note{}
	}

	w.Header().Set("X-Total-Count", fmt.Sprintf("%d", total))
	writeJSON(w, http.StatusOK, notes)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	noteID := chi.URLParam(r, "id")
	n, err := h.service.Get(r.Context(), userID, noteID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		h.logger.Error("get note failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, n)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	noteID := chi.URLParam(r, "id")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req UpdateNoteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Title == nil && req.Body == nil && req.ProjectID == nil && req.Tags == nil {
		writeError(w, http.StatusBadRequest, "no fields to update")
		return
	}

	if req.Title != nil && *req.Title == "" {
		writeError(w, http.StatusBadRequest, "title must not be empty")
		return
	}

	// A-5: Validate title for filesystem safety when updating.
	if req.Title != nil {
		if err := validate.Title(*req.Title); err != nil {
			writeError(w, http.StatusBadRequest, "title contains unsafe characters")
			return
		}
	}

	n, err := h.service.Update(r.Context(), userID, noteID, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		h.logger.Error("update note failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, n)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	noteID := chi.URLParam(r, "id")
	err := h.service.Delete(r.Context(), userID, noteID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		h.logger.Error("delete note failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) backlinks(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	noteID := chi.URLParam(r, "id")

	notes, err := h.service.GetBacklinks(r.Context(), userID, noteID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		h.logger.Error("get backlinks failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if notes == nil {
		notes = []*Note{}
	}

	writeJSON(w, http.StatusOK, notes)
}

func (h *Handler) resolveWikilink(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	title := r.URL.Query().Get("title")
	if title == "" {
		writeError(w, http.StatusBadRequest, "title parameter is required")
		return
	}

	noteID, err := h.service.ResolveWikilink(r.Context(), userID, title)
	if err != nil {
		// Dangling link -- no matching note.
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"dangling": true,
			"title":    title,
		})
		return
	}

	// Found a match -- fetch the note details.
	note, err := h.service.Get(r.Context(), userID, noteID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"dangling": true,
			"title":    title,
		})
		return
	}

	// Create a snippet (first ~200 runes of body, rune-safe truncation).
	snippet := note.Body
	runes := []rune(snippet)
	if len(runes) > 200 {
		snippet = string(runes[:200]) + "..."
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"dangling": false,
		"note_id":  note.ID,
		"title":    note.Title,
		"snippet":  snippet,
		"tags":     note.Tags,
	})
}

// TagsRoutes returns a chi router for the /api/tags endpoint.
func (h *Handler) TagsRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.listTags)
	return r
}

func (h *Handler) listTags(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	tags, err := h.service.ListTags(r.Context(), userID)
	if err != nil {
		h.logger.Error("list tags failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if tags == nil {
		tags = []TagCount{}
	}

	writeJSON(w, http.StatusOK, tags)
}

// writeJSON encodes v as JSON and writes it to the response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("note.writeJSON: encode error", "error", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		slog.Warn("note.writeError: encode error", "error", err)
	}
}
