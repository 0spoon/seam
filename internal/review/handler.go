package review

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
)

// Handler handles HTTP requests for the review queue.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

// NewHandler creates a new review Handler.
func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// Routes returns a chi router with review routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/queue", h.getQueue)
	return r
}

// getQueue handles GET /api/review/queue
func (h *Handler) getQueue(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	items, err := h.service.GetQueue(r.Context(), userID, limit)
	if err != nil {
		h.logger.Error("review.Handler.getQueue: failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load review queue")
		return
	}

	writeJSON(w, http.StatusOK, items)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("review.writeJSON: encode error", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
