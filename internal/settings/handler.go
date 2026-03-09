package settings

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
)

// Handler handles HTTP requests for settings endpoints.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

// NewHandler creates a new settings Handler.
func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// Routes returns a chi router with all settings routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.getAll)
	r.Put("/", h.update)
	r.Delete("/{key}", h.delete)
	return r
}

func (h *Handler) getAll(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	settings, err := h.service.GetAll(r.Context(), userID)
	if err != nil {
		h.logger.Error("get settings failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Return empty object instead of null.
	if settings == nil {
		settings = make(map[string]string)
	}

	writeJSON(w, http.StatusOK, settings)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var settings map[string]string
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(settings) == 0 {
		writeError(w, http.StatusBadRequest, "no settings provided")
		return
	}

	err := h.service.Update(r.Context(), userID, settings)
	if err != nil {
		if errors.Is(err, ErrInvalidKey) {
			writeError(w, http.StatusBadRequest, "unknown setting key")
			return
		}
		if errors.Is(err, ErrInvalidValue) {
			writeError(w, http.StatusBadRequest, "invalid setting value")
			return
		}
		h.logger.Error("update settings failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	key := chi.URLParam(r, "key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	err := h.service.Delete(r.Context(), userID, key)
	if err != nil {
		if errors.Is(err, ErrInvalidKey) {
			writeError(w, http.StatusBadRequest, "unknown setting key")
			return
		}
		h.logger.Error("delete setting failed", "error", err)
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
		slog.Warn("settings.writeJSON: encode error", "error", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
