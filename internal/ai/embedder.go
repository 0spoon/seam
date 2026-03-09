package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/katata/seam/internal/userdb"
)

// chunkSize is the approximate number of characters per chunk.
// Targeting ~512 tokens; rough estimate is 4 chars per token for English.
const chunkSize = 2048

// chunkOverlap is the number of characters that overlap between adjacent chunks.
const chunkOverlap = 200

// Embedder manages the embedding pipeline: chunking notes, generating
// embeddings via Ollama, and storing them in ChromaDB.
type Embedder struct {
	ollama    *OllamaClient
	chroma    *ChromaClient
	dbManager userdb.Manager
	model     string
	logger    *slog.Logger
}

// NewEmbedder creates a new Embedder.
func NewEmbedder(ollama *OllamaClient, chroma *ChromaClient, dbManager userdb.Manager, model string, logger *slog.Logger) *Embedder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Embedder{
		ollama:    ollama,
		chroma:    chroma,
		dbManager: dbManager,
		model:     model,
		logger:    logger,
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
func (e *Embedder) EmbedNote(ctx context.Context, userID, noteID, title, body string) error {
	colID, err := e.EnsureCollection(ctx, userID)
	if err != nil {
		return fmt.Errorf("ai.Embedder.EmbedNote: %w", err)
	}

	// Prepare text: prepend title for better semantic matching.
	fullText := title + "\n\n" + body
	chunks := ChunkText(fullText, chunkSize, chunkOverlap)
	if len(chunks) == 0 {
		return nil
	}

	var ids []string
	var embeddings [][]float64
	var metadatas []map[string]string

	for i, chunk := range chunks {
		docID := fmt.Sprintf("%s_chunk_%d", noteID, i)
		embedding, err := e.ollama.GenerateEmbedding(ctx, e.model, chunk)
		if err != nil {
			return fmt.Errorf("ai.Embedder.EmbedNote: embed chunk %d: %w", i, err)
		}

		ids = append(ids, docID)
		embeddings = append(embeddings, embedding)
		metadatas = append(metadatas, map[string]string{
			"note_id":     noteID,
			"title":       title,
			"chunk_index": fmt.Sprintf("%d", i),
			"user_id":     userID,
		})
	}

	if err := e.chroma.UpsertDocuments(ctx, colID, ids, embeddings, metadatas); err != nil {
		return fmt.Errorf("ai.Embedder.EmbedNote: upsert: %w", err)
	}

	e.logger.Debug("ai.Embedder.EmbedNote: embedded note",
		"user_id", userID, "note_id", noteID, "chunks", len(chunks))

	return nil
}

// maxChunksPerNote is the maximum number of chunk IDs to generate when
// deleting embeddings for a note. Based on chunkSize=2048 and overlap=200,
// a 400KB note (~200K tokens) would produce roughly 200 chunks. We use a
// generous upper bound to avoid leaving orphaned embeddings.
const maxChunksPerNote = 500

// DeleteNoteEmbeddings removes all embeddings for a note from ChromaDB.
func (e *Embedder) DeleteNoteEmbeddings(ctx context.Context, userID, noteID string) error {
	colID, err := e.EnsureCollection(ctx, userID)
	if err != nil {
		return fmt.Errorf("ai.Embedder.DeleteNoteEmbeddings: %w", err)
	}

	// Delete all chunks for this note. We generate IDs for up to
	// maxChunksPerNote. ChromaDB silently ignores IDs that do not exist.
	var ids []string
	for i := 0; i < maxChunksPerNote; i++ {
		ids = append(ids, fmt.Sprintf("%s_chunk_%d", noteID, i))
	}

	if err := e.chroma.DeleteDocuments(ctx, colID, ids); err != nil {
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

	if err := e.EmbedNote(ctx, task.UserID, payload.NoteID, title, body); err != nil {
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

	rows, err := db.QueryContext(ctx, `SELECT id FROM notes`)
	if err != nil {
		return 0, fmt.Errorf("ai.Embedder.ReindexAll: query notes: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var noteID string
		if err := rows.Scan(&noteID); err != nil {
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

	e.logger.Info("ai.Embedder.ReindexAll: enqueued embedding tasks",
		"user_id", userID, "count", count)

	return count, rows.Err()
}

// ChunkText splits text into overlapping chunks for embedding.
// Exported for testing.
func ChunkText(text string, size, overlap int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(text) <= size {
		return []string{text}
	}

	var chunks []string
	start := 0
	for start < len(text) {
		end := start + size
		if end > len(text) {
			end = len(text)
		}

		// Try to break at a paragraph or sentence boundary.
		if end < len(text) {
			// Look for paragraph break in the last 20% of the chunk.
			breakZone := end - size/5
			if breakZone < start {
				breakZone = start
			}
			if idx := strings.LastIndex(text[breakZone:end], "\n\n"); idx >= 0 {
				end = breakZone + idx + 2
			} else if idx := strings.LastIndex(text[breakZone:end], ". "); idx >= 0 {
				end = breakZone + idx + 2
			} else if idx := strings.LastIndex(text[breakZone:end], "\n"); idx >= 0 {
				end = breakZone + idx + 1
			}
		}

		chunk := strings.TrimSpace(text[start:end])
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
