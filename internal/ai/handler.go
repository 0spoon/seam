package ai

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/userdb"
)

// maxInputLen is the maximum allowed length (in bytes) for user-provided
// query, selection, and prompt fields in AI handler requests.
const maxInputLen = 100 * 1024 // 100 KB

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
	}
}

// Routes returns a chi router with AI routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/ask", h.ask)
	r.Post("/synthesize", h.synthesize)
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

// relatedNotes handles GET /api/ai/notes/{id}/related
func (h *Handler) relatedNotes(w http.ResponseWriter, r *http.Request) {
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

	limit := 5
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 20 {
			limit = n
		}
	}

	// Get the note's content.
	db, err := h.dbManager.Open(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	var title, body string
	err = db.QueryRowContext(r.Context(),
		`SELECT title, body FROM notes WHERE id = ?`, noteID,
	).Scan(&title, &body)
	if err != nil {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}

	// Embed and search for similar notes, excluding self.
	text := title + "\n\n" + body
	if len(text) > 3000 {
		text = text[:3000]
	}

	queryEmbedding, err := h.embedder.ollama.GenerateEmbedding(r.Context(), h.embedder.model, text)
	if err != nil {
		h.logger.Error("ai.Handler.relatedNotes: embed failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to generate embedding")
		return
	}

	colName := CollectionName(userID)
	colID, err := h.embedder.chroma.GetOrCreateCollection(r.Context(), colName)
	if err != nil {
		h.logger.Error("ai.Handler.relatedNotes: collection failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to access embeddings")
		return
	}

	chromaResults, err := h.embedder.chroma.Query(r.Context(), colID, queryEmbedding, limit*3)
	if err != nil {
		h.logger.Error("ai.Handler.relatedNotes: query failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to query related notes")
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
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
