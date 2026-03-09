package search

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/internal/userdb"
)

// SemanticResult represents a single semantic search result.
type SemanticResult struct {
	NoteID  string  `json:"note_id"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
}

// SemanticSearcher performs semantic search using embeddings.
type SemanticSearcher struct {
	ollama    *ai.OllamaClient
	chroma    *ai.ChromaClient
	dbManager userdb.Manager
	model     string
	logger    *slog.Logger
}

// NewSemanticSearcher creates a new SemanticSearcher.
func NewSemanticSearcher(ollama *ai.OllamaClient, chroma *ai.ChromaClient, dbManager userdb.Manager, model string, logger *slog.Logger) *SemanticSearcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &SemanticSearcher{
		ollama:    ollama,
		chroma:    chroma,
		dbManager: dbManager,
		model:     model,
		logger:    logger,
	}
}

// Search embeds the query text, queries ChromaDB for nearest neighbors,
// deduplicates by note ID (taking the best score), and returns ranked results.
func (s *SemanticSearcher) Search(ctx context.Context, userID, query string, limit int) ([]SemanticResult, error) {
	if limit <= 0 {
		limit = 10
	}

	// Generate embedding for the query.
	queryEmbedding, err := s.ollama.GenerateEmbedding(ctx, s.model, query)
	if err != nil {
		return nil, fmt.Errorf("search.SemanticSearcher.Search: embed query: %w", err)
	}

	// Get the collection ID.
	colName := ai.CollectionName(userID)
	colID, err := s.chroma.GetOrCreateCollection(ctx, colName)
	if err != nil {
		return nil, fmt.Errorf("search.SemanticSearcher.Search: get collection: %w", err)
	}

	// Query ChromaDB for more results than requested to allow deduplication.
	// Multiple chunks from the same note can match, so request extra.
	nResults := limit * 3
	if nResults < 20 {
		nResults = 20
	}

	chromaResults, err := s.chroma.Query(ctx, colID, queryEmbedding, nResults)
	if err != nil {
		return nil, fmt.Errorf("search.SemanticSearcher.Search: query chroma: %w", err)
	}

	// Deduplicate by note_id, keeping the best (lowest distance) score per note.
	type bestResult struct {
		noteID   string
		title    string
		distance float64
	}
	seen := make(map[string]*bestResult)
	for _, cr := range chromaResults {
		noteID := cr.Metadata["note_id"]
		if noteID == "" {
			continue
		}
		if existing, ok := seen[noteID]; !ok || cr.Distance < existing.distance {
			seen[noteID] = &bestResult{
				noteID:   noteID,
				title:    cr.Metadata["title"],
				distance: cr.Distance,
			}
		}
	}

	// Convert to SemanticResult and sort by score (lower distance = better).
	// Distance is converted to a similarity score for the API.
	var results []SemanticResult
	for _, br := range seen {
		// Convert cosine distance to similarity: score = 1 - distance.
		// Clamp to [0, 1].
		score := 1.0 - br.distance
		if score < 0 {
			score = 0
		}
		if score > 1 {
			score = 1
		}

		// Fetch a snippet from the note body.
		snippet := s.getNoteSnippet(ctx, userID, br.noteID, query)

		results = append(results, SemanticResult{
			NoteID:  br.noteID,
			Title:   br.title,
			Score:   score,
			Snippet: snippet,
		})
	}

	// Sort by score descending.
	sortSemanticResults(results)

	// Limit results.
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// getNoteSnippet returns a short snippet from a note's body.
func (s *SemanticSearcher) getNoteSnippet(ctx context.Context, userID, noteID, query string) string {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return ""
	}

	var body string
	err = db.QueryRowContext(ctx, `SELECT body FROM notes WHERE id = ?`, noteID).Scan(&body)
	if err != nil {
		return ""
	}

	return extractSnippet(body, query, 200)
}

// extractSnippet returns a snippet of the body around the first occurrence
// of any query word, or the beginning of the body if no match.
// It operates on runes to avoid splitting multi-byte UTF-8 characters.
func extractSnippet(body, query string, maxLen int) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}

	runes := []rune(body)

	lowerBody := strings.ToLower(body)
	queryWords := strings.Fields(strings.ToLower(query))

	// Find the first matching word position (in byte offset), then convert
	// to rune offset for slicing.
	bestBytePos := -1
	for _, word := range queryWords {
		if pos := strings.Index(lowerBody, word); pos >= 0 {
			if bestBytePos < 0 || pos < bestBytePos {
				bestBytePos = pos
			}
		}
	}

	// Convert byte position to rune position.
	bestPos := -1
	if bestBytePos >= 0 {
		bestPos = len([]rune(body[:bestBytePos]))
	}

	start := 0
	if bestPos > 0 {
		start = bestPos - maxLen/4
		if start < 0 {
			start = 0
		}
	}

	end := start + maxLen
	if end > len(runes) {
		end = len(runes)
	}

	snippet := string(runes[start:end])

	// Clean up: trim to word boundaries.
	if start > 0 {
		if idx := strings.IndexByte(snippet, ' '); idx >= 0 && idx < 20 {
			snippet = "..." + snippet[idx+1:]
		}
	}
	if end < len(runes) {
		if idx := strings.LastIndexByte(snippet, ' '); idx >= 0 && idx > len(snippet)-20 {
			snippet = snippet[:idx] + "..."
		}
	}

	return snippet
}

// sortSemanticResults sorts results by score descending.
func sortSemanticResults(results []SemanticResult) {
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Score > results[j-1].Score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}
