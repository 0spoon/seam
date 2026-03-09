package graph

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/katata/seam/migrations"
)

// testDB creates an isolated in-memory SQLite database for testing.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := sql.Open("sqlite", name)
	require.NoError(t, err)

	_, err = db.Exec("PRAGMA journal_mode=WAL")
	require.NoError(t, err)
	_, err = db.Exec("PRAGMA foreign_keys=ON")
	require.NoError(t, err)
	_, err = db.Exec(migrations.UserSQL)
	require.NoError(t, err)

	t.Cleanup(func() { db.Close() })
	return db
}

// fakeManager implements userdb.Manager for tests.
type fakeManager struct {
	db       *sql.DB
	notesDir string
}

func (f *fakeManager) Open(_ context.Context, _ string) (*sql.DB, error) {
	return f.db, nil
}
func (f *fakeManager) Close(_ string) error                          { return nil }
func (f *fakeManager) CloseAll() error                               { return nil }
func (f *fakeManager) UserNotesDir(_ string) string                  { return f.notesDir }
func (f *fakeManager) UserDataDir(_ string) string                   { return f.notesDir }
func (f *fakeManager) ListUsers(_ context.Context) ([]string, error) { return nil, nil }
func (f *fakeManager) EnsureUserDirs(_ string) error                 { return nil }

// insertNote inserts a test note into the database.
func insertNote(t *testing.T, db *sql.DB, id, title, projectID string, tags []string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO notes (id, title, project_id, file_path, body, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, '', 'hash', ?, ?)`,
		id, title, nullStr(projectID), id+".md", now, now,
	)
	require.NoError(t, err)

	for _, tag := range tags {
		_, err := db.Exec(`INSERT OR IGNORE INTO tags (name) VALUES (?)`, tag)
		require.NoError(t, err)
		var tagID int64
		err = db.QueryRow(`SELECT id FROM tags WHERE name = ?`, tag).Scan(&tagID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO note_tags (note_id, tag_id) VALUES (?, ?)`, id, tagID)
		require.NoError(t, err)
	}
}

// insertLink inserts a resolved link between two notes.
func insertLink(t *testing.T, db *sql.DB, sourceID, targetID, linkText string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO links (source_note_id, target_note_id, link_text)
		 VALUES (?, ?, ?)`,
		sourceID, targetID, linkText,
	)
	require.NoError(t, err)
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func TestService_GetGraph_Empty(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)

	g, err := svc.GetGraph(context.Background(), "user1", GraphFilter{})
	require.NoError(t, err)
	require.NotNil(t, g)
	require.Empty(t, g.Nodes)
	require.Empty(t, g.Edges)
}

func TestService_GetGraph_NodesAndEdges(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)

	// Create project for notes that reference it.
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO projects (id, name, slug, created_at, updated_at) VALUES ('p1', 'P', 'p', ?, ?)`, now, now)
	require.NoError(t, err)

	insertNote(t, db, "n1", "Note One", "", []string{"go", "api"})
	insertNote(t, db, "n2", "Note Two", "p1", []string{"go"})
	insertNote(t, db, "n3", "Note Three", "p1", nil)
	insertLink(t, db, "n1", "n2", "Note Two")
	insertLink(t, db, "n2", "n3", "Note Three")

	g, err := svc.GetGraph(context.Background(), "user1", GraphFilter{})
	require.NoError(t, err)
	require.Len(t, g.Nodes, 3)
	require.Len(t, g.Edges, 2)

	// Verify link counts.
	nodeMap := make(map[string]Node)
	for _, n := range g.Nodes {
		nodeMap[n.ID] = n
	}
	// n1 -> n2 (1 outbound), n2 -> n3 (1 outbound) + n1 -> n2 (1 inbound)
	require.Equal(t, 1, nodeMap["n1"].LinkCount) // 1 outbound
	require.Equal(t, 2, nodeMap["n2"].LinkCount) // 1 inbound + 1 outbound
	require.Equal(t, 1, nodeMap["n3"].LinkCount) // 1 inbound

	// Verify project name is included.
	require.Equal(t, "", nodeMap["n1"].ProjectName)
	require.Equal(t, "P", nodeMap["n2"].ProjectName)
	require.Equal(t, "P", nodeMap["n3"].ProjectName)
}

func TestService_GetGraph_FilterByProject(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)

	// Insert a project first.
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO projects (id, name, slug, created_at, updated_at)
		 VALUES ('p1', 'Project One', 'project-one', ?, ?)`, now, now)
	require.NoError(t, err)

	insertNote(t, db, "n1", "Inbox Note", "", nil)
	insertNote(t, db, "n2", "Project Note", "p1", nil)
	insertLink(t, db, "n1", "n2", "Project Note")

	g, err := svc.GetGraph(context.Background(), "user1", GraphFilter{ProjectID: "p1"})
	require.NoError(t, err)
	require.Len(t, g.Nodes, 1)
	require.Equal(t, "n2", g.Nodes[0].ID)
	// Edge n1->n2 should be excluded because n1 is not in the node set.
	require.Empty(t, g.Edges)
}

func TestService_GetGraph_FilterByTag(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)

	insertNote(t, db, "n1", "Go Note", "", []string{"go"})
	insertNote(t, db, "n2", "Python Note", "", []string{"python"})
	insertLink(t, db, "n1", "n2", "Python Note")

	g, err := svc.GetGraph(context.Background(), "user1", GraphFilter{Tag: "go"})
	require.NoError(t, err)
	require.Len(t, g.Nodes, 1)
	require.Equal(t, "n1", g.Nodes[0].ID)
	// Edge excluded: n2 not in node set.
	require.Empty(t, g.Edges)
}

func TestService_GetGraph_FilterByDateRange(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)

	// Insert notes with specific creation dates.
	_, err := db.Exec(
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES ('n1', 'Old Note', 'n1.md', '', 'h1', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')`)
	require.NoError(t, err)
	_, err = db.Exec(
		`INSERT INTO notes (id, title, file_path, body, content_hash, created_at, updated_at)
		 VALUES ('n2', 'New Note', 'n2.md', '', 'h2', '2026-03-01T00:00:00Z', '2026-03-01T00:00:00Z')`)
	require.NoError(t, err)

	since, _ := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	g, err := svc.GetGraph(context.Background(), "user1", GraphFilter{Since: since})
	require.NoError(t, err)
	require.Len(t, g.Nodes, 1)
	require.Equal(t, "n2", g.Nodes[0].ID)
}

func TestService_GetGraph_Limit(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)

	for i := 0; i < 10; i++ {
		insertNote(t, db, fmt.Sprintf("n%d", i), fmt.Sprintf("Note %d", i), "", nil)
	}

	g, err := svc.GetGraph(context.Background(), "user1", GraphFilter{Limit: 3})
	require.NoError(t, err)
	require.Len(t, g.Nodes, 3)
}

func TestService_GetGraph_DefaultLimit(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)

	// With limit 0, should default to 500.
	g, err := svc.GetGraph(context.Background(), "user1", GraphFilter{Limit: 0})
	require.NoError(t, err)
	require.NotNil(t, g)
}

func TestService_GetGraph_TagsIncluded(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)

	insertNote(t, db, "n1", "Tagged Note", "", []string{"go", "api", "rest"})

	g, err := svc.GetGraph(context.Background(), "user1", GraphFilter{})
	require.NoError(t, err)
	require.Len(t, g.Nodes, 1)
	require.Equal(t, []string{"api", "go", "rest"}, g.Nodes[0].Tags) // sorted
}

func TestService_GetTwoHopBacklinks(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)

	insertNote(t, db, "n1", "Note A", "", nil)
	insertNote(t, db, "n2", "Note B", "", nil)
	insertNote(t, db, "n3", "Note C", "", nil)
	insertNote(t, db, "n4", "Note D", "", nil) // direct backlink to n3

	// n1 -> n2 -> n3 (n1 is two hops from n3)
	insertLink(t, db, "n1", "n2", "Note B")
	insertLink(t, db, "n2", "n3", "Note C")
	// n4 -> n3 (direct backlink, should be excluded from two-hop results)
	insertLink(t, db, "n4", "n3", "Note C")

	nodes, err := svc.GetTwoHopBacklinks(context.Background(), "user1", "n3")
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	require.Equal(t, "n1", nodes[0].ID)
	require.Equal(t, "Note A", nodes[0].Title)
	// The intermediate note should be n2 (n1 -> n2 -> n3).
	require.Equal(t, "n2", nodes[0].ViaID)
	require.Equal(t, "Note B", nodes[0].ViaTitle)
}

func TestService_GetTwoHopBacklinks_Empty(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)

	insertNote(t, db, "n1", "Note A", "", nil)

	nodes, err := svc.GetTwoHopBacklinks(context.Background(), "user1", "n1")
	require.NoError(t, err)
	require.Empty(t, nodes)
}

func TestService_GetOrphanNotes(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)

	insertNote(t, db, "n1", "Connected", "", nil)
	insertNote(t, db, "n2", "Also Connected", "", nil)
	insertNote(t, db, "n3", "Orphan", "", nil)
	insertLink(t, db, "n1", "n2", "Also Connected")

	nodes, err := svc.GetOrphanNotes(context.Background(), "user1")
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	require.Equal(t, "n3", nodes[0].ID)
}

func TestService_GetOrphanNotes_AllConnected(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)

	insertNote(t, db, "n1", "Note A", "", nil)
	insertNote(t, db, "n2", "Note B", "", nil)
	insertLink(t, db, "n1", "n2", "Note B")

	nodes, err := svc.GetOrphanNotes(context.Background(), "user1")
	require.NoError(t, err)
	require.Empty(t, nodes)
}

func TestService_GetOrphanNotes_NoNotes(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)

	nodes, err := svc.GetOrphanNotes(context.Background(), "user1")
	require.NoError(t, err)
	require.Empty(t, nodes)
}
