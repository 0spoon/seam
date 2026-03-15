package search

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/katata/seam/internal/userdb"
)

// Service coordinates search operations.
type Service struct {
	ftsStore   *FTSStore
	semantic   *SemanticSearcher // nil if semantic search is not configured
	semanticMu sync.RWMutex
	dbManager  userdb.Manager
	logger     *slog.Logger
}

// NewService creates a new search Service.
func NewService(ftsStore *FTSStore, dbManager userdb.Manager, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		ftsStore:  ftsStore,
		dbManager: dbManager,
		logger:    logger,
	}
}

// SetSemanticSearcher enables semantic search by setting the searcher.
func (s *Service) SetSemanticSearcher(searcher *SemanticSearcher) {
	s.semanticMu.Lock()
	defer s.semanticMu.Unlock()
	s.semantic = searcher
}

// SearchFTS performs a full-text search for the given user.
func (s *Service) SearchFTS(ctx context.Context, userID, query string, limit, offset int) ([]FTSResult, int, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("search.Service.SearchFTS: %w", err)
	}
	return s.ftsStore.Search(ctx, db, query, limit, offset)
}

// SearchSemantic performs a semantic search for the given user.
func (s *Service) SearchSemantic(ctx context.Context, userID, query string, limit int) ([]SemanticResult, error) {
	s.semanticMu.RLock()
	sem := s.semantic
	s.semanticMu.RUnlock()
	if sem == nil {
		return nil, fmt.Errorf("search.Service.SearchSemantic: semantic search not configured")
	}
	return sem.Search(ctx, userID, query, limit)
}

// SearchFTSScoped performs full-text search with project-based scope filtering.
func (s *Service) SearchFTSScoped(ctx context.Context, userID, query string, limit, offset int, includeProjectID, excludeProjectID string) ([]FTSResult, int, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("search.Service.SearchFTSScoped: %w", err)
	}
	return s.ftsStore.SearchScoped(ctx, db, query, limit, offset, includeProjectID, excludeProjectID)
}

// SearchSemanticScoped performs semantic search with a ChromaDB metadata filter.
func (s *Service) SearchSemanticScoped(ctx context.Context, userID, query string, limit int, where map[string]interface{}) ([]SemanticResult, error) {
	s.semanticMu.RLock()
	sem := s.semantic
	s.semanticMu.RUnlock()
	if sem == nil {
		return nil, fmt.Errorf("search.Service.SearchSemanticScoped: semantic search not configured")
	}
	return sem.SearchScoped(ctx, userID, query, limit, where)
}

// SearchFTSWithRecency performs full-text search with recency-adjusted ranking.
// The recencyBias parameter (0.0-1.0) controls how much recency affects ranking.
func (s *Service) SearchFTSWithRecency(ctx context.Context, userID, query string, limit, offset int, recencyBias float64) ([]FTSResult, int, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("search.Service.SearchFTSWithRecency: %w", err)
	}
	return s.ftsStore.SearchWithRecency(ctx, db, query, limit, offset, recencyBias)
}

// SearchFTSScopedWithRecency performs scoped FTS with recency-adjusted ranking.
func (s *Service) SearchFTSScopedWithRecency(ctx context.Context, userID, query string, limit, offset int, includeProjectID, excludeProjectID string, recencyBias float64) ([]FTSResult, int, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("search.Service.SearchFTSScopedWithRecency: %w", err)
	}
	return s.ftsStore.SearchScopedWithRecency(ctx, db, query, limit, offset, includeProjectID, excludeProjectID, recencyBias)
}

// SearchSemanticWithRecency performs semantic search with recency-weighted scoring.
// The recencyBias parameter (0.0-1.0) controls how much recency boosts scores.
func (s *Service) SearchSemanticWithRecency(ctx context.Context, userID, query string, limit int, recencyBias float64) ([]SemanticResult, error) {
	s.semanticMu.RLock()
	sem := s.semantic
	s.semanticMu.RUnlock()
	if sem == nil {
		return nil, fmt.Errorf("search.Service.SearchSemanticWithRecency: semantic search not configured")
	}
	return sem.SearchWithRecency(ctx, userID, query, limit, recencyBias)
}

// SearchSemanticScopedWithRecency performs scoped semantic search with recency-weighted scoring.
// The recencyBias parameter (0.0-1.0) controls how much recency boosts scores.
func (s *Service) SearchSemanticScopedWithRecency(ctx context.Context, userID, query string, limit int, where map[string]interface{}, recencyBias float64) ([]SemanticResult, error) {
	s.semanticMu.RLock()
	sem := s.semantic
	s.semanticMu.RUnlock()
	if sem == nil {
		return nil, fmt.Errorf("search.Service.SearchSemanticScopedWithRecency: semantic search not configured")
	}
	return sem.SearchScopedWithRecency(ctx, userID, query, limit, where, recencyBias)
}
