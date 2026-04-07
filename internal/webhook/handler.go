package webhook

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
)

// Handler handles HTTP requests for webhook endpoints.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

// NewHandler creates a new webhook Handler.
func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// Routes returns a chi router with all webhook routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.create)
	r.Get("/", h.list)
	r.Get("/events", h.listEvents)
	r.Get("/{id}", h.get)
	r.Put("/{id}", h.update)
	r.Delete("/{id}", h.delete)
	r.Get("/{id}/deliveries", h.deliveries)
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

	wh, err := h.service.Create(r.Context(), userID, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrNameRequired):
			writeError(w, http.StatusBadRequest, "name is required")
		case errors.Is(err, ErrURLRequired):
			writeError(w, http.StatusBadRequest, "url is required")
		case errors.Is(err, ErrEventsRequired):
			writeError(w, http.StatusBadRequest, "event_types is required")
		case errors.Is(err, ErrInvalidURL):
			writeError(w, http.StatusBadRequest, "invalid webhook URL")
		case errors.Is(err, ErrInvalidEventType):
			writeError(w, http.StatusBadRequest, "invalid event type")
		default:
			h.logger.Error("create webhook failed",
				"error", err, "request_id", reqctx.RequestIDFromContext(r.Context()))
			writeError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	// Return the secret only in the create response so the user can
	// configure HMAC signature verification. Subsequent list/get
	// responses omit it (json:"-" on the Secret field).
	type createResponse struct {
		*Webhook
		Secret string `json:"secret"`
	}
	writeJSON(w, http.StatusCreated, createResponse{Webhook: wh, Secret: wh.Secret})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	activeOnly := r.URL.Query().Get("active") == "true"

	webhooks, err := h.service.List(r.Context(), userID, activeOnly)
	if err != nil {
		h.logger.Error("list webhooks failed",
			"error", err, "request_id", reqctx.RequestIDFromContext(r.Context()))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if webhooks == nil {
		webhooks = []*Webhook{}
	}

	writeJSON(w, http.StatusOK, webhooks)
}

func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request) {
	if reqctx.UserIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}
	writeJSON(w, http.StatusOK, AllEventTypes)
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

	wh, err := h.service.Get(r.Context(), userID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "webhook not found")
			return
		}
		h.logger.Error("get webhook failed",
			"error", err, "request_id", reqctx.RequestIDFromContext(r.Context()))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, wh)
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

	wh, err := h.service.Update(r.Context(), userID, id, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "webhook not found")
		case errors.Is(err, ErrNameRequired):
			writeError(w, http.StatusBadRequest, "name is required")
		case errors.Is(err, ErrURLRequired):
			writeError(w, http.StatusBadRequest, "url is required")
		case errors.Is(err, ErrEventsRequired):
			writeError(w, http.StatusBadRequest, "event_types is required")
		case errors.Is(err, ErrInvalidURL):
			writeError(w, http.StatusBadRequest, "invalid webhook URL")
		case errors.Is(err, ErrInvalidEventType):
			writeError(w, http.StatusBadRequest, "invalid event type")
		default:
			h.logger.Error("update webhook failed",
				"error", err, "request_id", reqctx.RequestIDFromContext(r.Context()))
			writeError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	writeJSON(w, http.StatusOK, wh)
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

	err := h.service.Delete(r.Context(), userID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "webhook not found")
			return
		}
		h.logger.Error("delete webhook failed",
			"error", err, "request_id", reqctx.RequestIDFromContext(r.Context()))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deliveries(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	webhookID := chi.URLParam(r, "id")
	if webhookID == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 100 {
		limit = 100
	}

	deliveries, err := h.service.Deliveries(r.Context(), userID, webhookID, limit)
	if err != nil {
		h.logger.Error("list deliveries failed",
			"error", err, "request_id", reqctx.RequestIDFromContext(r.Context()))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if deliveries == nil {
		deliveries = []*Delivery{}
	}

	writeJSON(w, http.StatusOK, deliveries)
}

// writeJSON encodes v as JSON and writes it to the response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("webhook.writeJSON: encode error", "error", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		slog.Warn("webhook.writeError: encode error", "error", err)
	}
}
