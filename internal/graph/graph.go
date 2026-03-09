// Package graph provides the knowledge graph data endpoint.
// It queries notes and links from the per-user SQLite database
// and returns a graph representation of nodes (notes) and edges (links).
package graph

import (
	"time"
)

// Node represents a note in the knowledge graph.
type Node struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	ProjectID   string   `json:"project_id,omitempty"`
	ProjectName string   `json:"project,omitempty"` // human-readable project name
	Tags        []string `json:"tags"`
	CreatedAt   string   `json:"created_at"`
	LinkCount   int      `json:"link_count"` // total inbound + outbound links (for sizing)
}

// Edge represents a link between two notes.
type Edge struct {
	Source string `json:"source"` // source note ID
	Target string `json:"target"` // target note ID
}

// Graph is the full graph data returned by the API.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// TwoHopNode represents a note that is two hops away, with the connecting note.
type TwoHopNode struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	ViaID    string `json:"via_id"`    // intermediate note ID
	ViaTitle string `json:"via_title"` // intermediate note title
}

// GraphFilter controls which notes and links are included in the graph.
type GraphFilter struct {
	ProjectID string
	Tag       string
	Since     time.Time
	Until     time.Time
	Limit     int // default 500
}
