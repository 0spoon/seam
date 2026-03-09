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
