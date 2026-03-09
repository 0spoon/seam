package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/userdb"
)

// maxInputLen is the maximum allowed length (in bytes) for user-provided
// query, selection, and prompt fields in AI handler requests.
const maxInputLen = 100 * 1024 // 100 KB

// Default rate limit: 10 requests per minute per user.
const (
	defaultRateLimit = rate.Limit(10.0 / 60.0) // 10 per minute
	defaultRateBurst = 10                      // allow short bursts
)

// Handler handles HTTP requests for AI endpoints.
type Handler struct {
	queue       *Queue
	chatSvc     *ChatService
	synthesizer *Synthesizer
	linker      *AutoLinker
	embedder    *Embedder
	writer      *Writer
	dbManager   userdb.Manager
	logger      *slog.Logger

	// Per-user rate limiters for AI endpoints.
	limiterMu sync.Mutex
	limiters  map[string]*rate.Limiter
}

// NewHandler creates a new AI Handler.
func NewHandler(
	queue *Queue,
	chatSvc *ChatService,
	synthesizer *Synthesizer,
	linker *AutoLinker,
	embedder *Embedder,
	writer *Writer,
	dbManager userdb.Manager,
	logger *slog.Logger,
) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		queue:       queue,
		chatSvc:     chatSvc,
		synthesizer: synthesizer,
		linker:      linker,
		embedder:    embedder,
		writer:      writer,
		dbManager:   dbManager,
		logger:      logger,
		limiters:    make(map[string]*rate.Limiter),
	}
}

// getLimiter returns the per-user rate limiter, creating one if necessary.
func (h *Handler) getLimiter(userID string) *rate.Limiter {
	h.limiterMu.Lock()
	defer h.limiterMu.Unlock()
	if lim, ok := h.limiters[userID]; ok {
		return lim
	}
	lim := rate.NewLimiter(defaultRateLimit, defaultRateBurst)
	h.limiters[userID] = lim
	return lim
}

// rateLimitMiddleware enforces per-user rate limiting on AI endpoints.
func (h *Handler) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := reqctx.UserIDFromContext(r.Context())
		if userID == "" {
			next.ServeHTTP(w, r)
			return
		}
		lim := h.getLimiter(userID)
		if !lim.Allow() {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded, try again later")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Routes returns a chi router with AI routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(h.rateLimitMiddleware)
	r.Post("/ask", h.ask)
	r.Post("/synthesize", h.synthesize)
	r.Post("/synthesize/stream", h.synthesizeStream)
	r.Post("/reindex-embeddings", h.reindexEmbeddings)
	r.Get("/notes/{id}/related", h.relatedNotes)
	r.Post("/notes/{id}/assist", h.assist)
	return r
}

// ask handles POST /api/ai/ask
func (h *Handler) ask(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	if h.chatSvc == nil {
		writeError(w, http.StatusServiceUnavailable, "AI services not configured")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Query   string        `json:"query"`
		History []ChatMessage `json:"history,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Query) > maxInputLen {
		writeError(w, http.StatusBadRequest, "query exceeds maximum length")
		return
	}
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	// Validate history message roles to prevent prompt injection.
	for _, msg := range req.History {
		if msg.Role != "user" && msg.Role != "assistant" {
			writeError(w, http.StatusBadRequest, "invalid message role in history: only 'user' and 'assistant' are allowed")
			return
		}
	}

	result, err := h.chatSvc.Ask(r.Context(), userID, req.Query, req.History)
	if err != nil {
		h.logger.Error("ai.Handler.ask: chat failed", "error", err)
		writeError(w, http.StatusInternalServerError, "chat request failed")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// synthesize handles POST /api/ai/synthesize
func (h *Handler) synthesize(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	if h.synthesizer == nil {
		writeError(w, http.StatusServiceUnavailable, "AI services not configured")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload SynthesizePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(payload.Prompt) > maxInputLen {
		writeError(w, http.StatusBadRequest, "prompt exceeds maximum length")
		return
	}
	if payload.Scope == "" || payload.Prompt == "" {
		writeError(w, http.StatusBadRequest, "scope and prompt are required")
		return
	}

	result, err := h.synthesizer.Synthesize(r.Context(), userID, payload)
	if err != nil {
		h.logger.Error("ai.Handler.synthesize: synthesis failed", "error", err)
		writeError(w, http.StatusInternalServerError, "synthesis failed")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// synthesizeStream handles POST /api/ai/synthesize/stream
// It streams synthesis tokens back to the client using Server-Sent Events.
func (h *Handler) synthesizeStream(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	if h.synthesizer == nil {
		writeError(w, http.StatusServiceUnavailable, "AI services not configured")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload SynthesizePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(payload.Prompt) > maxInputLen {
		writeError(w, http.StatusBadRequest, "prompt exceeds maximum length")
		return
	}
	if payload.Scope == "" || payload.Prompt == "" {
		writeError(w, http.StatusBadRequest, "scope and prompt are required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	tokenCh, errCh := h.synthesizer.SynthesizeStream(r.Context(), userID, payload)

	for token := range tokenCh {
		data, _ := json.Marshal(map[string]string{"token": token})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Check for stream errors.
	for err := range errCh {
		if err != nil {
			h.logger.Error("ai.Handler.synthesizeStream: stream error", "error", err)
			errData, _ := json.Marshal(map[string]string{"error": err.Error()})
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", errData)
			flusher.Flush()
			return
		}
	}

	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

// relatedNotes handles GET /api/ai/notes/{id}/related
func (h *Handler) relatedNotes(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	if h.embedder == nil {
		writeError(w, http.StatusServiceUnavailable, "AI services not configured")
		return
	}

	noteID := chi.URLParam(r, "id")
	if noteID == "" {
		writeError(w, http.StatusBadRequest, "note ID is required")
		return
	}

	limit := 5
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 20 {
			limit = n
		}
	}

	// Use the embedder's FindRelated to encapsulate embedding + query logic.
	chromaResults, err := h.embedder.FindRelated(r.Context(), noteID, userID, limit*3)
	if err != nil {
		h.logger.Error("ai.Handler.relatedNotes: find related failed", "error", err)
		// Check if the error is from a note not found (query note step).
		if strings.Contains(err.Error(), "query note") {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to find related notes")
		return
	}

	// Deduplicate and exclude self.
	type relatedResult struct {
		NoteID string  `json:"note_id"`
		Title  string  `json:"title"`
		Score  float64 `json:"score"`
	}

	seen := map[string]bool{noteID: true}
	var results []relatedResult

	for _, cr := range chromaResults {
		rid := cr.Metadata["note_id"]
		if rid == "" || seen[rid] {
			continue
		}
		seen[rid] = true

		score := 1.0 - cr.Distance
		if score < 0 {
			score = 0
		}

		results = append(results, relatedResult{
			NoteID: rid,
			Title:  cr.Metadata["title"],
			Score:  score,
		})

		if len(results) >= limit {
			break
		}
	}

	if results == nil {
		results = []relatedResult{}
	}

	writeJSON(w, http.StatusOK, results)
}

// reindexEmbeddings handles POST /api/ai/reindex-embeddings
func (h *Handler) reindexEmbeddings(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	if h.embedder == nil || h.queue == nil {
		writeError(w, http.StatusServiceUnavailable, "embeddings not configured")
		return
	}

	count, err := h.embedder.ReindexAll(r.Context(), userID, h.queue)
	if err != nil {
		h.logger.Error("ai.Handler.reindexEmbeddings: failed", "error", err)
		writeError(w, http.StatusInternalServerError, "reindex failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]int{"enqueued": count})
}

// assist handles POST /api/ai/notes/{id}/assist
func (h *Handler) assist(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	noteID := chi.URLParam(r, "id")
	if noteID == "" {
		writeError(w, http.StatusBadRequest, "note ID is required")
		return
	}

	if h.writer == nil {
		writeError(w, http.StatusServiceUnavailable, "AI writing assist not configured")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Action    string `json:"action"`
		Selection string `json:"selection"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Selection) > maxInputLen {
		writeError(w, http.StatusBadRequest, "selection exceeds maximum length")
		return
	}
	if req.Action == "" {
		writeError(w, http.StatusBadRequest, "action is required")
		return
	}

	result, err := h.writer.Assist(r.Context(), userID, noteID, req.Action, req.Selection)
	if err != nil {
		if errors.Is(err, ErrInvalidAction) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, ErrEmptyInput) {
			writeError(w, http.StatusBadRequest, "no text to process")
			return
		}
		h.logger.Error("ai.Handler.assist: failed", "error", err)
		writeError(w, http.StatusInternalServerError, "writing assist failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"result": result})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("ai.writeJSON: encode error", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
