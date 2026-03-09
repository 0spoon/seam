package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/reqctx"
)

// setupTestHandler creates a handler with a test database.
func setupTestHandler(t *testing.T) (*Handler, *http.Request) {
	t.Helper()
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)
	handler := NewHandler(svc, nil)

	// Create a test request with user context.
	req := httptest.NewRequest("GET", "/api/graph", nil)
	ctx := reqctx.WithUserID(req.Context(), "user1")
	req = req.WithContext(ctx)
	return handler, req
}

func TestHandler_GetGraph_Empty(t *testing.T) {
	handler, req := setupTestHandler(t)
	w := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Get("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		handler.getGraph(w, req)
	})
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var g Graph
	err := json.NewDecoder(w.Body).Decode(&g)
	require.NoError(t, err)
	require.NotNil(t, g.Nodes)
	require.NotNil(t, g.Edges)
	require.Empty(t, g.Nodes)
	require.Empty(t, g.Edges)
}

func TestHandler_GetGraph_WithData(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)
	handler := NewHandler(svc, nil)

	insertNote(t, db, "n1", "First Note", "", []string{"go"})
	insertNote(t, db, "n2", "Second Note", "", nil)
	insertLink(t, db, "n1", "n2", "Second Note")

	req := httptest.NewRequest("GET", "/api/graph", nil)
	ctx := reqctx.WithUserID(req.Context(), "user1")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.getGraph(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var g Graph
	err := json.NewDecoder(w.Body).Decode(&g)
	require.NoError(t, err)
	require.Len(t, g.Nodes, 2)
	require.Len(t, g.Edges, 1)
	require.Equal(t, "n1", g.Edges[0].Source)
	require.Equal(t, "n2", g.Edges[0].Target)
}

func TestHandler_GetGraph_WithFilters(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)
	handler := NewHandler(svc, nil)

	insertNote(t, db, "n1", "Go Note", "", []string{"go"})
	insertNote(t, db, "n2", "Python Note", "", []string{"python"})

	req := httptest.NewRequest("GET", "/api/graph?tag=go&limit=10", nil)
	ctx := reqctx.WithUserID(req.Context(), "user1")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.getGraph(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var g Graph
	err := json.NewDecoder(w.Body).Decode(&g)
	require.NoError(t, err)
	require.Len(t, g.Nodes, 1)
	require.Equal(t, "n1", g.Nodes[0].ID)
}

func TestHandler_GetGraph_Unauthorized(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)
	handler := NewHandler(svc, nil)

	// No user context.
	req := httptest.NewRequest("GET", "/api/graph", nil)
	w := httptest.NewRecorder()

	handler.getGraph(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_GetTwoHopBacklinks(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)
	handler := NewHandler(svc, nil)

	insertNote(t, db, "n1", "A", "", nil)
	insertNote(t, db, "n2", "B", "", nil)
	insertNote(t, db, "n3", "C", "", nil)
	insertLink(t, db, "n1", "n2", "B")
	insertLink(t, db, "n2", "n3", "C")

	req := httptest.NewRequest("GET", "/api/graph/two-hop-backlinks/n3", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "n3")
	ctx := context.WithValue(reqctx.WithUserID(req.Context(), "user1"), chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.getTwoHopBacklinks(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var nodes []TwoHopNode
	err := json.NewDecoder(w.Body).Decode(&nodes)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	require.Equal(t, "n1", nodes[0].ID)
	require.Equal(t, "n2", nodes[0].ViaID)
}

func TestHandler_GetOrphans(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)
	handler := NewHandler(svc, nil)

	insertNote(t, db, "n1", "Connected", "", nil)
	insertNote(t, db, "n2", "Also Connected", "", nil)
	insertNote(t, db, "n3", "Orphan", "", nil)
	insertLink(t, db, "n1", "n2", "Also Connected")

	req := httptest.NewRequest("GET", "/api/graph/orphans", nil)
	ctx := reqctx.WithUserID(req.Context(), "user1")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.getOrphans(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var nodes []Node
	err := json.NewDecoder(w.Body).Decode(&nodes)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	require.Equal(t, "n3", nodes[0].ID)
}

func TestHandler_GetGraph_LimitCap(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)
	handler := NewHandler(svc, nil)

	for i := 0; i < 5; i++ {
		insertNote(t, db, fmt.Sprintf("n%d", i), fmt.Sprintf("Note %d", i), "", nil)
	}

	// Request limit=99999 -- should be capped at 500.
	req := httptest.NewRequest("GET", "/api/graph?limit=99999", nil)
	ctx := reqctx.WithUserID(req.Context(), "user1")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.getGraph(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var g Graph
	err := json.NewDecoder(w.Body).Decode(&g)
	require.NoError(t, err)
	// All 5 notes returned (under the 500 cap).
	require.Len(t, g.Nodes, 5)
}

func TestHandler_GetOrphans_Unauthorized(t *testing.T) {
	db := testDB(t)
	svc := NewService(&fakeManager{db: db}, nil)
	handler := NewHandler(svc, nil)

	req := httptest.NewRequest("GET", "/api/graph/orphans", nil)
	w := httptest.NewRecorder()

	handler.getOrphans(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}
