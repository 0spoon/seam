package assistant

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

// memoryHalfLifeDays is the half-life used by memory decay scoring.
// A memory's relevance is multiplied by exp(-age_days / halfLife) where
// age is measured from last_accessed (or created_at if never accessed).
// At 30 days, relevance halves; at 60 days, it quarters; etc.
const memoryHalfLifeDays = 30.0

// Memory represents a long-term memory entry persisted across conversations.
type Memory struct {
	ID           string    `json:"id"`
	Category     string    `json:"category"` // fact, preference, decision, commitment
	Content      string    `json:"content"`
	Source       string    `json:"source,omitempty"` // conversation_id or "manual"
	Confidence   float64   `json:"confidence"`       // 0.0 to 1.0
	LastAccessed time.Time `json:"last_accessed,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

// Memory categories.
const (
	MemoryCategoryFact       = "fact"
	MemoryCategoryPreference = "preference"
	MemoryCategoryDecision   = "decision"
	MemoryCategoryCommitment = "commitment"
	// MemoryCategorySummary is reserved for conversation summaries.
	// Entries with this category are produced by the assistant when
	// folding older conversation turns out of the verbatim history
	// window. The Source field stores the originating conversation
	// ID. There is at most one summary per conversation.
	MemoryCategorySummary = "summary"
)

// ValidMemoryCategories is the set of allowed memory categories for
// user-driven memory operations. The summary category is intentionally
// excluded so the LLM cannot create summaries via the save_memory
// tool; summaries are managed exclusively by the conversation
// summarization path.
var ValidMemoryCategories = map[string]bool{
	MemoryCategoryFact:       true,
	MemoryCategoryPreference: true,
	MemoryCategoryDecision:   true,
	MemoryCategoryCommitment: true,
}

// MemoryStore provides CRUD operations for assistant memories.
type MemoryStore struct{}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// SaveMemory inserts or updates a memory entry.
func (s *MemoryStore) SaveMemory(ctx context.Context, db *sql.DB, m *Memory) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO memories (id, category, content, source, confidence, last_accessed, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		     category = excluded.category,
		     content = excluded.content,
		     confidence = excluded.confidence,
		     last_accessed = excluded.last_accessed,
		     expires_at = excluded.expires_at`,
		m.ID, m.Category, m.Content,
		nullableStr(m.Source), m.Confidence,
		nullableTime(m.LastAccessed),
		m.CreatedAt.Format(time.RFC3339),
		nullableTime(m.ExpiresAt),
	)
	if err != nil {
		return fmt.Errorf("assistant.MemoryStore.SaveMemory: %w", err)
	}
	return nil
}

// GetMemory retrieves a single memory by ID.
func (s *MemoryStore) GetMemory(ctx context.Context, db *sql.DB, id string) (*Memory, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, category, content, source, confidence, last_accessed, created_at, expires_at
		 FROM memories WHERE id = ?`, id)

	m, err := scanMemoryRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("assistant.MemoryStore.GetMemory: %w", err)
	}
	return m, nil
}

// ListMemories returns memories filtered by optional category.
func (s *MemoryStore) ListMemories(ctx context.Context, db *sql.DB, category string, limit, offset int) ([]*Memory, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	var countQ, listQ string
	var args []interface{}

	if category != "" {
		countQ = `SELECT COUNT(*) FROM memories WHERE category = ?`
		listQ = `SELECT id, category, content, source, confidence, last_accessed, created_at, expires_at
		         FROM memories WHERE category = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`
		args = []interface{}{category, limit, offset}
	} else {
		countQ = `SELECT COUNT(*) FROM memories`
		listQ = `SELECT id, category, content, source, confidence, last_accessed, created_at, expires_at
		         FROM memories ORDER BY created_at DESC LIMIT ? OFFSET ?`
		args = []interface{}{limit, offset}
	}

	var total int
	if category != "" {
		err := db.QueryRowContext(ctx, countQ, category).Scan(&total)
		if err != nil {
			return nil, 0, fmt.Errorf("assistant.MemoryStore.ListMemories: count: %w", err)
		}
	} else {
		err := db.QueryRowContext(ctx, countQ).Scan(&total)
		if err != nil {
			return nil, 0, fmt.Errorf("assistant.MemoryStore.ListMemories: count: %w", err)
		}
	}

	rows, err := db.QueryContext(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("assistant.MemoryStore.ListMemories: %w", err)
	}
	defer rows.Close()

	var memories []*Memory
	for rows.Next() {
		m, scanErr := scanMemoryRows(rows)
		if scanErr != nil {
			return nil, 0, fmt.Errorf("assistant.MemoryStore.ListMemories: scan: %w", scanErr)
		}
		memories = append(memories, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("assistant.MemoryStore.ListMemories: rows: %w", err)
	}
	return memories, total, nil
}

// SearchMemories performs full-text search over memory content with
// time-decay relevance scoring. Results are ranked by a composite of
// the FTS5 BM25 score, an exponential decay over the memory's age,
// and the memory's confidence value. The query is sanitized to prevent
// FTS5 injection.
func (s *MemoryStore) SearchMemories(ctx context.Context, db *sql.DB, query string, limit int) ([]*Memory, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	sanitized := sanitizeMemoryFTSQuery(query)
	if sanitized == "" {
		return nil, nil
	}

	// Fetch a wider candidate pool than the caller asked for so the
	// in-Go re-ranking has room to surface stale-but-strong matches
	// alongside fresh-but-weak ones.
	fetchLimit := limit * 3
	if fetchLimit < 30 {
		fetchLimit = 30
	}

	rows, err := db.QueryContext(ctx,
		`SELECT m.id, m.category, m.content, m.source, m.confidence,
		        m.last_accessed, m.created_at, m.expires_at,
		        bm25(memories_fts) AS rank
		 FROM memories_fts
		 JOIN memories m ON memories_fts.rowid = m.rowid
		 WHERE memories_fts MATCH ?
		 ORDER BY rank
		 LIMIT ?`, sanitized, fetchLimit)
	if err != nil {
		return nil, fmt.Errorf("assistant.MemoryStore.SearchMemories: %w", err)
	}
	defer rows.Close()

	type scored struct {
		mem   *Memory
		score float64
	}
	now := time.Now()
	var hits []scored
	for rows.Next() {
		m, rank, scanErr := scanMemoryRowWithRank(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("assistant.MemoryStore.SearchMemories: scan: %w", scanErr)
		}
		hits = append(hits, scored{mem: m, score: relevanceScore(m, rank, now)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("assistant.MemoryStore.SearchMemories: rows: %w", err)
	}

	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].score > hits[j].score
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]*Memory, len(hits))
	for i, h := range hits {
		out[i] = h.mem
	}
	return out, nil
}

// relevanceScore combines the FTS5 BM25 score, an exponential time
// decay over the memory's age, and the memory's confidence into a
// single ranking value. Higher is more relevant.
//
// SQLite's bm25() returns the negative BM25 score, so the more
// relevant a match, the more negative the rank. We negate it to get a
// positive baseline score; values cluster around 0.5-5 in practice.
func relevanceScore(m *Memory, bm25Rank float64, now time.Time) float64 {
	relevance := -bm25Rank
	if relevance < 0 {
		relevance = 0
	}

	ref := m.LastAccessed
	if ref.IsZero() {
		ref = m.CreatedAt
	}
	var decay float64 = 1.0
	if !ref.IsZero() {
		ageDays := now.Sub(ref).Hours() / 24.0
		if ageDays < 0 {
			ageDays = 0
		}
		// True half-life decay: at age == halfLife, decay == 0.5;
		// at 2*halfLife, 0.25; etc.
		decay = math.Pow(0.5, ageDays/memoryHalfLifeDays)
	}

	confidence := m.Confidence
	if confidence <= 0 {
		confidence = 1.0
	}

	return relevance * decay * confidence
}

// TouchMemories updates last_accessed for the given memory IDs in a
// single statement. IDs that do not exist are silently skipped. This
// keeps frequently-recalled memories ranked higher in future searches.
func (s *MemoryStore) TouchMemories(ctx context.Context, db *sql.DB, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids)+1)
	args[0] = time.Now().UTC().Format(time.RFC3339)
	for i, id := range ids {
		placeholders[i] = "?"
		args[i+1] = id
	}
	query := `UPDATE memories SET last_accessed = ? WHERE id IN (` +
		strings.Join(placeholders, ",") + `)`
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("assistant.MemoryStore.TouchMemories: %w", err)
	}
	return nil
}

// DeleteMemory removes a memory by ID.
func (s *MemoryStore) DeleteMemory(ctx context.Context, db *sql.DB, id string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("assistant.MemoryStore.DeleteMemory: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("assistant.MemoryStore.DeleteMemory: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchMemory updates the last_accessed timestamp for a memory.
func (s *MemoryStore) TouchMemory(ctx context.Context, db *sql.DB, id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.ExecContext(ctx, `UPDATE memories SET last_accessed = ? WHERE id = ?`, now, id)
	if err != nil {
		return fmt.Errorf("assistant.MemoryStore.TouchMemory: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("assistant.MemoryStore.TouchMemory: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// conversationSummaryID returns the deterministic memory ID used for
// the summary of a given conversation. There is exactly one summary
// per conversation; subsequent saves overwrite it via UPSERT.
func conversationSummaryID(conversationID string) string {
	return "conv_summary_" + conversationID
}

// GetConversationSummary returns the current summary memory for the
// given conversation, or (nil, ErrNotFound) if none has been recorded.
func (s *MemoryStore) GetConversationSummary(ctx context.Context, db *sql.DB, conversationID string) (*Memory, error) {
	if conversationID == "" {
		return nil, ErrNotFound
	}
	return s.GetMemory(ctx, db, conversationSummaryID(conversationID))
}

// SaveConversationSummary upserts the summary memory for a
// conversation. The summary text replaces any prior summary for the
// same conversation. Empty summaries delete any existing entry.
func (s *MemoryStore) SaveConversationSummary(ctx context.Context, db *sql.DB, conversationID, summary string) error {
	if conversationID == "" {
		return fmt.Errorf("assistant.MemoryStore.SaveConversationSummary: conversationID is required")
	}
	id := conversationSummaryID(conversationID)
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" {
		err := s.DeleteMemory(ctx, db, id)
		if err != nil && err != ErrNotFound {
			return fmt.Errorf("assistant.MemoryStore.SaveConversationSummary: %w", err)
		}
		return nil
	}
	now := time.Now().UTC()
	m := &Memory{
		ID:         id,
		Category:   MemoryCategorySummary,
		Content:    trimmed,
		Source:     conversationID,
		Confidence: 1.0,
		CreatedAt:  now,
	}
	return s.SaveMemory(ctx, db, m)
}

// GetRecentMemories returns the N most recently created memories,
// optionally filtered by category.
func (s *MemoryStore) GetRecentMemories(ctx context.Context, db *sql.DB, limit int) ([]*Memory, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, category, content, source, confidence, last_accessed, created_at, expires_at
		 FROM memories
		 ORDER BY created_at DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("assistant.MemoryStore.GetRecentMemories: %w", err)
	}
	defer rows.Close()

	var memories []*Memory
	for rows.Next() {
		m, scanErr := scanMemoryRows(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("assistant.MemoryStore.GetRecentMemories: scan: %w", scanErr)
		}
		memories = append(memories, m)
	}
	return memories, rows.Err()
}

func scanMemoryRow(row *sql.Row) (*Memory, error) {
	var m Memory
	var source, lastAccessed, expiresAt sql.NullString
	var createdAt string

	err := row.Scan(&m.ID, &m.Category, &m.Content, &source, &m.Confidence,
		&lastAccessed, &createdAt, &expiresAt)
	if err != nil {
		return nil, err
	}

	if source.Valid {
		m.Source = source.String
	}
	if parsed, parseErr := time.Parse(time.RFC3339, createdAt); parseErr == nil {
		m.CreatedAt = parsed
	} else {
		slog.Warn("assistant.scanMemoryRow: failed to parse created_at",
			"memory_id", m.ID, "value", createdAt, "error", parseErr)
	}
	if lastAccessed.Valid {
		if parsed, parseErr := time.Parse(time.RFC3339, lastAccessed.String); parseErr == nil {
			m.LastAccessed = parsed
		}
	}
	if expiresAt.Valid {
		if parsed, parseErr := time.Parse(time.RFC3339, expiresAt.String); parseErr == nil {
			m.ExpiresAt = parsed
		}
	}
	return &m, nil
}

func scanMemoryRows(rows *sql.Rows) (*Memory, error) {
	var m Memory
	var source, lastAccessed, expiresAt sql.NullString
	var createdAt string

	err := rows.Scan(&m.ID, &m.Category, &m.Content, &source, &m.Confidence,
		&lastAccessed, &createdAt, &expiresAt)
	if err != nil {
		return nil, err
	}

	if source.Valid {
		m.Source = source.String
	}
	if parsed, parseErr := time.Parse(time.RFC3339, createdAt); parseErr == nil {
		m.CreatedAt = parsed
	} else {
		slog.Warn("assistant.scanMemoryRows: failed to parse created_at",
			"memory_id", m.ID, "value", createdAt, "error", parseErr)
	}
	if lastAccessed.Valid {
		if parsed, parseErr := time.Parse(time.RFC3339, lastAccessed.String); parseErr == nil {
			m.LastAccessed = parsed
		}
	}
	if expiresAt.Valid {
		if parsed, parseErr := time.Parse(time.RFC3339, expiresAt.String); parseErr == nil {
			m.ExpiresAt = parsed
		}
	}
	return &m, nil
}

// scanMemoryRowWithRank scans a row that includes the FTS5 bm25() rank
// as its trailing column. The rank is returned alongside the memory so
// the caller can apply secondary scoring (decay, confidence, etc).
func scanMemoryRowWithRank(rows *sql.Rows) (*Memory, float64, error) {
	var m Memory
	var source, lastAccessed, expiresAt sql.NullString
	var createdAt string
	var rank float64

	err := rows.Scan(&m.ID, &m.Category, &m.Content, &source, &m.Confidence,
		&lastAccessed, &createdAt, &expiresAt, &rank)
	if err != nil {
		return nil, 0, err
	}

	if source.Valid {
		m.Source = source.String
	}
	if parsed, parseErr := time.Parse(time.RFC3339, createdAt); parseErr == nil {
		m.CreatedAt = parsed
	} else {
		slog.Warn("assistant.scanMemoryRowWithRank: failed to parse created_at",
			"memory_id", m.ID, "value", createdAt, "error", parseErr)
	}
	if lastAccessed.Valid {
		if parsed, parseErr := time.Parse(time.RFC3339, lastAccessed.String); parseErr == nil {
			m.LastAccessed = parsed
		}
	}
	if expiresAt.Valid {
		if parsed, parseErr := time.Parse(time.RFC3339, expiresAt.String); parseErr == nil {
			m.ExpiresAt = parsed
		}
	}
	return &m, rank, nil
}

// ftsOperatorRe matches FTS5 special characters that need escaping.
var ftsOperatorRe = regexp.MustCompile(`[()":^]`)

// sanitizeMemoryFTSQuery escapes FTS5 special characters for safe querying.
// Treats all input as literal search terms.
func sanitizeMemoryFTSQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}

	cleaned := ftsOperatorRe.ReplaceAllString(query, " ")
	words := strings.Fields(cleaned)
	var terms []string
	for _, w := range words {
		w = strings.TrimRight(w, "*")
		upper := strings.ToUpper(w)
		if upper == "AND" || upper == "OR" || upper == "NOT" || upper == "NEAR" {
			continue
		}
		if w == "" {
			continue
		}
		terms = append(terms, `"`+w+`"`)
	}
	if len(terms) == 0 {
		return ""
	}
	return strings.Join(terms, " ")
}
