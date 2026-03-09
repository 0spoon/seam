package note

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
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
	h.templateApplier = applier
}

// Routes returns a chi router with all note routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.create)
	r.Get("/", h.list)
	r.Get("/{id}", h.get)
	r.Put("/{id}", h.update)
	r.Delete("/{id}", h.delete)
	r.Get("/{id}/backlinks", h.backlinks)
	return r
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

	// A-5: Validate title for filesystem safety (no /, \, .., null bytes).
	if err := validate.Name(req.Title); err != nil {
		writeError(w, http.StatusBadRequest, "title contains unsafe characters")
		return
	}

	// If a template is specified, apply it to pre-fill the body.
	if req.Template != "" && h.templateApplier != nil {
		vars := map[string]string{}
		if req.Title != "" {
			vars["title"] = req.Title
		}
		body, tmplErr := h.templateApplier.Apply(r.Context(), userID, req.Template, vars)
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
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			filter.Since = t
		}
	}
	if until := r.URL.Query().Get("until"); until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			filter.Until = t
		}
	}

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
		if err := validate.Name(*req.Title); err != nil {
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

	db, err := h.service.userDBManager.Open(r.Context(), userID)
	if err != nil {
		h.logger.Error("backlinks: open db failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Verify note exists.
	if _, err := h.service.store.Get(r.Context(), db, noteID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		h.logger.Error("backlinks: get note failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	notes, err := h.service.store.GetBacklinks(r.Context(), db, noteID)
	if err != nil {
		h.logger.Error("get backlinks failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if notes == nil {
		notes = []*Note{}
	}

	writeJSON(w, http.StatusOK, notes)
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

	db, err := h.service.userDBManager.Open(r.Context(), userID)
	if err != nil {
		h.logger.Error("open user db failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	tags, err := h.service.store.ListTags(r.Context(), db)
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
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
