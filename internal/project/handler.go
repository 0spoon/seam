package project

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/validate"
)

// Handler handles HTTP requests for project endpoints.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

// NewHandler creates a new project Handler.
func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// Routes returns a chi router with all project routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.create)
	r.Get("/", h.list)
	r.Get("/{id}", h.get)
	r.Put("/{id}", h.update)
	r.Delete("/{id}", h.delete)
	return r
}

// createReq is the request body for creating a project.
type createReq struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// updateReq is the request body for updating a project.
type updateReq struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req createReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// A-6: Validate project name for filesystem safety (no /, \, .., null bytes).
	if err := validate.Name(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, "name contains unsafe characters")
		return
	}

	p, err := h.service.Create(r.Context(), userID, req.Name, req.Description)
	if err != nil {
		if errors.Is(err, ErrSlugExists) {
			writeError(w, http.StatusConflict, "a project with that name already exists")
			return
		}
		h.logger.Error("create project failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, p)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	projects, err := h.service.List(r.Context(), userID)
	if err != nil {
		h.logger.Error("list projects failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Return empty array instead of null.
	if projects == nil {
		projects = []*Project{}
	}

	writeJSON(w, http.StatusOK, projects)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	projectID := chi.URLParam(r, "id")
	p, err := h.service.Get(r.Context(), userID, projectID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		h.logger.Error("get project failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	projectID := chi.URLParam(r, "id")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req updateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == nil && req.Description == nil {
		writeError(w, http.StatusBadRequest, "no fields to update")
		return
	}

	// Reject empty name if explicitly provided.
	if req.Name != nil && *req.Name == "" {
		writeError(w, http.StatusBadRequest, "name must not be empty")
		return
	}

	// A-6: Validate project name for filesystem safety when updating.
	if req.Name != nil {
		if err := validate.Name(*req.Name); err != nil {
			writeError(w, http.StatusBadRequest, "name contains unsafe characters")
			return
		}
	}

	p, err := h.service.Update(r.Context(), userID, projectID, req.Name, req.Description)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		if errors.Is(err, ErrSlugExists) {
			writeError(w, http.StatusConflict, "a project with that name already exists")
			return
		}
		h.logger.Error("update project failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	projectID := chi.URLParam(r, "id")
	cascade := r.URL.Query().Get("cascade")
	if cascade == "" {
		cascade = "inbox"
	}
	if cascade != "inbox" && cascade != "delete" {
		writeError(w, http.StatusBadRequest, "cascade must be \"inbox\" or \"delete\"")
		return
	}

	err := h.service.Delete(r.Context(), userID, projectID, cascade)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		h.logger.Error("delete project failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// writeJSON encodes v as JSON and writes it to the response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("project.writeJSON: encode error", "error", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		slog.Warn("project.writeError: encode error", "error", err)
	}
}
