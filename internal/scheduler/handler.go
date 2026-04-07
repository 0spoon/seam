package scheduler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
)

// Handler exposes scheduler CRUD endpoints under /api/schedules.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

// NewHandler constructs a Handler.
func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// Routes returns a chi router for the scheduler endpoints.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.create)
	r.Get("/", h.list)
	r.Get("/{id}", h.get)
	r.Put("/{id}", h.update)
	r.Delete("/{id}", h.delete)
	r.Post("/{id}/run", h.runNow)
	return r
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req CreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sch, err := h.service.Create(r.Context(), userID, req)
	if err != nil {
		h.writeServiceError(w, r, "create schedule", err)
		return
	}
	writeJSON(w, http.StatusCreated, sch)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}
	enabledOnly := r.URL.Query().Get("enabled") == "true"
	out, err := h.service.List(r.Context(), userID, enabledOnly)
	if err != nil {
		h.logger.Error("scheduler.Handler.list",
			"error", err, "request_id", reqctx.RequestIDFromContext(r.Context()))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if out == nil {
		out = []*Schedule{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	sch, err := h.service.Get(r.Context(), userID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "schedule not found")
			return
		}
		h.logger.Error("scheduler.Handler.get",
			"error", err, "request_id", reqctx.RequestIDFromContext(r.Context()))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, sch)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req UpdateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sch, err := h.service.Update(r.Context(), userID, id, req)
	if err != nil {
		h.writeServiceError(w, r, "update schedule", err)
		return
	}
	writeJSON(w, http.StatusOK, sch)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := h.service.Delete(r.Context(), userID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "schedule not found")
			return
		}
		h.logger.Error("scheduler.Handler.delete",
			"error", err, "request_id", reqctx.RequestIDFromContext(r.Context()))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// runNow forces the scheduler to evaluate due jobs immediately. Useful for
// testing a freshly created schedule without waiting up to tickInterval.
func (h *Handler) runNow(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := h.service.RunSchedule(r.Context(), userID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "schedule not found")
			return
		}
		h.logger.Error("scheduler.Handler.runNow",
			"error", err, "request_id", reqctx.RequestIDFromContext(r.Context()))
		writeError(w, http.StatusInternalServerError, "failed to run schedule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, op string, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "schedule not found")
	case errors.Is(err, ErrNameRequired):
		writeError(w, http.StatusBadRequest, "name is required")
	case errors.Is(err, ErrInvalidActionType):
		writeError(w, http.StatusBadRequest, "invalid action type")
	case errors.Is(err, ErrCronOrRunAtMissing):
		writeError(w, http.StatusBadRequest, "cron_expr or run_at must be set")
	case errors.Is(err, ErrCronAndRunAtBoth):
		writeError(w, http.StatusBadRequest, "cron_expr and run_at are mutually exclusive")
	case errors.Is(err, ErrInvalidCron):
		writeError(w, http.StatusBadRequest, "invalid cron expression")
	case errors.Is(err, ErrInvalidActionCfg):
		writeError(w, http.StatusBadRequest, "invalid action config (must be JSON)")
	default:
		h.logger.Error("scheduler.Handler",
			"op", op, "error", err,
			"request_id", reqctx.RequestIDFromContext(r.Context()))
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("scheduler.writeJSON: encode error", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}
