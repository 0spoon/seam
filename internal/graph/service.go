package graph

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/katata/seam/internal/userdb"
)

// Service provides knowledge graph data from the per-user database.
type Service struct {
	dbManager userdb.Manager
	logger    *slog.Logger
}

// NewService creates a new graph Service.
func NewService(dbManager userdb.Manager, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		dbManager: dbManager,
		logger:    logger,
	}
}

// GetGraph returns the knowledge graph for a user, filtered by the given criteria.
func (s *Service) GetGraph(ctx context.Context, userID string, filter GraphFilter) (*Graph, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("graph.Service.GetGraph: open db: %w", err)
	}

	if filter.Limit <= 0 {
		filter.Limit = 500
	}

	nodes, err := s.queryNodes(ctx, db, filter)
	if err != nil {
		return nil, fmt.Errorf("graph.Service.GetGraph: %w", err)
	}

	// Build a set of node IDs for filtering edges.
	nodeIDs := make(map[string]bool, len(nodes))
	for i := range nodes {
		nodeIDs[nodes[i].ID] = true
	}

	edges, err := s.queryEdges(ctx, db, nodeIDs)
	if err != nil {
		return nil, fmt.Errorf("graph.Service.GetGraph: %w", err)
	}

	// Count links per node (inbound + outbound) for sizing.
	linkCounts := make(map[string]int, len(nodes))
	for _, e := range edges {
		linkCounts[e.Source]++
		linkCounts[e.Target]++
	}
	for i := range nodes {
		nodes[i].LinkCount = linkCounts[nodes[i].ID]
	}

	return &Graph{Nodes: nodes, Edges: edges}, nil
}

// queryNodes retrieves notes matching the filter as graph nodes.
// Uses a LEFT JOIN on projects to include the human-readable project name.
func (s *Service) queryNodes(ctx context.Context, db *sql.DB, filter GraphFilter) ([]Node, error) {
	var where []string
	var args []interface{}

	if filter.ProjectID != "" {
		where = append(where, "n.project_id = ?")
		args = append(args, filter.ProjectID)
	}
	if filter.Tag != "" {
		where = append(where, "EXISTS (SELECT 1 FROM note_tags nt JOIN tags t ON t.id = nt.tag_id WHERE nt.note_id = n.id AND t.name = ?)")
		args = append(args, filter.Tag)
	}
	if !filter.Since.IsZero() {
		where = append(where, "n.created_at >= ?")
		args = append(args, filter.Since.Format(time.RFC3339))
	}
	if !filter.Until.IsZero() {
		where = append(where, "n.created_at <= ?")
		args = append(args, filter.Until.Format(time.RFC3339))
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = " WHERE " + strings.Join(where, " AND ")
	}

	query := fmt.Sprintf(
		`SELECT n.id, n.title, n.project_id, p.name, n.created_at
		 FROM notes n
		 LEFT JOIN projects p ON p.id = n.project_id
		 %s
		 ORDER BY n.updated_at DESC
		 LIMIT ?`,
		whereClause,
	)
	args = append(args, filter.Limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("queryNodes: %w", err)
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var projectID sql.NullString
		var projectName sql.NullString
		var createdAt string

		if err := rows.Scan(&n.ID, &n.Title, &projectID, &projectName, &createdAt); err != nil {
			return nil, fmt.Errorf("queryNodes: scan: %w", err)
		}
		n.ProjectID = projectID.String
		n.ProjectName = projectName.String
		n.CreatedAt = createdAt
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("queryNodes: rows: %w", err)
	}

	// Batch-load tags for all nodes in a single query.
	if err := s.loadAllNodeTags(ctx, db, nodes); err != nil {
		return nil, fmt.Errorf("queryNodes: %w", err)
	}

	return nodes, nil
}

// loadAllNodeTags loads tags for all nodes in a single query (avoids N+1).
func (s *Service) loadAllNodeTags(ctx context.Context, db *sql.DB, nodes []Node) error {
	if len(nodes) == 0 {
		return nil
	}

	// Build the IN clause.
	placeholders := make([]string, len(nodes))
	args := make([]interface{}, len(nodes))
	nodeIndex := make(map[string]int, len(nodes))
	for i, n := range nodes {
		placeholders[i] = "?"
		args[i] = n.ID
		nodeIndex[n.ID] = i
	}

	query := fmt.Sprintf(
		`SELECT nt.note_id, t.name
		 FROM note_tags nt
		 JOIN tags t ON t.id = nt.tag_id
		 WHERE nt.note_id IN (%s)
		 ORDER BY t.name`,
		strings.Join(placeholders, ","),
	)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("loadAllNodeTags: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var noteID, tagName string
		if err := rows.Scan(&noteID, &tagName); err != nil {
			return fmt.Errorf("loadAllNodeTags: scan: %w", err)
		}
		if idx, ok := nodeIndex[noteID]; ok {
			nodes[idx].Tags = append(nodes[idx].Tags, tagName)
		}
	}
	return rows.Err()
}

// queryEdges returns all resolved links between the given set of node IDs.
// Uses SQL-level filtering to avoid scanning the entire links table.
func (s *Service) queryEdges(ctx context.Context, db *sql.DB, nodeIDs map[string]bool) ([]Edge, error) {
	if len(nodeIDs) == 0 {
		return nil, nil
	}

	// Build IN clause for SQL-level filtering.
	placeholders := make([]string, 0, len(nodeIDs))
	args := make([]interface{}, 0, len(nodeIDs))
	for id := range nodeIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	inClause := strings.Join(placeholders, ",")
	// Duplicate args for both source and target IN clauses.
	allArgs := make([]interface{}, 0, len(args)*2)
	allArgs = append(allArgs, args...)
	allArgs = append(allArgs, args...)

	query := fmt.Sprintf(
		`SELECT source_note_id, target_note_id FROM links
		 WHERE target_note_id IS NOT NULL
		   AND source_note_id IN (%s)
		   AND target_note_id IN (%s)`,
		inClause, inClause,
	)

	rows, err := db.QueryContext(ctx, query, allArgs...)
	if err != nil {
		return nil, fmt.Errorf("queryEdges: %w", err)
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.Source, &e.Target); err != nil {
			return nil, fmt.Errorf("queryEdges: scan: %w", err)
		}
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("queryEdges: rows: %w", err)
	}

	return edges, nil
}

// GetTwoHopBacklinks returns notes that are two hops away from the given note ID,
// including the intermediate connecting note.
func (s *Service) GetTwoHopBacklinks(ctx context.Context, userID, noteID string) ([]TwoHopNode, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("graph.Service.GetTwoHopBacklinks: open db: %w", err)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT DISTINCT n.id, n.title, via.id, via.title
		 FROM notes n
		 JOIN links l1 ON l1.source_note_id = n.id AND l1.target_note_id IS NOT NULL
		 JOIN notes via ON via.id = l1.target_note_id
		 JOIN links l2 ON l2.source_note_id = via.id AND l2.target_note_id IS NOT NULL
		 WHERE l2.target_note_id = ?
		   AND n.id != ?
		   AND n.id NOT IN (
		       SELECT source_note_id FROM links
		       WHERE target_note_id = ? AND target_note_id IS NOT NULL
		   )
		 ORDER BY n.title`,
		noteID, noteID, noteID,
	)
	if err != nil {
		return nil, fmt.Errorf("graph.Service.GetTwoHopBacklinks: %w", err)
	}
	defer rows.Close()

	var nodes []TwoHopNode
	for rows.Next() {
		var n TwoHopNode
		if err := rows.Scan(&n.ID, &n.Title, &n.ViaID, &n.ViaTitle); err != nil {
			return nil, fmt.Errorf("graph.Service.GetTwoHopBacklinks: scan: %w", err)
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// GetOrphanNotes returns notes with no incoming or outgoing links.
// C-26: Now includes project names (LEFT JOIN) and batch-loads tags.
// C-28: Limits results to 500 to prevent unbounded queries.
func (s *Service) GetOrphanNotes(ctx context.Context, userID string) ([]Node, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("graph.Service.GetOrphanNotes: open db: %w", err)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT n.id, n.title, n.project_id, p.name, n.created_at
		 FROM notes n
		 LEFT JOIN projects p ON p.id = n.project_id
		 WHERE n.id NOT IN (
		     SELECT DISTINCT source_note_id FROM links WHERE target_note_id IS NOT NULL
		     UNION
		     SELECT DISTINCT target_note_id FROM links WHERE target_note_id IS NOT NULL
		 )
		 ORDER BY n.title
		 LIMIT 500`)
	if err != nil {
		return nil, fmt.Errorf("graph.Service.GetOrphanNotes: %w", err)
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var projectID sql.NullString
		var projectName sql.NullString
		var createdAt string
		if err := rows.Scan(&n.ID, &n.Title, &projectID, &projectName, &createdAt); err != nil {
			return nil, fmt.Errorf("graph.Service.GetOrphanNotes: scan: %w", err)
		}
		n.ProjectID = projectID.String
		n.ProjectName = projectName.String
		n.CreatedAt = createdAt
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("graph.Service.GetOrphanNotes: rows: %w", err)
	}

	// Batch-load tags for all orphan nodes.
	if err := s.loadAllNodeTags(ctx, db, nodes); err != nil {
		return nil, fmt.Errorf("graph.Service.GetOrphanNotes: %w", err)
	}

	return nodes, nil
}
