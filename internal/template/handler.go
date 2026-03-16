package template

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
)

// Handler handles HTTP requests for template endpoints.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

// NewHandler creates a new template Handler.
func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// Routes returns a chi router with template routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Get("/{name}", h.get)
	r.Post("/{name}/apply", h.apply)
	return r
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	templates, err := h.service.List(r.Context(), userID)
	if err != nil {
		h.logger.Error("template.Handler.list: failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, templates)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	name := chi.URLParam(r, "name")
	tmpl, err := h.service.Get(r.Context(), userID, name)
	if err != nil {
		if errors.Is(err, ErrTemplateNotFound) || errors.Is(err, ErrInvalidName) {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		h.logger.Error("template.Handler.get: failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, tmpl)
}

// applyRequest is the request body for POST /api/templates/{name}/apply.
type applyRequest struct {
	Vars map[string]string `json:"vars"`
}

func (h *Handler) apply(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	name := chi.URLParam(r, "name")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req applyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// If the body is empty/EOF, use empty vars. For actual
		// parse errors (malformed JSON), return 400.
		if err.Error() != "EOF" {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		req.Vars = map[string]string{}
	}

	body, err := h.service.Apply(r.Context(), userID, name, req.Vars)
	if err != nil {
		if errors.Is(err, ErrTemplateNotFound) || errors.Is(err, ErrInvalidName) {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		h.logger.Error("template.Handler.apply: failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"body": body})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("template.writeJSON: encode error", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
