package ai

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/reqlimits"
	"github.com/katata/seam/internal/userdb"
)

// maxInputLen is the maximum allowed length (in bytes) for user-provided
// query, selection, and prompt fields in AI handler requests.
const maxInputLen = 10 * 1024 * 1024 // 10 MB

// Handler handles HTTP requests for AI endpoints.
type Handler struct {
	queue       *Queue
	chatSvc     *ChatService
	synthesizer *Synthesizer
	linker      *AutoLinker
	embedder    *Embedder
	writer      *Writer
	suggester   *Suggester
	dbManager   userdb.Manager
	logger      *slog.Logger
}

// NewHandler creates a new AI Handler.
func NewHandler(
	queue *Queue,
	chatSvc *ChatService,
	synthesizer *Synthesizer,
	linker *AutoLinker,
	embedder *Embedder,
	writer *Writer,
	suggester *Suggester,
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
		suggester:   suggester,
		dbManager:   dbManager,
		logger:      logger,
	}
}

// Routes returns a chi router with AI routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/ask", h.ask)
	r.Post("/synthesize", h.synthesize)
	r.Post("/synthesize/stream", h.synthesizeStream)
	r.Post("/reindex-embeddings", h.reindexEmbeddings)
	r.Get("/notes/{id}/related", h.relatedNotes)
	r.Post("/notes/{id}/assist", h.assist)
	r.Post("/suggest-tags", h.suggestTags)
	r.Post("/suggest-project", h.suggestProject)
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

	r.Body = http.MaxBytesReader(w, r.Body, reqlimits.MaxJSONBody)
	var req struct {
		Query   string        `json:"query"`
		History []ChatMessage `json:"history,omitempty"`
		Summary string        `json:"summary,omitempty"`
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
	if len(req.Summary) > maxInputLen {
		writeError(w, http.StatusBadRequest, "summary exceeds maximum length")
		return
	}

	// Validate history message roles to prevent prompt injection.
	for _, msg := range req.History {
		if msg.Role != "user" && msg.Role != "assistant" {
			writeError(w, http.StatusBadRequest, "invalid message role in history: only 'user' and 'assistant' are allowed")
			return
		}
	}

	result, err := h.chatSvc.Ask(r.Context(), userID, req.Query, req.History, req.Summary)
	if err != nil {
		h.logger.Error("ai.Handler.ask: chat failed", "error", err)
		if !writeProviderError(w, err) {
			writeError(w, http.StatusInternalServerError, "chat request failed")
		}
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

	r.Body = http.MaxBytesReader(w, r.Body, reqlimits.MaxJSONBody)
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
	if payload.Scope != "project" && payload.Scope != "tag" {
		writeError(w, http.StatusBadRequest, "scope must be 'project' or 'tag'")
		return
	}

	result, err := h.synthesizer.Synthesize(r.Context(), userID, payload)
	if err != nil {
		h.logger.Error("ai.Handler.synthesize: synthesis failed", "error", err)
		if !writeProviderError(w, err) {
			writeError(w, http.StatusInternalServerError, "synthesis failed")
		}
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

	r.Body = http.MaxBytesReader(w, r.Body, reqlimits.MaxJSONBody)
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
	if payload.Scope != "project" && payload.Scope != "tag" {
		writeError(w, http.StatusBadRequest, "scope must be 'project' or 'tag'")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Disable the global WriteTimeout for this SSE stream. Each write
	// resets the deadline so idle connections still get cleaned up.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	tokenCh, errCh := h.synthesizer.SynthesizeStream(r.Context(), userID, payload)

	for token := range tokenCh {
		// Reset write deadline for each token to allow long-running streams
		// while still detecting dead connections.
		_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
		data, _ := json.Marshal(map[string]string{"token": token})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Check for stream errors.
	for err := range errCh {
		if err != nil {
			h.logger.Error("ai.Handler.synthesizeStream: stream error", "error", err)
			errData, _ := json.Marshal(map[string]string{"error": "synthesis stream failed"})
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
		if errors.Is(err, sql.ErrNoRows) {
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

	r.Body = http.MaxBytesReader(w, r.Body, reqlimits.MaxJSONBody)
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
			writeError(w, http.StatusBadRequest, "invalid action")
			return
		}
		if errors.Is(err, ErrEmptyInput) {
			writeError(w, http.StatusBadRequest, "no text to process")
			return
		}
		h.logger.Error("ai.Handler.assist: failed", "error", err)
		if !writeProviderError(w, err) {
			writeError(w, http.StatusInternalServerError, "writing assist failed")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"result": result})
}

// suggestTags handles POST /api/ai/suggest-tags
func (h *Handler) suggestTags(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	if h.suggester == nil {
		writeError(w, http.StatusServiceUnavailable, "AI services not configured")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, reqlimits.MaxJSONBody)
	var req struct {
		NoteID string `json:"note_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.NoteID == "" {
		writeError(w, http.StatusBadRequest, "note_id is required")
		return
	}

	db, err := h.dbManager.Open(r.Context(), userID)
	if err != nil {
		h.logger.Error("ai.Handler.suggestTags: open db", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Load the note.
	var noteTitle, noteBody string
	err = db.QueryRowContext(r.Context(),
		`SELECT title, body FROM notes WHERE id = ?`, req.NoteID,
	).Scan(&noteTitle, &noteBody)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		h.logger.Error("ai.Handler.suggestTags: query note", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Load existing tags.
	rows, err := db.QueryContext(r.Context(),
		`SELECT DISTINCT t.name FROM tags t JOIN note_tags nt ON nt.tag_id = t.id ORDER BY t.name`)
	if err != nil {
		h.logger.Error("ai.Handler.suggestTags: query tags", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()

	var existingTags []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			h.logger.Error("ai.Handler.suggestTags: scan tag", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		existingTags = append(existingTags, name)
	}
	if err := rows.Err(); err != nil {
		h.logger.Error("ai.Handler.suggestTags: rows error", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tags, err := h.suggester.SuggestTags(r.Context(), noteTitle, noteBody, existingTags)
	if err != nil {
		h.logger.Error("ai.Handler.suggestTags: suggest failed", "error", err)
		if !writeProviderError(w, err) {
			writeError(w, http.StatusInternalServerError, "tag suggestion failed")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"tags": tags})
}

// suggestProject handles POST /api/ai/suggest-project
func (h *Handler) suggestProject(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	if h.suggester == nil {
		writeError(w, http.StatusServiceUnavailable, "AI services not configured")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, reqlimits.MaxJSONBody)
	var req struct {
		NoteID string `json:"note_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.NoteID == "" {
		writeError(w, http.StatusBadRequest, "note_id is required")
		return
	}

	db, err := h.dbManager.Open(r.Context(), userID)
	if err != nil {
		h.logger.Error("ai.Handler.suggestProject: open db", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Load the note.
	var noteTitle, noteBody string
	err = db.QueryRowContext(r.Context(),
		`SELECT title, body FROM notes WHERE id = ?`, req.NoteID,
	).Scan(&noteTitle, &noteBody)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		h.logger.Error("ai.Handler.suggestProject: query note", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Load projects.
	projectRows, err := db.QueryContext(r.Context(),
		`SELECT id, name, description FROM projects ORDER BY name`)
	if err != nil {
		h.logger.Error("ai.Handler.suggestProject: query projects", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer projectRows.Close()

	var projects []ProjectInfo
	for projectRows.Next() {
		var p ProjectInfo
		if err := projectRows.Scan(&p.ID, &p.Name, &p.Description); err != nil {
			h.logger.Error("ai.Handler.suggestProject: scan project", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		projects = append(projects, p)
	}
	if err := projectRows.Err(); err != nil {
		h.logger.Error("ai.Handler.suggestProject: rows error", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	suggestions, err := h.suggester.SuggestProject(r.Context(), noteTitle, noteBody, projects)
	if err != nil {
		h.logger.Error("ai.Handler.suggestProject: suggest failed", "error", err)
		if !writeProviderError(w, err) {
			writeError(w, http.StatusInternalServerError, "project suggestion failed")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"projects": suggestions})
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
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		slog.Warn("ai.writeError: encode error", "error", err)
	}
}

// writeProviderError maps provider-specific sentinel errors to appropriate
// HTTP status codes and sanitized messages. Returns true if the error was
// handled; false if the caller should use a generic 500 response.
func writeProviderError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "LLM provider rate limit exceeded, try again later")
		return true
	case errors.Is(err, ErrAuthFailed):
		writeError(w, http.StatusBadGateway, "LLM provider authentication failed; check server configuration")
		return true
	case errors.Is(err, ErrModelNotFound):
		writeError(w, http.StatusBadGateway, "configured model not found at LLM provider")
		return true
	default:
		return false
	}
}
