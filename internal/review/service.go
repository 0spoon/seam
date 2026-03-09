package review

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/katata/seam/internal/graph"
	"github.com/katata/seam/internal/userdb"
)

// Service aggregates review queue items from multiple sources.
type Service struct {
	dbManager    userdb.Manager
	graphService *graph.Service
	logger       *slog.Logger
}

// NewService creates a new review Service.
func NewService(
	dbManager userdb.Manager,
	graphService *graph.Service,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		dbManager:    dbManager,
		graphService: graphService,
		logger:       logger,
	}
}

// GetQueue returns the review queue for a user, limited to the given count.
// Items are returned without AI suggestions; those are fetched separately
// via the suggest-tags and suggest-project endpoints.
func (s *Service) GetQueue(ctx context.Context, userID string, limit int) ([]ReviewItem, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var items []ReviewItem

	// 1. Orphan notes (no links).
	orphans, err := s.getOrphanItems(ctx, userID)
	if err != nil {
		s.logger.Warn("review.Service.GetQueue: orphans failed", "error", err)
		// Continue with other sources.
	} else {
		items = append(items, orphans...)
	}

	// 2. Untagged notes.
	untagged, err := s.getUntaggedItems(ctx, userID)
	if err != nil {
		s.logger.Warn("review.Service.GetQueue: untagged failed", "error", err)
	} else {
		items = append(items, untagged...)
	}

	// 3. Inbox notes (no project).
	inbox, err := s.getInboxItems(ctx, userID)
	if err != nil {
		s.logger.Warn("review.Service.GetQueue: inbox failed", "error", err)
	} else {
		items = append(items, inbox...)
	}

	// Deduplicate: a note may appear in multiple categories. Keep the first
	// occurrence (priority order: orphan > untagged > inbox).
	seen := make(map[string]bool, len(items))
	var deduped []ReviewItem
	for _, item := range items {
		if seen[item.NoteID] {
			continue
		}
		seen[item.NoteID] = true
		deduped = append(deduped, item)
	}

	// Apply limit.
	if len(deduped) > limit {
		deduped = deduped[:limit]
	}

	if deduped == nil {
		deduped = []ReviewItem{}
	}

	return deduped, nil
}

// getOrphanItems returns notes with no links as review items.
func (s *Service) getOrphanItems(ctx context.Context, userID string) ([]ReviewItem, error) {
	if s.graphService == nil {
		return nil, nil
	}

	nodes, err := s.graphService.GetOrphanNotes(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("review.Service.getOrphanItems: %w", err)
	}

	items := make([]ReviewItem, 0, len(nodes))
	for _, n := range nodes {
		items = append(items, ReviewItem{
			Type:        "orphan",
			NoteID:      n.ID,
			NoteTitle:   n.Title,
			NoteSnippet: "", // snippets are loaded separately if needed
			Suggestions: []Suggestion{},
			CreatedAt:   n.CreatedAt,
		})
	}

	return items, nil
}

// getUntaggedItems returns notes with zero tags.
func (s *Service) getUntaggedItems(ctx context.Context, userID string) ([]ReviewItem, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("review.Service.getUntaggedItems: open db: %w", err)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT n.id, n.title, SUBSTR(n.body, 1, 150), n.created_at
		 FROM notes n
		 WHERE NOT EXISTS (
		     SELECT 1 FROM note_tags nt WHERE nt.note_id = n.id
		 )
		 ORDER BY n.updated_at DESC
		 LIMIT 100`)
	if err != nil {
		return nil, fmt.Errorf("review.Service.getUntaggedItems: %w", err)
	}
	defer rows.Close()

	return scanReviewItems(rows, "untagged")
}

// getInboxItems returns notes with no project assignment.
func (s *Service) getInboxItems(ctx context.Context, userID string) ([]ReviewItem, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("review.Service.getInboxItems: open db: %w", err)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT n.id, n.title, SUBSTR(n.body, 1, 150), n.created_at
		 FROM notes n
		 WHERE n.project_id IS NULL
		 ORDER BY n.updated_at DESC
		 LIMIT 100`)
	if err != nil {
		return nil, fmt.Errorf("review.Service.getInboxItems: %w", err)
	}
	defer rows.Close()

	return scanReviewItems(rows, "inbox")
}

// scanReviewItems scans rows into ReviewItem slices with the given type.
func scanReviewItems(rows *sql.Rows, itemType string) ([]ReviewItem, error) {
	var items []ReviewItem
	for rows.Next() {
		var item ReviewItem
		var snippet sql.NullString
		if err := rows.Scan(&item.NoteID, &item.NoteTitle, &snippet, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanReviewItems: %w", err)
		}
		item.Type = itemType
		item.NoteSnippet = snippet.String
		item.Suggestions = []Suggestion{}
		items = append(items, item)
	}
	return items, rows.Err()
}
