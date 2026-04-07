package assistant

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/internal/reqctx"
)

// Handler provides HTTP endpoints for the assistant.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

// NewHandler creates a new assistant Handler.
func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		service: service,
		logger:  logger,
	}
}

// Routes returns a chi.Router with assistant endpoints.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/chat", h.chat)
	r.Post("/chat/stream", h.chatStream)
	r.Get("/conversations/{conversationID}/actions", h.listActions)
	r.Post("/actions/{actionID}/approve", h.approveAction)
	r.Post("/actions/{actionID}/reject", h.rejectAction)

	// Profile endpoints.
	r.Get("/profile", h.getProfile)
	r.Put("/profile", h.updateProfile)

	// Memory endpoints.
	r.Get("/memories", h.listMemories)
	r.Post("/memories", h.createMemory)
	r.Delete("/memories/{memoryID}", h.deleteMemory)

	return r
}

// chatRequest is the JSON body for POST /api/assistant/chat.
type chatRequest struct {
	ConversationID string           `json:"conversation_id"`
	Message        string           `json:"message"`
	History        []historyMessage `json:"history,omitempty"`
}

// historyMessage represents a message in the conversation history.
type historyMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	ToolCalls  []ai.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	Name       string        `json:"name,omitempty"`
}

func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	if req.ConversationID == "" {
		writeError(w, http.StatusBadRequest, "conversation_id is required")
		return
	}

	history := convertHistory(req.History)

	chatReq := ChatRequest{
		UserID:         userID,
		ConversationID: req.ConversationID,
		Message:        req.Message,
		History:        history,
	}

	resp, err := h.service.Chat(r.Context(), chatReq)
	if err != nil {
		h.logger.Error("assistant.Handler.chat: chat failed",
			"error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "assistant chat failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
		h.logger.Error("assistant.Handler.chat: encode response failed", "error", encErr)
	}
}

func (h *Handler) chatStream(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	if req.ConversationID == "" {
		writeError(w, http.StatusBadRequest, "conversation_id is required")
		return
	}

	history := convertHistory(req.History)

	chatReq := ChatRequest{
		UserID:         userID,
		ConversationID: req.ConversationID,
		Message:        req.Message,
		History:        history,
	}

	eventCh, err := h.service.ChatStream(r.Context(), chatReq)
	if err != nil {
		h.logger.Error("assistant.Handler.chatStream: stream failed",
			"error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "assistant stream failed")
		return
	}

	// SSE response.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	for event := range eventCh {
		select {
		case <-r.Context().Done():
			return
		default:
		}
		data, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (h *Handler) listActions(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	conversationID := chi.URLParam(r, "conversationID")
	if conversationID == "" {
		writeError(w, http.StatusBadRequest, "conversation_id is required")
		return
	}

	actions, err := h.service.ListActions(r.Context(), userID, conversationID)
	if err != nil {
		h.logger.Error("assistant.Handler.listActions: failed",
			"error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "failed to list actions")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if encErr := json.NewEncoder(w).Encode(map[string]interface{}{
		"actions": actions,
	}); encErr != nil {
		h.logger.Error("assistant.Handler.listActions: encode response failed", "error", encErr)
	}
}

func (h *Handler) approveAction(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	actionID := chi.URLParam(r, "actionID")
	if actionID == "" {
		writeError(w, http.StatusBadRequest, "action_id is required")
		return
	}

	result, err := h.service.ApproveAction(r.Context(), userID, actionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "action not found")
			return
		}
		h.logger.Error("assistant.Handler.approveAction: failed",
			"error", err, "user_id", userID, "action_id", actionID)
		writeError(w, http.StatusInternalServerError, "failed to approve action")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if encErr := json.NewEncoder(w).Encode(result); encErr != nil {
		h.logger.Error("assistant.Handler.approveAction: encode response failed", "error", encErr)
	}
}

func (h *Handler) rejectAction(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	actionID := chi.URLParam(r, "actionID")
	if actionID == "" {
		writeError(w, http.StatusBadRequest, "action_id is required")
		return
	}

	if err := h.service.RejectAction(r.Context(), userID, actionID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "action not found")
			return
		}
		h.logger.Error("assistant.Handler.rejectAction: failed",
			"error", err, "user_id", userID, "action_id", actionID)
		writeError(w, http.StatusInternalServerError, "failed to reject action")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getProfile(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	profile, err := h.service.GetProfile(r.Context(), userID)
	if err != nil {
		h.logger.Error("assistant.Handler.getProfile: failed",
			"error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "failed to get profile")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if encErr := json.NewEncoder(w).Encode(profile); encErr != nil {
		h.logger.Error("assistant.Handler.getProfile: encode failed", "error", encErr)
	}
}

func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var profile UserProfile
	if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.UpdateProfile(r.Context(), userID, &profile); err != nil {
		h.logger.Error("assistant.Handler.updateProfile: failed",
			"error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "failed to update profile")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if encErr := json.NewEncoder(w).Encode(map[string]interface{}{"updated": true}); encErr != nil {
		h.logger.Error("assistant.Handler.updateProfile: encode failed", "error", encErr)
	}
}

func (h *Handler) listMemories(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	category := r.URL.Query().Get("category")
	limit := intQueryParam(r, "limit", 50)
	offset := intQueryParam(r, "offset", 0)

	memories, total, err := h.service.ListMemories(r.Context(), userID, category, limit, offset)
	if err != nil {
		h.logger.Error("assistant.Handler.listMemories: failed",
			"error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "failed to list memories")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if encErr := json.NewEncoder(w).Encode(map[string]interface{}{
		"memories": memories,
		"total":    total,
	}); encErr != nil {
		h.logger.Error("assistant.Handler.listMemories: encode failed", "error", encErr)
	}
}

func (h *Handler) createMemory(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req struct {
		Content  string `json:"content"`
		Category string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	m := &Memory{
		Content:  req.Content,
		Category: req.Category,
		Source:   "manual",
	}

	if err := h.service.CreateMemory(r.Context(), userID, m); err != nil {
		h.logger.Error("assistant.Handler.createMemory: failed",
			"error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "failed to create memory")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if encErr := json.NewEncoder(w).Encode(m); encErr != nil {
		h.logger.Error("assistant.Handler.createMemory: encode failed", "error", encErr)
	}
}

func (h *Handler) deleteMemory(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	memoryID := chi.URLParam(r, "memoryID")
	if memoryID == "" {
		writeError(w, http.StatusBadRequest, "memory_id is required")
		return
	}

	if err := h.service.DeleteMemory(r.Context(), userID, memoryID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "memory not found")
			return
		}
		h.logger.Error("assistant.Handler.deleteMemory: failed",
			"error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "failed to delete memory")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func convertHistory(messages []historyMessage) []ai.ToolMessage {
	result := make([]ai.ToolMessage, 0, len(messages))
	for _, m := range messages {
		tm := ai.ToolMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
			ToolCalls:  m.ToolCalls,
		}
		result = append(result, tm)
	}
	return result
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func intQueryParam(r *http.Request, key string, defaultVal int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return defaultVal
	}
	return v
}
