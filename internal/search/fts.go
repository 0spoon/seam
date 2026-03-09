// Package search provides full-text and semantic search for notes.
package search

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// FTSResult represents a single full-text search result.
type FTSResult struct {
	NoteID  string  `json:"note_id"`
	Title   string  `json:"title"`
	Snippet string  `json:"snippet"`
	Rank    float64 `json:"rank"`
}

// FTSStore implements full-text search queries against a user's SQLite DB.
type FTSStore struct{}

// NewFTSStore creates a new FTSStore.
func NewFTSStore() *FTSStore {
	return &FTSStore{}
}

// Search queries the FTS5 index and returns ranked results.
// The query is sanitized to prevent FTS5 syntax errors.
func (s *FTSStore) Search(ctx context.Context, db *sql.DB, query string, limit, offset int) ([]FTSResult, int, error) {
	sanitized := sanitizeFTSQuery(query)
	if sanitized == "" {
		return nil, 0, nil
	}

	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	// Count total matches.
	var total int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notes_fts WHERE notes_fts MATCH ?`,
		sanitized,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("search.FTSStore.Search: count: %w", err)
	}

	if total == 0 {
		return nil, 0, nil
	}

	// Query with ranking and snippets.
	rows, err := db.QueryContext(ctx,
		`SELECT n.id, n.title,
		        snippet(notes_fts, 1, '<mark>', '</mark>', '...', 32) as snippet,
		        bm25(notes_fts) as rank
		 FROM notes_fts
		 JOIN notes n ON notes_fts.rowid = n.rowid
		 WHERE notes_fts MATCH ?
		 ORDER BY rank
		 LIMIT ? OFFSET ?`,
		sanitized, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("search.FTSStore.Search: query: %w", err)
	}
	defer rows.Close()

	var results []FTSResult
	for rows.Next() {
		var r FTSResult
		if err := rows.Scan(&r.NoteID, &r.Title, &r.Snippet, &r.Rank); err != nil {
			return nil, 0, fmt.Errorf("search.FTSStore.Search: scan: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("search.FTSStore.Search: rows: %w", err)
	}

	return results, total, nil
}

// ftsOperatorRe matches FTS5 operators that should be escaped in user input.
// Note: * is handled separately (allowed at end of words for prefix search).
var ftsOperatorRe = regexp.MustCompile(`[()":^]`)

// sanitizeFTSQuery escapes FTS5 special characters and wraps terms for safe querying.
// Treats all user input as literal search terms by default.
// Supports prefix queries by allowing trailing * on terms.
func sanitizeFTSQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}

	// Remove FTS5 operators (but preserve trailing * for prefix queries).
	cleaned := ftsOperatorRe.ReplaceAllString(query, " ")

	// Remove boolean operators used as standalone words.
	words := strings.Fields(cleaned)
	var terms []string
	for _, w := range words {
		// Strip any * that are not trailing (mid-word wildcards).
		stripped := strings.TrimRight(w, "*")
		hasStar := len(stripped) < len(w)
		w = stripped
		if hasStar && w != "" {
			w = w + "*"
		}

		upper := strings.ToUpper(strings.TrimSuffix(w, "*"))
		if upper == "AND" || upper == "OR" || upper == "NOT" || upper == "NEAR" {
			continue
		}
		if w == "" || w == "*" {
			continue
		}
		terms = append(terms, w)
	}

	if len(terms) == 0 {
		return ""
	}

	// Quote each term to make it literal, allowing trailing * for prefix search.
	var quoted []string
	for _, t := range terms {
		if strings.HasSuffix(t, "*") {
			// Prefix query: quote the base term, append *.
			base := strings.TrimSuffix(t, "*")
			if base != "" {
				quoted = append(quoted, `"`+base+`"*`)
			}
		} else {
			quoted = append(quoted, `"`+t+`"`)
		}
	}

	return strings.Join(quoted, " ")
}
