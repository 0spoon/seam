package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/katata/seam/internal/userdb"
)

// Default chunk parameters for embedding.
const (
	defaultChunkSize    = 2048
	defaultChunkOverlap = 200
)

// defaultReindexLimit is the maximum number of notes loaded in a single ReindexAll call.
const defaultReindexLimit = 10000

// Embedder manages the embedding pipeline: chunking notes, generating
// embeddings via a local embedding model, and storing them in ChromaDB.
type Embedder struct {
	embedder     EmbeddingGenerator
	chroma       *ChromaClient
	dbManager    userdb.Manager
	model        string
	logger       *slog.Logger
	chunkSize    int
	chunkOverlap int
}

// NewEmbedder creates a new Embedder. Optional chunkSize and chunkOverlap can
// be provided; zero values use defaults (2048 and 200 respectively).
func NewEmbedder(embedder EmbeddingGenerator, chroma *ChromaClient, dbManager userdb.Manager, model string, logger *slog.Logger, opts ...func(*Embedder)) *Embedder {
	if logger == nil {
		logger = slog.Default()
	}
	e := &Embedder{
		embedder:     embedder,
		chroma:       chroma,
		dbManager:    dbManager,
		model:        model,
		logger:       logger,
		chunkSize:    defaultChunkSize,
		chunkOverlap: defaultChunkOverlap,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// WithChunkSize returns an option that sets the chunk size for embedding.
func WithChunkSize(size int) func(*Embedder) {
	return func(e *Embedder) {
		if size > 0 {
			e.chunkSize = size
		}
	}
}

// WithChunkOverlap returns an option that sets the chunk overlap for embedding.
func WithChunkOverlap(overlap int) func(*Embedder) {
	return func(e *Embedder) {
		if overlap >= 0 {
			e.chunkOverlap = overlap
		}
	}
}

// EnsureCollection creates the ChromaDB collection for a user if it does not exist.
// Returns the collection ID.
func (e *Embedder) EnsureCollection(ctx context.Context, userID string) (string, error) {
	name := CollectionName(userID)
	colID, err := e.chroma.GetOrCreateCollection(ctx, name)
	if err != nil {
		return "", fmt.Errorf("ai.Embedder.EnsureCollection: %w", err)
	}
	return colID, nil
}

// EmbedNote generates embeddings for a note and upserts them into ChromaDB.
// Long notes are chunked. Each chunk is embedded separately and stored with
// the note ID plus chunk index as the document ID.
// Optional extraMeta is merged into each chunk's metadata (e.g., scope).
func (e *Embedder) EmbedNote(ctx context.Context, userID, noteID, title, body string, extraMeta ...map[string]string) error {
	colID, err := e.EnsureCollection(ctx, userID)
	if err != nil {
		return fmt.Errorf("ai.Embedder.EmbedNote: %w", err)
	}

	// Prepare text: prepend title for better semantic matching.
	fullText := title + "\n\n" + body
	chunks := ChunkText(fullText, e.chunkSize, e.chunkOverlap)
	if len(chunks) == 0 {
		return nil
	}

	var ids []string
	var embeddings [][]float64
	var metadatas []map[string]string

	for i, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("ai.Embedder.EmbedNote: context cancelled before chunk %d: %w", i, err)
		}
		docID := fmt.Sprintf("%s_chunk_%d", noteID, i)
		embedding, err := e.embedder.GenerateEmbedding(ctx, e.model, chunk)
		if err != nil {
			return fmt.Errorf("ai.Embedder.EmbedNote: embed chunk %d: %w", i, err)
		}

		meta := map[string]string{
			"note_id":     noteID,
			"title":       title,
			"chunk_index": fmt.Sprintf("%d", i),
			"user_id":     userID,
			"scope":       "user", // default scope
		}
		// Merge extra metadata (e.g., scope override).
		if len(extraMeta) > 0 && extraMeta[0] != nil {
			for k, v := range extraMeta[0] {
				meta[k] = v
			}
		}

		ids = append(ids, docID)
		embeddings = append(embeddings, embedding)
		metadatas = append(metadatas, meta)
	}

	if err := e.chroma.UpsertDocuments(ctx, colID, ids, embeddings, metadatas); err != nil {
		return fmt.Errorf("ai.Embedder.EmbedNote: upsert: %w", err)
	}

	e.logger.Debug("ai.Embedder.EmbedNote: embedded note",
		"user_id", userID, "note_id", noteID, "chunks", len(chunks))

	return nil
}

// DeleteNoteEmbeddings removes all embeddings for a note from ChromaDB.
// C-29: Uses metadata-based deletion (`where: {note_id: noteID}`) instead
// of generating 500 chunk IDs per delete. This is both more efficient and
// correctly handles notes with any number of chunks.
func (e *Embedder) DeleteNoteEmbeddings(ctx context.Context, userID, noteID string) error {
	colID, err := e.EnsureCollection(ctx, userID)
	if err != nil {
		return fmt.Errorf("ai.Embedder.DeleteNoteEmbeddings: %w", err)
	}

	where := map[string]interface{}{
		"note_id": noteID,
	}
	if err := e.chroma.DeleteByMetadata(ctx, colID, where); err != nil {
		return fmt.Errorf("ai.Embedder.DeleteNoteEmbeddings: %w", err)
	}

	return nil
}

// HandleEmbedTask is a TaskHandler for embed tasks.
func (e *Embedder) HandleEmbedTask(ctx context.Context, task *Task) (json.RawMessage, error) {
	var payload EmbedPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("ai.Embedder.HandleEmbedTask: unmarshal payload: %w", err)
	}

	// Read the note from the user's database.
	db, err := e.dbManager.Open(ctx, task.UserID)
	if err != nil {
		return nil, fmt.Errorf("ai.Embedder.HandleEmbedTask: open db: %w", err)
	}

	var title, body string
	err = db.QueryRowContext(ctx,
		`SELECT title, body FROM notes WHERE id = ?`, payload.NoteID,
	).Scan(&title, &body)
	if err != nil {
		return nil, fmt.Errorf("ai.Embedder.HandleEmbedTask: query note: %w", err)
	}

	var extra map[string]string
	if payload.Scope != "" {
		extra = map[string]string{"scope": payload.Scope}
	}

	if err := e.EmbedNote(ctx, task.UserID, payload.NoteID, title, body, extra); err != nil {
		return nil, err
	}

	return json.RawMessage(`{"status":"embedded"}`), nil
}

// HandleDeleteEmbedTask is a TaskHandler for delete_embed tasks.
func (e *Embedder) HandleDeleteEmbedTask(ctx context.Context, task *Task) (json.RawMessage, error) {
	var payload DeleteEmbedPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("ai.Embedder.HandleDeleteEmbedTask: unmarshal payload: %w", err)
	}

	if err := e.DeleteNoteEmbeddings(ctx, task.UserID, payload.NoteID); err != nil {
		return nil, err
	}

	return json.RawMessage(`{"status":"deleted"}`), nil
}

// ReindexAll enqueues embed tasks for all notes belonging to the given user.
// This is used for bulk reindexing after model changes or initial Ollama setup.
func (e *Embedder) ReindexAll(ctx context.Context, userID string, queue *Queue) (int, error) {
	db, err := e.dbManager.Open(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("ai.Embedder.ReindexAll: open db: %w", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT id FROM notes LIMIT ?`, defaultReindexLimit+1)
	if err != nil {
		return 0, fmt.Errorf("ai.Embedder.ReindexAll: query notes: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var noteID string
		if err := rows.Scan(&noteID); err != nil {
			e.logger.Warn("ai.Embedder.ReindexAll: scan note id", "error", err)
			continue
		}
		payload, _ := json.Marshal(EmbedPayload{NoteID: noteID})
		if err := queue.Enqueue(ctx, &Task{
			UserID:   userID,
			Type:     TaskTypeEmbed,
			Priority: PriorityBackground,
			Payload:  payload,
		}); err != nil {
			e.logger.Warn("ai.Embedder.ReindexAll: failed to enqueue",
				"note_id", noteID, "error", err)
			continue
		}
		count++
	}

	if count > defaultReindexLimit {
		e.logger.Warn("ai.Embedder.ReindexAll: note count exceeds limit, some notes were not reindexed",
			"user_id", userID, "limit", defaultReindexLimit, "enqueued", count)
	}

	e.logger.Info("ai.Embedder.ReindexAll: enqueued embedding tasks",
		"user_id", userID, "count", count)

	return count, rows.Err()
}

// FindRelated finds notes related to the given note by embedding similarity.
// It returns ChromaDB query results for the given note's content, requesting
// nResults neighbors (caller should over-request to allow dedup/filtering).
func (e *Embedder) FindRelated(ctx context.Context, noteID, userID string, nResults int) ([]QueryResult, error) {
	db, err := e.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("ai.Embedder.FindRelated: open db: %w", err)
	}

	var title, body string
	err = db.QueryRowContext(ctx,
		`SELECT title, body FROM notes WHERE id = ?`, noteID,
	).Scan(&title, &body)
	if err != nil {
		return nil, fmt.Errorf("ai.Embedder.FindRelated: query note: %w", err)
	}

	text := title + "\n\n" + body
	if runes := []rune(text); len(runes) > 3000 {
		text = string(runes[:3000])
	}

	queryEmbedding, err := e.embedder.GenerateEmbedding(ctx, e.model, text)
	if err != nil {
		return nil, fmt.Errorf("ai.Embedder.FindRelated: generate embedding: %w", err)
	}

	colName := CollectionName(userID)
	colID, err := e.chroma.GetOrCreateCollection(ctx, colName)
	if err != nil {
		return nil, fmt.Errorf("ai.Embedder.FindRelated: get collection: %w", err)
	}

	results, err := e.chroma.Query(ctx, colID, queryEmbedding, nResults)
	if err != nil {
		return nil, fmt.Errorf("ai.Embedder.FindRelated: query: %w", err)
	}

	return results, nil
}

// ChunkText splits text into overlapping chunks for embedding.
// Uses rune-based indexing to avoid splitting multi-byte UTF-8 characters.
// Exported for testing.
func ChunkText(text string, size, overlap int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	runes := []rune(text)
	if len(runes) <= size {
		return []string{text}
	}

	var chunks []string
	start := 0
	for start < len(runes) {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}

		// Try to break at a paragraph or sentence boundary.
		if end < len(runes) {
			segment := string(runes[start:end])
			segRuneLen := utf8.RuneCountInString(segment)
			breakZone := segRuneLen - segRuneLen/5
			// Convert rune-based breakZone to byte offset for string slicing.
			breakZone = len(string([]rune(segment)[:breakZone]))
			if breakZone < 0 {
				breakZone = 0
			}
			if idx := strings.LastIndex(segment[breakZone:], "\n\n"); idx >= 0 {
				// Convert byte offset back to rune count from start.
				end = start + runeCount(segment[:breakZone+idx+2])
			} else if idx := strings.LastIndex(segment[breakZone:], ". "); idx >= 0 {
				end = start + runeCount(segment[:breakZone+idx+2])
			} else if idx := strings.LastIndex(segment[breakZone:], "\n"); idx >= 0 {
				end = start + runeCount(segment[:breakZone+idx+1])
			}
		}

		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		// Advance with overlap.
		nextStart := end - overlap
		if nextStart <= start {
			nextStart = end
		}
		start = nextStart
	}

	return chunks
}

// runeCount returns the number of runes in a string.
func runeCount(s string) int {
	return len([]rune(s))
}
