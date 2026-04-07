//go:build performance

// Package integration contains performance tests for the graph endpoint
// with large numbers of notes.
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
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

func setupPerfServer(t *testing.T) (*httptest.Server, *auth.JWTManager, userdb.Manager) {
	t.Helper()
	dataDir := testutil.TestDataDir(t)
	db := testutil.TestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	userDBMgr := userdb.NewSQLManager(dataDir, logger)
	t.Cleanup(func() { userDBMgr.CloseAll() })

	jwtMgr := auth.NewJWTManager("perf-test-secret", 15*time.Minute)
	authStore := auth.NewSQLStore(db)
	authSvc := auth.NewService(authStore, jwtMgr, userDBMgr, 24*time.Hour, bcrypt.MinCost, logger)
	authHandler := auth.NewHandler(authSvc, logger)

	projectStore := project.NewStore()
	projectSvc := project.NewService(projectStore, userDBMgr, logger)
	projectHandler := project.NewHandler(projectSvc, logger)

	noteStore := note.NewSQLStore()
	versionStore := note.NewVersionStore()
	noteSvc := note.NewService(noteStore, versionStore, projectStore, userDBMgr, nil, logger)
	noteHandler := note.NewHandler(noteSvc, logger)

	ftsStore := search.NewFTSStore()
	searchSvc := search.NewService(ftsStore, userDBMgr, logger)
	searchHandler := search.NewHandler(searchSvc, logger)

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

	return ts, jwtMgr, userDBMgr
}

func registerUser(t *testing.T, ts *httptest.Server, username, email, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"username": username,
		"email":    email,
		"password": password,
	})
	resp, err := http.Post(ts.URL+"/api/auth/register", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var result struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Tokens.AccessToken
}

func authedRequest(ts *httptest.Server, method, path, token string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, ts.URL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

// TestPerformance_1000Notes tests graph endpoint performance with 1000 notes.
func TestPerformance_1000Notes(t *testing.T) {
	ts, _, _ := setupPerfServer(t)
	token := registerUser(t, ts, "perfuser", "perf@test.com", "password123")

	// Create a project.
	resp, err := authedRequest(ts, "POST", "/api/projects", token, map[string]string{
		"name": "Perf Project",
	})
	require.NoError(t, err)
	defer resp.Body.Close()
	var proj struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&proj)
	require.NotEmpty(t, proj.ID)

	// Create 1000 notes, some with wikilinks to earlier notes.
	noteIDs := make([]string, 0, 1000)
	for i := 0; i < 1000; i++ {
		body := fmt.Sprintf("Note %d content.", i)
		if i > 0 && i%3 == 0 {
			// Link to a previous note.
			body += fmt.Sprintf(" See [[Note %d]].", i-1)
		}
		if i > 10 && i%7 == 0 {
			body += fmt.Sprintf(" Also [[Note %d]].", i-10)
		}

		resp, err := authedRequest(ts, "POST", "/api/notes", token, map[string]interface{}{
			"title":      fmt.Sprintf("Note %d", i),
			"body":       body,
			"project_id": proj.ID,
			"tags":       []string{fmt.Sprintf("tag%d", i%10)},
		})
		require.NoError(t, err)
		var n struct {
			ID string `json:"id"`
		}
		json.NewDecoder(resp.Body).Decode(&n)
		resp.Body.Close()
		require.NotEmpty(t, n.ID, "note %d should have an ID", i)
		noteIDs = append(noteIDs, n.ID)

		if i%100 == 0 {
			t.Logf("Created %d/1000 notes", i)
		}
	}
	t.Logf("Created %d notes total", len(noteIDs))

	// Measure graph endpoint performance.
	start := time.Now()
	resp, err = authedRequest(ts, "GET", "/api/graph", token, nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	graphDuration := time.Since(start)

	var graphResp struct {
		Nodes []struct{ ID string }             `json:"nodes"`
		Edges []struct{ Source, Target string } `json:"edges"`
	}
	json.NewDecoder(resp.Body).Decode(&graphResp)

	t.Logf("Graph endpoint: %d nodes, %d edges in %v", len(graphResp.Nodes), len(graphResp.Edges), graphDuration)
	require.LessOrEqual(t, graphDuration, 5*time.Second, "graph endpoint should respond within 5 seconds for 500 nodes")
	// Default limit is 500 so should get at most 500.
	require.LessOrEqual(t, len(graphResp.Nodes), 500)

	// Measure orphan endpoint.
	start = time.Now()
	resp, err = authedRequest(ts, "GET", "/api/graph/orphans", token, nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	orphanDuration := time.Since(start)
	t.Logf("Orphans endpoint: %v", orphanDuration)
	require.LessOrEqual(t, orphanDuration, 5*time.Second)

	// Measure search endpoint.
	start = time.Now()
	resp, err = authedRequest(ts, "GET", "/api/search?q=Note", token, nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	searchDuration := time.Since(start)
	t.Logf("Search endpoint: %v", searchDuration)
	require.LessOrEqual(t, searchDuration, 5*time.Second)
}

// TestPerformance_ConcurrentUsers tests 3 concurrent clients hitting
// the single-user backend. Seam is single-user (post-C-2), so the test
// registers one owner up front and hands the same token to 3 goroutines
// to exercise the same per-user database under load.
func TestPerformance_ConcurrentUsers(t *testing.T) {
	ts, _, _ := setupPerfServer(t)

	token := registerUser(t, ts, "perfuser", "perf@test.com", "password123")

	var wg sync.WaitGroup
	errors := make(chan error, 3)

	for u := 0; u < 3; u++ {
		wg.Add(1)
		go func(userIdx int) {
			defer wg.Done()

			// Create 50 notes per user.
			for i := 0; i < 50; i++ {
				body := fmt.Sprintf("User %d Note %d.", userIdx, i)
				if i > 0 && i%2 == 0 {
					body += fmt.Sprintf(" See [[User %d Note %d]].", userIdx, i-1)
				}
				resp, err := authedRequest(ts, "POST", "/api/notes", token, map[string]interface{}{
					"title": fmt.Sprintf("User %d Note %d", userIdx, i),
					"body":  body,
					"tags":  []string{fmt.Sprintf("user%d", userIdx)},
				})
				if err != nil {
					errors <- fmt.Errorf("user %d note %d create: %w", userIdx, i, err)
					return
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusCreated {
					errors <- fmt.Errorf("user %d note %d: expected 201 got %d", userIdx, i, resp.StatusCode)
					return
				}
			}

			// Fetch graph for this user.
			resp, err := authedRequest(ts, "GET", "/api/graph", token, nil)
			if err != nil {
				errors <- fmt.Errorf("user %d graph: %w", userIdx, err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errors <- fmt.Errorf("user %d graph: expected 200 got %d", userIdx, resp.StatusCode)
				return
			}
		}(u)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Fatal(err)
	}
}
