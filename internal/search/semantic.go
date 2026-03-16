package search

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

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

	// C-24: Batch-load note bodies in a single query instead of N+1.
	noteIDs := make([]string, 0, len(seen))
	for noteID := range seen {
		noteIDs = append(noteIDs, noteID)
	}
	bodyMap := s.batchGetNoteBodies(ctx, userID, noteIDs)

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

		// Extract snippet from the batch-loaded body.
		snippet := extractSnippet(bodyMap[br.noteID], query, 200)

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

// SearchScoped performs semantic search with a ChromaDB metadata where filter.
// The where map is passed to ChromaDB's query filter (e.g., {"scope": "agent"}).
// If where is nil, behaves identically to Search.
func (s *SemanticSearcher) SearchScoped(ctx context.Context, userID, query string, limit int, where map[string]interface{}) ([]SemanticResult, error) {
	if limit <= 0 {
		limit = 10
	}

	queryEmbedding, err := s.ollama.GenerateEmbedding(ctx, s.model, query)
	if err != nil {
		return nil, fmt.Errorf("search.SemanticSearcher.SearchScoped: embed query: %w", err)
	}

	colName := ai.CollectionName(userID)
	colID, err := s.chroma.GetOrCreateCollection(ctx, colName)
	if err != nil {
		return nil, fmt.Errorf("search.SemanticSearcher.SearchScoped: get collection: %w", err)
	}

	nResults := limit * 3
	if nResults < 20 {
		nResults = 20
	}

	var chromaResults []ai.QueryResult
	if len(where) > 0 {
		chromaResults, err = s.chroma.QueryWithFilter(ctx, colID, queryEmbedding, nResults, where)
	} else {
		chromaResults, err = s.chroma.Query(ctx, colID, queryEmbedding, nResults)
	}
	if err != nil {
		return nil, fmt.Errorf("search.SemanticSearcher.SearchScoped: query chroma: %w", err)
	}

	// Deduplicate by note_id.
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

	noteIDs := make([]string, 0, len(seen))
	for noteID := range seen {
		noteIDs = append(noteIDs, noteID)
	}
	bodyMap := s.batchGetNoteBodies(ctx, userID, noteIDs)

	var results []SemanticResult
	for _, br := range seen {
		score := 1.0 - br.distance
		if score < 0 {
			score = 0
		}
		if score > 1 {
			score = 1
		}
		snippet := extractSnippet(bodyMap[br.noteID], query, 200)
		results = append(results, SemanticResult{
			NoteID:  br.noteID,
			Title:   br.title,
			Score:   score,
			Snippet: snippet,
		})
	}

	sortSemanticResults(results)
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// batchGetNoteBodies loads note bodies for all given note IDs in a single query,
// replacing the N+1 per-note getNoteSnippet pattern (C-24, C-25).
func (s *SemanticSearcher) batchGetNoteBodies(ctx context.Context, userID string, noteIDs []string) map[string]string {
	result := make(map[string]string, len(noteIDs))
	if len(noteIDs) == 0 {
		return result
	}

	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		s.logger.Warn("search.SemanticSearcher.batchGetNoteBodies: open db failed",
			"user_id", userID, "error", err)
		return result
	}

	placeholders := make([]string, len(noteIDs))
	args := make([]interface{}, len(noteIDs))
	for i, id := range noteIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT id, body FROM notes WHERE id IN (%s)`,
		strings.Join(placeholders, ","),
	)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		s.logger.Warn("search.SemanticSearcher.batchGetNoteBodies: query failed",
			"error", err)
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			s.logger.Warn("search.SemanticSearcher.batchGetNoteBodies: scan failed",
				"error", err)
			continue
		}
		result[id] = body
	}
	if err := rows.Err(); err != nil {
		s.logger.Warn("search.SemanticSearcher.batchGetNoteBodies: rows error",
			"error", err)
	}

	return result
}

// batchGetNoteTimestamps loads updated_at timestamps for all given note IDs
// in a single query, returning a map of note_id -> updated_at.
func (s *SemanticSearcher) batchGetNoteTimestamps(ctx context.Context, userID string, noteIDs []string) (map[string]time.Time, error) {
	result := make(map[string]time.Time, len(noteIDs))
	if len(noteIDs) == 0 {
		return result, nil
	}

	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		s.logger.Warn("search.SemanticSearcher.batchGetNoteTimestamps: open db failed",
			"user_id", userID, "error", err)
		return result, fmt.Errorf("search.SemanticSearcher.batchGetNoteTimestamps: open db: %w", err)
	}

	placeholders := make([]string, len(noteIDs))
	args := make([]interface{}, len(noteIDs))
	for i, id := range noteIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT id, updated_at FROM notes WHERE id IN (%s)`,
		strings.Join(placeholders, ","),
	)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		s.logger.Warn("search.SemanticSearcher.batchGetNoteTimestamps: query failed",
			"error", err)
		return result, fmt.Errorf("search.SemanticSearcher.batchGetNoteTimestamps: query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, updatedAtStr string
		if err := rows.Scan(&id, &updatedAtStr); err != nil {
			s.logger.Warn("search.SemanticSearcher.batchGetNoteTimestamps: scan failed",
				"error", err)
			continue
		}
		if t, parseErr := time.Parse(time.RFC3339, updatedAtStr); parseErr == nil {
			result[id] = t
		}
	}
	if err := rows.Err(); err != nil {
		s.logger.Warn("search.SemanticSearcher.batchGetNoteTimestamps: rows error",
			"error", err)
		return result, fmt.Errorf("search.SemanticSearcher.batchGetNoteTimestamps: rows: %w", err)
	}

	return result, nil
}

// SearchWithRecency performs semantic search with recency-weighted scoring.
// The recencyBias parameter (0.0-1.0) controls how much recency boosts scores.
func (s *SemanticSearcher) SearchWithRecency(ctx context.Context, userID, query string, limit int, recencyBias float64) ([]SemanticResult, error) {
	if limit <= 0 {
		limit = 10
	}

	queryEmbedding, err := s.ollama.GenerateEmbedding(ctx, s.model, query)
	if err != nil {
		return nil, fmt.Errorf("search.SemanticSearcher.SearchWithRecency: embed query: %w", err)
	}

	colName := ai.CollectionName(userID)
	colID, err := s.chroma.GetOrCreateCollection(ctx, colName)
	if err != nil {
		return nil, fmt.Errorf("search.SemanticSearcher.SearchWithRecency: get collection: %w", err)
	}

	nResults := limit * 3
	if nResults < 20 {
		nResults = 20
	}

	chromaResults, err := s.chroma.Query(ctx, colID, queryEmbedding, nResults)
	if err != nil {
		return nil, fmt.Errorf("search.SemanticSearcher.SearchWithRecency: query chroma: %w", err)
	}

	// Deduplicate by note_id.
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

	noteIDs := make([]string, 0, len(seen))
	for noteID := range seen {
		noteIDs = append(noteIDs, noteID)
	}
	bodyMap := s.batchGetNoteBodies(ctx, userID, noteIDs)
	tsMap, err := s.batchGetNoteTimestamps(ctx, userID, noteIDs)
	if err != nil {
		s.logger.Warn("search.SemanticSearcher.SearchWithRecency: failed to get timestamps, skipping recency adjustment",
			"error", err)
		tsMap = nil // Ensure no partial results are used for recency adjustment.
	}

	var results []SemanticResult
	for _, br := range seen {
		score := 1.0 - br.distance
		if score < 0 {
			score = 0
		}
		if score > 1 {
			score = 1
		}

		// Additive blend: mix similarity score with recency to preserve
		// relative ordering. At max recencyBias (1.0), up to 30% of the
		// final score comes from recency.
		if updatedAt, ok := tsMap[br.noteID]; ok {
			recency := recencyWeight(updatedAt)
			score = score*(1-recencyBias*0.3) + recency*recencyBias*0.3
		}

		snippet := extractSnippet(bodyMap[br.noteID], query, 200)
		results = append(results, SemanticResult{
			NoteID:  br.noteID,
			Title:   br.title,
			Score:   score,
			Snippet: snippet,
		})
	}

	// Sort by score descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// SearchScopedWithRecency performs scoped semantic search with recency-weighted scoring.
// The recencyBias parameter (0.0-1.0) controls how much recency boosts scores.
func (s *SemanticSearcher) SearchScopedWithRecency(ctx context.Context, userID, query string, limit int, where map[string]interface{}, recencyBias float64) ([]SemanticResult, error) {
	if limit <= 0 {
		limit = 10
	}

	queryEmbedding, err := s.ollama.GenerateEmbedding(ctx, s.model, query)
	if err != nil {
		return nil, fmt.Errorf("search.SemanticSearcher.SearchScopedWithRecency: embed query: %w", err)
	}

	colName := ai.CollectionName(userID)
	colID, err := s.chroma.GetOrCreateCollection(ctx, colName)
	if err != nil {
		return nil, fmt.Errorf("search.SemanticSearcher.SearchScopedWithRecency: get collection: %w", err)
	}

	nResults := limit * 3
	if nResults < 20 {
		nResults = 20
	}

	var chromaResults []ai.QueryResult
	if len(where) > 0 {
		chromaResults, err = s.chroma.QueryWithFilter(ctx, colID, queryEmbedding, nResults, where)
	} else {
		chromaResults, err = s.chroma.Query(ctx, colID, queryEmbedding, nResults)
	}
	if err != nil {
		return nil, fmt.Errorf("search.SemanticSearcher.SearchScopedWithRecency: query chroma: %w", err)
	}

	// Deduplicate by note_id.
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

	noteIDs := make([]string, 0, len(seen))
	for noteID := range seen {
		noteIDs = append(noteIDs, noteID)
	}
	bodyMap := s.batchGetNoteBodies(ctx, userID, noteIDs)
	tsMap, err := s.batchGetNoteTimestamps(ctx, userID, noteIDs)
	if err != nil {
		s.logger.Warn("search.SemanticSearcher.SearchScopedWithRecency: failed to get timestamps, skipping recency adjustment",
			"error", err)
		tsMap = nil // Ensure no partial results are used for recency adjustment.
	}

	var results []SemanticResult
	for _, br := range seen {
		score := 1.0 - br.distance
		if score < 0 {
			score = 0
		}
		if score > 1 {
			score = 1
		}

		// Additive blend: mix similarity score with recency to preserve
		// relative ordering. At max recencyBias (1.0), up to 30% of the
		// final score comes from recency.
		if updatedAt, ok := tsMap[br.noteID]; ok {
			recency := recencyWeight(updatedAt)
			score = score*(1-recencyBias*0.3) + recency*recencyBias*0.3
		}

		snippet := extractSnippet(bodyMap[br.noteID], query, 200)
		results = append(results, SemanticResult{
			NoteID:  br.noteID,
			Title:   br.title,
			Score:   score,
			Snippet: snippet,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
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
