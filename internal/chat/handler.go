package chat

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
)

// ServiceInterface defines the business logic methods for chat history.
// It is satisfied by *Service and enables unit testing with mocks.
type ServiceInterface interface {
	CreateConversation(ctx context.Context, userID string) (*Conversation, error)
	ListConversations(ctx context.Context, userID string, limit, offset int) ([]Conversation, int, error)
	GetConversation(ctx context.Context, userID, conversationID string) (*Conversation, []Message, error)
	DeleteConversation(ctx context.Context, userID, conversationID string) error
	AddMessage(ctx context.Context, userID string, msg Message) error
}

// Handler handles HTTP requests for chat history endpoints.
type Handler struct {
	service ServiceInterface
	logger  *slog.Logger
}

// NewHandler creates a new chat Handler.
func NewHandler(service ServiceInterface, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// Routes returns a chi router with all chat routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/conversations", h.createConversation)
	r.Get("/conversations", h.listConversations)
	r.Get("/conversations/{id}", h.getConversation)
	r.Delete("/conversations/{id}", h.deleteConversation)
	r.Post("/conversations/{id}/messages", h.addMessage)
	return r
}

func (h *Handler) createConversation(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	conv, err := h.service.CreateConversation(r.Context(), userID)
	if err != nil {
		h.logger.Error("create conversation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, conv)
}

func (h *Handler) listConversations(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	limit := 20
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	convs, total, err := h.service.ListConversations(r.Context(), userID, limit, offset)
	if err != nil {
		h.logger.Error("list conversations failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Return empty array instead of null.
	if convs == nil {
		convs = []Conversation{}
	}

	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	writeJSON(w, http.StatusOK, convs)
}

func (h *Handler) getConversation(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "conversation ID is required")
		return
	}

	conv, msgs, err := h.service.GetConversation(r.Context(), userID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found")
			return
		}
		h.logger.Error("get conversation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Return empty array instead of null for messages.
	if msgs == nil {
		msgs = []Message{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"conversation": conv,
		"messages":     msgs,
	})
}

func (h *Handler) deleteConversation(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "conversation ID is required")
		return
	}

	if err := h.service.DeleteConversation(r.Context(), userID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found")
			return
		}
		h.logger.Error("delete conversation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) addMessage(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	conversationID := chi.URLParam(r, "id")
	if conversationID == "" {
		writeError(w, http.StatusBadRequest, "conversation ID is required")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Role      string     `json:"role"`
		Content   string     `json:"content"`
		Citations []Citation `json:"citations,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Role == "" || req.Content == "" {
		writeError(w, http.StatusBadRequest, "role and content are required")
		return
	}
	if req.Role != "user" && req.Role != "assistant" {
		writeError(w, http.StatusBadRequest, "role must be 'user' or 'assistant'")
		return
	}

	msg := Message{
		ConversationID: conversationID,
		Role:           req.Role,
		Content:        req.Content,
		Citations:      req.Citations,
	}

	if err := h.service.AddMessage(r.Context(), userID, msg); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found")
			return
		}
		if errors.Is(err, ErrInvalidRole) {
			writeError(w, http.StatusBadRequest, "invalid message role: must be 'user' or 'assistant'")
			return
		}
		h.logger.Error("add message failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// writeJSON encodes v as JSON and writes it to the response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("chat.writeJSON: encode error", "error", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		slog.Warn("chat.writeError: encode error", "error", err)
	}
}
