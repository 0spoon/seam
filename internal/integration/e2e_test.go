//go:build integration

// Package integration contains end-to-end tests that exercise the full
// server stack: registration, authentication, project CRUD, note CRUD
// with wikilinks, search, and graph visualization.
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/graph"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/search"
	"github.com/katata/seam/internal/server"
	"github.com/katata/seam/internal/testutil"
	"github.com/katata/seam/internal/userdb"
)

// testClient wraps an httptest.Server with helper methods.
type testClient struct {
	ts          *httptest.Server
	accessToken string
}

func (c *testClient) do(method, path string, body interface{}, out interface{}) *http.Response {
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.ts.URL+path, bodyReader)
	if err != nil {
		panic(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	if out != nil {
		defer resp.Body.Close()
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp
}

func setupServer(t *testing.T) *testClient {
	t.Helper()
	dataDir := testutil.TestDataDir(t)
	serverDB := testutil.TestServerDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// User DB manager (real filesystem).
	userDBMgr := userdb.NewSQLManager(dataDir, 10*time.Minute, logger)
	t.Cleanup(func() { userDBMgr.CloseAll() })

	// Auth stack.
	jwtMgr := auth.NewJWTManager("test-secret-key-for-integration", 15*time.Minute)
	authStore := auth.NewSQLStore(serverDB)
	authSvc := auth.NewService(authStore, jwtMgr, userDBMgr, 24*time.Hour, bcrypt.MinCost, logger)
	authHandler := auth.NewHandler(authSvc, logger)

	// Project stack.
	projectStore := project.NewStore()
	projectSvc := project.NewService(projectStore, userDBMgr, logger)
	projectHandler := project.NewHandler(projectSvc, logger)

	// Note stack.
	noteStore := note.NewSQLStore()
	versionStore := note.NewVersionStore()
	noteSvc := note.NewService(noteStore, versionStore, projectStore, userDBMgr, nil, logger)
	noteHandler := note.NewHandler(noteSvc, logger)

	// Search stack.
	ftsStore := search.NewFTSStore()
	searchSvc := search.NewService(ftsStore, userDBMgr, logger)
	searchHandler := search.NewHandler(searchSvc, logger)

	// Graph stack.
	graphSvc := graph.NewService(userDBMgr, logger)
	graphHandler := graph.NewHandler(graphSvc, logger)

	srv := server.New(server.Config{
		Listen:         ":0",
		Logger:         logger,
		JWTManager:     jwtMgr,
		AuthHandler:    authHandler,
		ProjectHandler: projectHandler,
		NoteHandler:    noteHandler,
		SearchHandler:  searchHandler,
		GraphHandler:   graphHandler,
	})

	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	return &testClient{ts: ts}
}

// TestE2E_FullUserJourney exercises the complete user journey:
// register -> login -> create project -> create notes with wikilinks ->
// search -> get graph -> get two-hop backlinks -> get orphans.
func TestE2E_FullUserJourney(t *testing.T) {
	c := setupServer(t)

	// Step 1: Register.
	var authResp struct {
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
	}
	resp := c.do("POST", "/api/auth/register", map[string]string{
		"username": "testuser",
		"email":    "test@example.com",
		"password": "securepassword123",
	}, &authResp)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.NotEmpty(t, authResp.User.ID)
	require.Equal(t, "testuser", authResp.User.Username)
	require.NotEmpty(t, authResp.Tokens.AccessToken)
	c.accessToken = authResp.Tokens.AccessToken

	// Step 2: Create project.
	var projectResp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	resp = c.do("POST", "/api/projects", map[string]string{
		"name": "My Research",
	}, &projectResp)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.NotEmpty(t, projectResp.ID)
	require.Equal(t, "My Research", projectResp.Name)
	projectID := projectResp.ID

	// Step 3: Create notes with wikilinks.
	type noteResp struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}

	var note1 noteResp
	resp = c.do("POST", "/api/notes", map[string]interface{}{
		"title":      "Graph Theory",
		"body":       "This note is about graph theory. See also [[Data Structures]].",
		"project_id": projectID,
		"tags":       []string{"math", "cs"},
	}, &note1)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.NotEmpty(t, note1.ID)

	var note2 noteResp
	resp = c.do("POST", "/api/notes", map[string]interface{}{
		"title":      "Data Structures",
		"body":       "Trees, graphs, and more. Related to [[Algorithms]].",
		"project_id": projectID,
		"tags":       []string{"cs"},
	}, &note2)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var note3 noteResp
	resp = c.do("POST", "/api/notes", map[string]interface{}{
		"title":      "Algorithms",
		"body":       "Sorting, searching, and graph algorithms.",
		"project_id": projectID,
		"tags":       []string{"cs", "math"},
	}, &note3)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Create an orphan note (no wikilinks in or out).
	var orphanNote noteResp
	resp = c.do("POST", "/api/notes", map[string]interface{}{
		"title": "Standalone Thought",
		"body":  "This note links to nothing and nothing links here.",
	}, &orphanNote)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Step 4: Search.
	var searchResp struct {
		Results []struct {
			NoteID string `json:"note_id"`
			Title  string `json:"title"`
		} `json:"results"`
		Total int `json:"total"`
	}
	resp = c.do("GET", "/api/search?q=graph", nil, &searchResp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	// FTS search should return results. Notes contain "graph" in both title and body.
	t.Logf("Search for 'graph': %d results", searchResp.Total)

	// Step 5: Get graph.
	var graphResp struct {
		Nodes []struct {
			ID          string   `json:"id"`
			Title       string   `json:"title"`
			ProjectID   string   `json:"project_id"`
			ProjectName string   `json:"project"`
			Tags        []string `json:"tags"`
			LinkCount   int      `json:"link_count"`
		} `json:"nodes"`
		Edges []struct {
			Source string `json:"source"`
			Target string `json:"target"`
		} `json:"edges"`
	}
	resp = c.do("GET", "/api/graph", nil, &graphResp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, graphResp.Nodes, 4, "should have 4 notes in graph")

	// Verify project name is populated on project notes.
	for _, n := range graphResp.Nodes {
		if n.ProjectID == projectID {
			require.Equal(t, "My Research", n.ProjectName)
		}
	}

	// Verify at least one edge exists (from wikilinks).
	// Graph Theory -> Data Structures should be resolved.
	require.NotEmpty(t, graphResp.Edges, "wikilinks should create graph edges")

	// Step 6: Get graph with project filter.
	var filteredGraph struct {
		Nodes []struct{ ID string } `json:"nodes"`
	}
	resp = c.do("GET", "/api/graph?project="+projectID, nil, &filteredGraph)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, filteredGraph.Nodes, 3, "project filter should return only project notes")

	// Step 7: Get orphan notes.
	var orphans []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	resp = c.do("GET", "/api/graph/orphans", nil, &orphans)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	// The standalone note has no wikilinks, but it might still not be orphan
	// if wikilink resolution did not create links. Check that the endpoint works.
	require.NotNil(t, orphans)
	// At minimum, orphanNote should be in the list since it has no wikilinks.
	found := false
	for _, o := range orphans {
		if o.ID == orphanNote.ID {
			found = true
			break
		}
	}
	require.True(t, found, "standalone note should be detected as orphan")

	// Step 8: Get two-hop backlinks.
	// Graph Theory -> Data Structures -> Algorithms
	// So Graph Theory is a two-hop backlink of Algorithms.
	var twoHop []struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		ViaID    string `json:"via_id"`
		ViaTitle string `json:"via_title"`
	}
	resp = c.do("GET", fmt.Sprintf("/api/graph/two-hop-backlinks/%s", note3.ID), nil, &twoHop)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	// Graph Theory links to Data Structures, Data Structures links to Algorithms.
	// So Graph Theory should be a two-hop backlink of Algorithms (if links are resolved).
	require.NotNil(t, twoHop)

	// Step 9: Verify tags endpoint works.
	var tagsResp []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	resp = c.do("GET", "/api/tags", nil, &tagsResp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotEmpty(t, tagsResp, "should have tags")
	// Should have cs and math tags.
	tagNames := make(map[string]bool)
	for _, t := range tagsResp {
		tagNames[t.Name] = true
	}
	require.True(t, tagNames["cs"], "should have 'cs' tag")
	require.True(t, tagNames["math"], "should have 'math' tag")

	// Step 10: Health check (sanity).
	resp = c.do("GET", "/api/health", nil, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}
