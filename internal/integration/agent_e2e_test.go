//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/katata/seam/internal/agent"
	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/graph"
	seamMCP "github.com/katata/seam/internal/mcp"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/search"
	"github.com/katata/seam/internal/server"
	"github.com/katata/seam/internal/testutil"
	"github.com/katata/seam/internal/userdb"
)

// setupAgentServer creates a full server stack including agent/MCP components.
// Returns a testClient with a valid access token.
func setupAgentServer(t *testing.T) *testClient {
	t.Helper()
	dataDir := testutil.TestDataDir(t)
	serverDB := testutil.TestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	userDBMgr := userdb.NewSQLManager(dataDir, 10*time.Minute, logger)
	t.Cleanup(func() { userDBMgr.CloseAll() })

	// Auth stack.
	jwtMgr := auth.NewJWTManager("test-secret-key-for-agent-e2e", 15*time.Minute)
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

	// Agent / MCP stack.
	agentStore := agent.NewSQLStore()
	agentSvc := agent.NewService(agent.ServiceConfig{
		Store:          agentStore,
		NoteService:    noteSvc,
		ProjectService: projectSvc,
		SearchService:  searchSvc,
		UserDBManager:  userDBMgr,
		Logger:         logger,
	})
	mcpSrv := seamMCP.New(seamMCP.Config{
		AgentService: agentSvc,
		Logger:       logger,
	})
	mcpHandler := mcpSrv.Handler(jwtMgr)

	srv := server.New(server.Config{
		Listen:         ":0",
		Logger:         logger,
		JWTManager:     jwtMgr,
		AuthHandler:    authHandler,
		ProjectHandler: projectHandler,
		NoteHandler:    noteHandler,
		SearchHandler:  searchHandler,
		GraphHandler:   graphHandler,
		MCPHandler:     mcpHandler,
	})

	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	c := &testClient{ts: ts}

	// Register and authenticate.
	var authResp struct {
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	resp := c.do("POST", "/api/auth/register", map[string]string{
		"username": "agentuser",
		"email":    "agent@example.com",
		"password": "securepassword123",
	}, &authResp)
	require.Equal(t, 201, resp.StatusCode)
	c.accessToken = authResp.Tokens.AccessToken

	return c
}

// setupAgentService creates an agent service with real stores for integration testing.
func setupAgentService(t *testing.T) (*agent.Service, userdb.Manager) {
	t.Helper()
	dataDir := testutil.TestDataDir(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	userDBMgr := userdb.NewSQLManager(dataDir, 10*time.Minute, logger)
	t.Cleanup(func() { userDBMgr.CloseAll() })

	projectStore := project.NewStore()
	projectSvc := project.NewService(projectStore, userDBMgr, logger)
	noteStore := note.NewSQLStore()
	versionStore := note.NewVersionStore()
	noteSvc := note.NewService(noteStore, versionStore, projectStore, userDBMgr, nil, logger)
	ftsStore := search.NewFTSStore()
	searchSvc := search.NewService(ftsStore, userDBMgr, logger)

	agentStore := agent.NewSQLStore()
	svc := agent.NewService(agent.ServiceConfig{
		Store:          agentStore,
		NoteService:    noteSvc,
		ProjectService: projectSvc,
		SearchService:  searchSvc,
		UserDBManager:  userDBMgr,
		Logger:         logger,
	})

	return svc, userDBMgr
}

// TestE2E_AgentSessionLifecycle exercises the full session lifecycle:
// start -> plan -> progress -> context -> resume -> end -> verify.
func TestE2E_AgentSessionLifecycle(t *testing.T) {
	svc, userDBMgr := setupAgentService(t)
	ctx := context.Background()
	userID := "integ-user-001"
	_, err := userDBMgr.Open(ctx, userID)
	require.NoError(t, err)

	// Step 1: session_start -- creates a new session.
	briefing, err := svc.SessionStart(ctx, userID, "build-feature", agent.DefaultMaxContextChars)
	require.NoError(t, err)
	require.NotNil(t, briefing)
	require.NotNil(t, briefing.Session)
	require.Equal(t, "build-feature", briefing.Session.Name)
	require.Equal(t, agent.StatusActive, briefing.Session.Status)
	require.NotEmpty(t, briefing.Session.ID)

	// Step 2: session_plan_set -- creates a plan note.
	planContent := "## Goals\n- Implement the feature\n- Write tests\n\n## Tasks\n1. Design API\n2. Implement handler\n3. Add tests"
	planNoteID, err := svc.SessionPlanSet(ctx, userID, "build-feature", planContent)
	require.NoError(t, err)
	require.NotEmpty(t, planNoteID)

	// Step 3: session_progress_update -- create and append progress entries.
	progNoteID, err := svc.SessionProgressUpdate(ctx, userID, "build-feature", "Design API", "completed", "API design finalized")
	require.NoError(t, err)
	require.NotEmpty(t, progNoteID)

	progNoteID2, err := svc.SessionProgressUpdate(ctx, userID, "build-feature", "Implement handler", "in_progress", "")
	require.NoError(t, err)
	require.Equal(t, progNoteID, progNoteID2, "should append to the same progress note")

	progNoteID3, err := svc.SessionProgressUpdate(ctx, userID, "build-feature", "Implement handler", "completed", "Handler implemented with validation")
	require.NoError(t, err)
	require.Equal(t, progNoteID, progNoteID3)

	// Step 4: session_context_set -- create and update context note.
	ctxContent := "Working on the user authentication feature. Using JWT for auth tokens."
	ctxNoteID, err := svc.SessionContextSet(ctx, userID, "build-feature", ctxContent)
	require.NoError(t, err)
	require.NotEmpty(t, ctxNoteID)

	ctxContent2 := "Updated context: switched to OAuth2 flow."
	ctxNoteID2, err := svc.SessionContextSet(ctx, userID, "build-feature", ctxContent2)
	require.NoError(t, err)
	require.Equal(t, ctxNoteID, ctxNoteID2, "should update existing context note")

	// Step 5: Resume session -- verify full briefing.
	resumed, err := svc.SessionStart(ctx, userID, "build-feature", agent.DefaultMaxContextChars)
	require.NoError(t, err)
	require.Equal(t, briefing.Session.ID, resumed.Session.ID, "should resume same session")
	require.NotEmpty(t, resumed.Plan, "briefing should contain the plan")
	require.NotEmpty(t, resumed.LastProgress, "briefing should contain progress")

	// Step 6: session_end -- complete the session.
	findings := "Feature implemented. API handler at /api/users with JWT auth. 3 new files, all tests pass."
	err = svc.SessionEnd(ctx, userID, "build-feature", findings)
	require.NoError(t, err)

	// Verify session is completed.
	sessions, err := svc.SessionList(ctx, userID, "completed", 10)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, "build-feature", sessions[0].Name)
	require.Equal(t, agent.StatusCompleted, sessions[0].Status)
	require.Equal(t, findings, sessions[0].Findings)

	// Verify active session list is empty.
	activeSessions, err := svc.SessionList(ctx, userID, "active", 10)
	require.NoError(t, err)
	require.Empty(t, activeSessions)

	// Step 7: Cannot end an already-completed session.
	err = svc.SessionEnd(ctx, userID, "build-feature", "double end")
	require.Error(t, err)
	require.ErrorIs(t, err, agent.ErrSessionNotActive)
}

// TestE2E_HierarchicalSessions exercises parent -> child -> sibling finding flow.
func TestE2E_HierarchicalSessions(t *testing.T) {
	svc, userDBMgr := setupAgentService(t)
	ctx := context.Background()
	userID := "integ-user-002"
	_, err := userDBMgr.Open(ctx, userID)
	require.NoError(t, err)

	// Step 1: Start parent session.
	parentBriefing, err := svc.SessionStart(ctx, userID, "refactor-auth", agent.DefaultMaxContextChars)
	require.NoError(t, err)
	parentID := parentBriefing.Session.ID

	// Set parent plan.
	_, err = svc.SessionPlanSet(ctx, userID, "refactor-auth", "Refactor auth to support API keys and JWT.")
	require.NoError(t, err)

	// Step 2: Start child-a (subagent A).
	childA, err := svc.SessionStart(ctx, userID, "refactor-auth/analyze", agent.DefaultMaxContextChars)
	require.NoError(t, err)
	require.Equal(t, parentID, childA.Session.ParentSessionID, "child should reference parent")
	require.NotEmpty(t, childA.ParentPlan, "child briefing should contain parent plan")

	// Complete child-a with findings.
	childAFindings := "Current middleware uses chain-of-responsibility. Auth at internal/auth/middleware.go:45."
	err = svc.SessionEnd(ctx, userID, "refactor-auth/analyze", childAFindings)
	require.NoError(t, err)

	// Step 3: Start child-b (subagent B) -- should see sibling findings.
	childB, err := svc.SessionStart(ctx, userID, "refactor-auth/implement", agent.DefaultMaxContextChars)
	require.NoError(t, err)
	require.Equal(t, parentID, childB.Session.ParentSessionID, "child-b should reference parent")
	require.NotEmpty(t, childB.ParentPlan, "child-b should see parent plan")

	// Verify sibling findings flow.
	require.NotEmpty(t, childB.SiblingFindings, "child-b should see child-a's findings")
	found := false
	for _, sib := range childB.SiblingFindings {
		if sib.SessionName == "refactor-auth/analyze" {
			require.Contains(t, sib.Findings, "chain-of-responsibility")
			found = true
		}
	}
	require.True(t, found, "child-b briefing should contain child-a findings")

	// Complete child-b.
	err = svc.SessionEnd(ctx, userID, "refactor-auth/implement", "API key store implemented. SHA-256 hashed keys.")
	require.NoError(t, err)

	// Step 4: Verify session list shows all sessions.
	allSessions, err := svc.SessionList(ctx, userID, "", 20)
	require.NoError(t, err)
	require.Len(t, allSessions, 3, "should have parent + 2 children")
}

// TestE2E_OrphanChildReconciliation tests that children started before
// their parent get linked when the parent starts.
func TestE2E_OrphanChildReconciliation(t *testing.T) {
	svc, userDBMgr := setupAgentService(t)
	ctx := context.Background()
	userID := "integ-user-003"
	_, err := userDBMgr.Open(ctx, userID)
	require.NoError(t, err)

	// Step 1: Start child BEFORE parent (orphan scenario).
	childBriefing, err := svc.SessionStart(ctx, userID, "project-x/task-a", agent.DefaultMaxContextChars)
	require.NoError(t, err)
	require.Empty(t, childBriefing.Session.ParentSessionID, "no parent yet")

	// Step 2: Start parent -- should reconcile orphan child.
	parentBriefing, err := svc.SessionStart(ctx, userID, "project-x", agent.DefaultMaxContextChars)
	require.NoError(t, err)
	parentID := parentBriefing.Session.ID

	// Step 3: Resume child -- should now have parent linked.
	childResumed, err := svc.SessionStart(ctx, userID, "project-x/task-a", agent.DefaultMaxContextChars)
	require.NoError(t, err)
	require.Equal(t, parentID, childResumed.Session.ParentSessionID,
		"orphan child should be reconciled to parent")
}

// TestE2E_MemoryCRUDLifecycle exercises write -> read -> append -> list -> delete.
func TestE2E_MemoryCRUDLifecycle(t *testing.T) {
	svc, userDBMgr := setupAgentService(t)
	ctx := context.Background()
	userID := "integ-user-004"
	_, err := userDBMgr.Open(ctx, userID)
	require.NoError(t, err)

	// Step 1: Write knowledge notes.
	noteID1, err := svc.MemoryWrite(ctx, userID, "go", "error-handling", "Use fmt.Errorf with %%w for wrapping errors.")
	require.NoError(t, err)
	require.NotEmpty(t, noteID1)

	noteID2, err := svc.MemoryWrite(ctx, userID, "go", "testing-patterns", "Use testify/require for fail-fast assertions.")
	require.NoError(t, err)
	require.NotEmpty(t, noteID2)

	noteID3, err := svc.MemoryWrite(ctx, userID, "python", "virtual-envs", "Always use venv for project isolation.")
	require.NoError(t, err)
	require.NotEmpty(t, noteID3)

	// Step 2: Read a note.
	title, body, err := svc.MemoryRead(ctx, userID, "go", "error-handling")
	require.NoError(t, err)
	require.Equal(t, "Knowledge: go - error-handling", title)
	require.Contains(t, body, "fmt.Errorf")

	// Step 3: Update existing note via MemoryWrite (upsert).
	noteID1b, err := svc.MemoryWrite(ctx, userID, "go", "error-handling", "Updated: Use errors.Is and errors.As for unwrapping.")
	require.NoError(t, err)
	require.Equal(t, noteID1, noteID1b, "should update existing note, not create new")

	// Verify update.
	_, body, err = svc.MemoryRead(ctx, userID, "go", "error-handling")
	require.NoError(t, err)
	require.Contains(t, body, "errors.Is")

	// Step 4: Append to a note.
	err = svc.MemoryAppend(ctx, userID, "go", "testing-patterns", "\nAlso use table-driven tests for multiple inputs.")
	require.NoError(t, err)

	_, body, err = svc.MemoryRead(ctx, userID, "go", "testing-patterns")
	require.NoError(t, err)
	require.Contains(t, body, "testify/require")
	require.Contains(t, body, "table-driven tests")

	// Step 5: List all knowledge notes.
	allItems, err := svc.MemoryList(ctx, userID, "")
	require.NoError(t, err)
	require.Len(t, allItems, 3)

	// Step 6: List by category.
	goItems, err := svc.MemoryList(ctx, userID, "go")
	require.NoError(t, err)
	require.Len(t, goItems, 2)
	for _, item := range goItems {
		require.Equal(t, "go", item.Category)
	}

	pythonItems, err := svc.MemoryList(ctx, userID, "python")
	require.NoError(t, err)
	require.Len(t, pythonItems, 1)

	// Step 7: Delete a note.
	err = svc.MemoryDelete(ctx, userID, "python", "virtual-envs")
	require.NoError(t, err)

	// Verify deletion.
	_, _, err = svc.MemoryRead(ctx, userID, "python", "virtual-envs")
	require.Error(t, err)
	require.ErrorIs(t, err, agent.ErrNotFound)

	// Verify list updated.
	allItems, err = svc.MemoryList(ctx, userID, "")
	require.NoError(t, err)
	require.Len(t, allItems, 2)
}

// TestE2E_ContextGather_WithRealFTS exercises ContextGather with real FTS data.
func TestE2E_ContextGather_WithRealFTS(t *testing.T) {
	svc, userDBMgr := setupAgentService(t)
	ctx := context.Background()
	userID := "integ-user-005"
	_, err := userDBMgr.Open(ctx, userID)
	require.NoError(t, err)

	// Create agent knowledge notes.
	_, err = svc.MemoryWrite(ctx, userID, "architecture", "middleware-patterns",
		"Go middleware uses the chain-of-responsibility pattern. Each middleware wraps the next handler.")
	require.NoError(t, err)

	_, err = svc.MemoryWrite(ctx, userID, "architecture", "database-access",
		"SQLite with WAL mode for concurrent reads. Use named in-memory databases for test isolation.")
	require.NoError(t, err)

	// ContextGather should find relevant notes via FTS.
	hits, err := svc.ContextGather(ctx, userID, "middleware", 3000)
	require.NoError(t, err)
	require.NotEmpty(t, hits, "should find notes matching the query")

	// Verify results are within budget.
	totalChars := 0
	for _, hit := range hits {
		totalChars += len(hit.Title) + len(": ") + len(hit.Snippet)
	}
	require.LessOrEqual(t, totalChars, 3100, "results should be within budget")
}

// TestE2E_ContextGather_BudgetTruncation verifies strict budget enforcement.
func TestE2E_ContextGather_BudgetTruncation(t *testing.T) {
	svc, userDBMgr := setupAgentService(t)
	ctx := context.Background()
	userID := "integ-user-006"
	_, err := userDBMgr.Open(ctx, userID)
	require.NoError(t, err)

	// Create many large notes to test budget enforcement.
	for i := 0; i < 10; i++ {
		largeBody := "This is a comprehensive note about middleware patterns and authentication. " +
			"It covers various approaches to implementing middleware in Go, including " +
			"chain-of-responsibility, decorator pattern, and functional composition. " +
			"The middleware validates JWT tokens, checks rate limits, and logs requests. " +
			"Each middleware function wraps the next handler in the chain."
		_, err = svc.MemoryWrite(ctx, userID, "patterns", fmt.Sprintf("middleware-note-%d", i), largeBody)
		require.NoError(t, err)
	}

	// Search with a small budget.
	hits, err := svc.ContextGather(ctx, userID, "middleware patterns authentication", 200)
	require.NoError(t, err)

	// Verify total output is within budget.
	totalChars := 0
	for _, hit := range hits {
		totalChars += len(hit.Title) + len(": ") + len(hit.Snippet)
	}
	require.LessOrEqual(t, totalChars, 250,
		"total output should be within budget (small margin for rounding)")
}

// TestE2E_MixedSessionAndMemory verifies sessions and knowledge work together.
func TestE2E_MixedSessionAndMemory(t *testing.T) {
	svc, userDBMgr := setupAgentService(t)
	ctx := context.Background()
	userID := "integ-user-007"
	_, err := userDBMgr.Open(ctx, userID)
	require.NoError(t, err)

	// Store domain knowledge first.
	_, err = svc.MemoryWrite(ctx, userID, "go", "concurrency",
		"Use sync.WaitGroup for goroutine coordination. Never use time.Sleep for synchronization.")
	require.NoError(t, err)

	// Start a session.
	briefing, err := svc.SessionStart(ctx, userID, "fix-race-condition", agent.DefaultMaxContextChars)
	require.NoError(t, err)
	require.NotNil(t, briefing)

	// Create session plan.
	_, err = svc.SessionPlanSet(ctx, userID, "fix-race-condition",
		"Fix the race condition in the concurrent map access. Use sync.RWMutex.")
	require.NoError(t, err)

	// Track progress.
	_, err = svc.SessionProgressUpdate(ctx, userID, "fix-race-condition",
		"Add mutex to shared state", "completed", "Added sync.RWMutex to protect the map")
	require.NoError(t, err)

	// Store what we learned as persistent knowledge.
	_, err = svc.MemoryWrite(ctx, userID, "go", "race-conditions",
		"Maps are not safe for concurrent use. Protect with sync.RWMutex or sync.Map.")
	require.NoError(t, err)

	// End session.
	err = svc.SessionEnd(ctx, userID, "fix-race-condition",
		"Fixed race condition by adding sync.RWMutex to the shared cache map.")
	require.NoError(t, err)

	// Knowledge persists after session ends.
	_, body, err := svc.MemoryRead(ctx, userID, "go", "race-conditions")
	require.NoError(t, err)
	require.Contains(t, body, "sync.RWMutex")

	// Both knowledge items are listed.
	items, err := svc.MemoryList(ctx, userID, "go")
	require.NoError(t, err)
	require.Len(t, items, 2)
}

// TestE2E_UserIsolation verifies user data isolation.
func TestE2E_UserIsolation(t *testing.T) {
	svc, userDBMgr := setupAgentService(t)
	ctx := context.Background()
	user1 := "iso-user-001"
	user2 := "iso-user-002"
	_, err := userDBMgr.Open(ctx, user1)
	require.NoError(t, err)
	_, err = userDBMgr.Open(ctx, user2)
	require.NoError(t, err)

	// User1 writes knowledge.
	_, err = svc.MemoryWrite(ctx, user1, "secrets", "api-key", "sk-user1-secret-key")
	require.NoError(t, err)

	// User1 starts a session.
	_, err = svc.SessionStart(ctx, user1, "user1-session", agent.DefaultMaxContextChars)
	require.NoError(t, err)

	// User2 cannot read user1's knowledge.
	_, _, err = svc.MemoryRead(ctx, user2, "secrets", "api-key")
	require.Error(t, err)
	require.ErrorIs(t, err, agent.ErrNotFound)

	// User2 cannot see user1's sessions.
	sessions, err := svc.SessionList(ctx, user2, "", 20)
	require.NoError(t, err)
	require.Empty(t, sessions)

	// User2 creates their own.
	_, err = svc.MemoryWrite(ctx, user2, "secrets", "api-key", "sk-user2-different-key")
	require.NoError(t, err)

	// User2 reads their own -- different content.
	_, body, err := svc.MemoryRead(ctx, user2, "secrets", "api-key")
	require.NoError(t, err)
	require.Contains(t, body, "user2-different-key")
	require.NotContains(t, body, "user1-secret-key")
}

// TestE2E_MCPEndpoint_Accessible verifies the MCP endpoint is reachable.
func TestE2E_MCPEndpoint_Accessible(t *testing.T) {
	c := setupAgentServer(t)

	// Send a JSON-RPC initialize request to verify the endpoint is reachable.
	var result json.RawMessage
	resp := c.do("POST", "/api/mcp", map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"clientInfo": map[string]string{
				"name":    "test-client",
				"version": "1.0.0",
			},
			"capabilities": map[string]interface{}{},
		},
	}, &result)
	require.Equal(t, 200, resp.StatusCode)
	require.NotNil(t, result)
}
