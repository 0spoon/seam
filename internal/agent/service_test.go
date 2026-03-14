package agent

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/search"
	"github.com/katata/seam/internal/userdb"
)

const testUserID = "test-user-001"

// setupTestService creates a Service with real stores and a temp data dir.
func setupTestService(t *testing.T) (*Service, userdb.Manager) {
	t.Helper()

	dataDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	mgr := userdb.NewSQLManager(dataDir, time.Hour, logger)
	t.Cleanup(func() { mgr.CloseAll() })

	noteStore := note.NewSQLStore()
	versionStore := note.NewVersionStore()
	projStore := project.NewStore()

	noteSvc := note.NewService(noteStore, versionStore, projStore, mgr, nil, logger)
	projSvc := project.NewService(projStore, mgr, logger)

	searchFTS := search.NewFTSStore()
	searchSvc := search.NewService(searchFTS, mgr, logger)

	svc := NewService(ServiceConfig{
		Store:          NewSQLStore(),
		NoteService:    noteSvc,
		ProjectService: projSvc,
		SearchService:  searchSvc,
		UserDBManager:  mgr,
		Logger:         logger,
	})

	return svc, mgr
}

// --- Session Lifecycle Tests ---

func TestService_SessionStart_CreatesNewSession(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	briefing, err := svc.SessionStart(ctx, testUserID, "new-session", DefaultMaxContextChars)
	require.NoError(t, err)
	require.NotNil(t, briefing)
	require.NotNil(t, briefing.Session)
	require.Equal(t, "new-session", briefing.Session.Name)
	require.Equal(t, StatusActive, briefing.Session.Status)
	require.NotEmpty(t, briefing.Session.ID)
}

func TestService_SessionStart_ResumesExisting(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Start a session.
	b1, err := svc.SessionStart(ctx, testUserID, "resume-me", DefaultMaxContextChars)
	require.NoError(t, err)
	sessionID := b1.Session.ID

	// Start the same session again -- should resume, not create new.
	b2, err := svc.SessionStart(ctx, testUserID, "resume-me", DefaultMaxContextChars)
	require.NoError(t, err)
	require.Equal(t, sessionID, b2.Session.ID)
}

func TestService_SessionStart_ResolvesParent(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Start parent.
	parentBriefing, err := svc.SessionStart(ctx, testUserID, "parent", DefaultMaxContextChars)
	require.NoError(t, err)

	// Start child.
	childBriefing, err := svc.SessionStart(ctx, testUserID, "parent/child", DefaultMaxContextChars)
	require.NoError(t, err)
	require.Equal(t, parentBriefing.Session.ID, childBriefing.Session.ParentSessionID)
}

func TestService_SessionStart_ReconcileOrphans(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Start child before parent (orphan).
	childBriefing, err := svc.SessionStart(ctx, testUserID, "late-parent/early-child", DefaultMaxContextChars)
	require.NoError(t, err)
	require.Empty(t, childBriefing.Session.ParentSessionID)

	// Now start the parent -- should reconcile the child.
	parentBriefing, err := svc.SessionStart(ctx, testUserID, "late-parent", DefaultMaxContextChars)
	require.NoError(t, err)

	// Re-fetch the child to verify reconciliation.
	db, err := svc.cfg.UserDBManager.Open(ctx, testUserID)
	require.NoError(t, err)
	child, err := svc.cfg.Store.GetSession(ctx, db, childBriefing.Session.ID)
	require.NoError(t, err)
	require.Equal(t, parentBriefing.Session.ID, child.ParentSessionID)
}

func TestService_SessionStart_InvalidName(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.SessionStart(ctx, testUserID, "", DefaultMaxContextChars)
	require.ErrorIs(t, err, ErrInvalidSessionName)

	_, err = svc.SessionStart(ctx, testUserID, "/leading-slash", DefaultMaxContextChars)
	require.ErrorIs(t, err, ErrInvalidSessionName)

	_, err = svc.SessionStart(ctx, testUserID, "has spaces", DefaultMaxContextChars)
	require.ErrorIs(t, err, ErrInvalidSessionName)
}

func TestService_SessionEnd_Success(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.SessionStart(ctx, testUserID, "end-me", DefaultMaxContextChars)
	require.NoError(t, err)

	err = svc.SessionEnd(ctx, testUserID, "end-me", "Completed the refactoring.")
	require.NoError(t, err)

	// Verify session is now completed.
	db, err := svc.cfg.UserDBManager.Open(ctx, testUserID)
	require.NoError(t, err)
	session, err := svc.cfg.Store.GetSessionByName(ctx, db, "end-me")
	require.NoError(t, err)
	require.Equal(t, StatusCompleted, session.Status)
	require.Equal(t, "Completed the refactoring.", session.Findings)
}

func TestService_SessionEnd_EmptyFindings(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.SessionStart(ctx, testUserID, "no-findings", DefaultMaxContextChars)
	require.NoError(t, err)

	err = svc.SessionEnd(ctx, testUserID, "no-findings", "")
	require.ErrorIs(t, err, ErrFindingsRequired)
}

func TestService_SessionEnd_FindingsTooLong(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.SessionStart(ctx, testUserID, "verbose", DefaultMaxContextChars)
	require.NoError(t, err)

	longFindings := make([]byte, MaxFindingsChars+1)
	for i := range longFindings {
		longFindings[i] = 'a'
	}
	err = svc.SessionEnd(ctx, testUserID, "verbose", string(longFindings))
	require.ErrorIs(t, err, ErrFindingsTooLong)
}

func TestService_SessionEnd_NotFound(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	err := svc.SessionEnd(ctx, testUserID, "nonexistent", "findings")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestService_SessionList(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.SessionStart(ctx, testUserID, "list-a", DefaultMaxContextChars)
	require.NoError(t, err)
	_, err = svc.SessionStart(ctx, testUserID, "list-b", DefaultMaxContextChars)
	require.NoError(t, err)

	sessions, err := svc.SessionList(ctx, testUserID, StatusActive, 20)
	require.NoError(t, err)
	require.Len(t, sessions, 2)
}

// --- Session Plan, Progress, Context ---

func TestService_SessionPlanSet_CreatesNote(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.SessionStart(ctx, testUserID, "plan-test", DefaultMaxContextChars)
	require.NoError(t, err)

	noteID, err := svc.SessionPlanSet(ctx, testUserID, "plan-test", "## Goals\n- Refactor auth")
	require.NoError(t, err)
	require.NotEmpty(t, noteID)

	// Verify note was created with correct tags.
	n, err := svc.cfg.NoteService.Get(ctx, testUserID, noteID)
	require.NoError(t, err)
	require.Equal(t, PlanNoteTitle("plan-test"), n.Title)
	require.Contains(t, n.Body, "Refactor auth")
	require.Contains(t, n.Tags, "session:plan-test")
	require.Contains(t, n.Tags, "type:plan")
	require.Contains(t, n.Tags, TagCreatedByAgent)
}

func TestService_SessionPlanSet_UpdatesExistingNote(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.SessionStart(ctx, testUserID, "plan-update", DefaultMaxContextChars)
	require.NoError(t, err)

	noteID1, err := svc.SessionPlanSet(ctx, testUserID, "plan-update", "## Original Plan")
	require.NoError(t, err)

	// Set a new plan -- should update the same note.
	noteID2, err := svc.SessionPlanSet(ctx, testUserID, "plan-update", "## Updated Plan")
	require.NoError(t, err)
	require.Equal(t, noteID1, noteID2)

	n, err := svc.cfg.NoteService.Get(ctx, testUserID, noteID1)
	require.NoError(t, err)
	require.Contains(t, n.Body, "Updated Plan")
}

func TestService_SessionProgressUpdate(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.SessionStart(ctx, testUserID, "progress-test", DefaultMaxContextChars)
	require.NoError(t, err)

	noteID, err := svc.SessionProgressUpdate(ctx, testUserID, "progress-test",
		"Analyze middleware", "in_progress", "Starting analysis")
	require.NoError(t, err)
	require.NotEmpty(t, noteID)

	// Add another progress entry.
	noteID2, err := svc.SessionProgressUpdate(ctx, testUserID, "progress-test",
		"Analyze middleware", "completed", "Found 3 patterns")
	require.NoError(t, err)
	require.Equal(t, noteID, noteID2)
}

func TestService_SessionContextSet(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.SessionStart(ctx, testUserID, "context-test", DefaultMaxContextChars)
	require.NoError(t, err)

	noteID, err := svc.SessionContextSet(ctx, testUserID, "context-test", "Key decisions: use JWT for auth")
	require.NoError(t, err)
	require.NotEmpty(t, noteID)

	n, err := svc.cfg.NoteService.Get(ctx, testUserID, noteID)
	require.NoError(t, err)
	require.Equal(t, ContextNoteTitle("context-test"), n.Title)
	require.Contains(t, n.Body, "Key decisions")
}

// --- Knowledge (Memory) CRUD Tests ---

func TestService_MemoryWrite_CreatesNote(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	noteID, err := svc.MemoryWrite(ctx, testUserID, "go", "middleware-patterns",
		"## Middleware Patterns\n\n- Chain of responsibility")
	require.NoError(t, err)
	require.NotEmpty(t, noteID)

	n, err := svc.cfg.NoteService.Get(ctx, testUserID, noteID)
	require.NoError(t, err)
	require.Equal(t, KnowledgeNoteTitle("go", "middleware-patterns"), n.Title)
	require.Contains(t, n.Tags, "type:knowledge")
	require.Contains(t, n.Tags, "domain:go")
	require.Contains(t, n.Tags, TagCreatedByAgent)
}

func TestService_MemoryWrite_UpdatesExisting(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	id1, err := svc.MemoryWrite(ctx, testUserID, "go", "patterns", "Original")
	require.NoError(t, err)

	id2, err := svc.MemoryWrite(ctx, testUserID, "go", "patterns", "Updated")
	require.NoError(t, err)
	require.Equal(t, id1, id2)

	n, err := svc.cfg.NoteService.Get(ctx, testUserID, id1)
	require.NoError(t, err)
	require.Contains(t, n.Body, "Updated")
}

func TestService_MemoryRead_Success(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.MemoryWrite(ctx, testUserID, "rust", "ownership", "Borrow checker rules")
	require.NoError(t, err)

	title, body, err := svc.MemoryRead(ctx, testUserID, "rust", "ownership")
	require.NoError(t, err)
	require.Equal(t, KnowledgeNoteTitle("rust", "ownership"), title)
	require.Contains(t, body, "Borrow checker")
}

func TestService_MemoryRead_NotFound(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Ensure the agent-memory project exists.
	_, err := svc.MemoryWrite(ctx, testUserID, "x", "y", "z")
	require.NoError(t, err)

	_, _, err = svc.MemoryRead(ctx, testUserID, "nonexistent", "nope")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestService_MemoryAppend_Success(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.MemoryWrite(ctx, testUserID, "go", "tips", "Tip 1: use interfaces")
	require.NoError(t, err)

	err = svc.MemoryAppend(ctx, testUserID, "go", "tips", "\nTip 2: prefer composition")
	require.NoError(t, err)

	_, body, err := svc.MemoryRead(ctx, testUserID, "go", "tips")
	require.NoError(t, err)
	require.Contains(t, body, "Tip 1")
	require.Contains(t, body, "Tip 2")
}

func TestService_MemoryAppend_NotFound(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Ensure project exists.
	_, err := svc.MemoryWrite(ctx, testUserID, "x", "y", "z")
	require.NoError(t, err)

	err = svc.MemoryAppend(ctx, testUserID, "nonexistent", "nope", "data")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestService_MemoryList(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.MemoryWrite(ctx, testUserID, "go", "patterns", "content")
	require.NoError(t, err)
	_, err = svc.MemoryWrite(ctx, testUserID, "go", "testing", "content")
	require.NoError(t, err)
	_, err = svc.MemoryWrite(ctx, testUserID, "rust", "ownership", "content")
	require.NoError(t, err)

	// List all knowledge.
	items, err := svc.MemoryList(ctx, testUserID, "")
	require.NoError(t, err)
	require.Len(t, items, 3)

	// List by category.
	items, err = svc.MemoryList(ctx, testUserID, "go")
	require.NoError(t, err)
	require.Len(t, items, 2)
}

func TestService_MemoryDelete_Success(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	noteID, err := svc.MemoryWrite(ctx, testUserID, "go", "delete-me", "content")
	require.NoError(t, err)

	err = svc.MemoryDelete(ctx, testUserID, "go", "delete-me")
	require.NoError(t, err)

	// Verify it is gone.
	_, err = svc.cfg.NoteService.Get(ctx, testUserID, noteID)
	require.ErrorIs(t, err, note.ErrNotFound)
}

func TestService_MemoryDelete_NotFound(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Ensure project exists.
	_, err := svc.MemoryWrite(ctx, testUserID, "x", "y", "z")
	require.NoError(t, err)

	err = svc.MemoryDelete(ctx, testUserID, "nonexistent", "nope")
	require.ErrorIs(t, err, ErrNotFound)
}

// --- Agent-Memory Project Auto-Creation ---

func TestService_AutoCreatesAgentMemoryProject(t *testing.T) {
	svc, mgr := setupTestService(t)
	ctx := context.Background()

	// Before any operation, agent-memory project should not exist.
	db, err := mgr.Open(ctx, testUserID)
	require.NoError(t, err)
	projStore := project.NewStore()
	_, err = projStore.GetBySlug(ctx, db, AgentMemoryProject)
	require.ErrorIs(t, err, project.ErrNotFound)

	// SessionStart should auto-create the project.
	_, err = svc.SessionStart(ctx, testUserID, "auto-create-test", DefaultMaxContextChars)
	require.NoError(t, err)

	// Verify project exists now.
	p, err := projStore.GetBySlug(ctx, db, AgentMemoryProject)
	require.NoError(t, err)
	require.Equal(t, AgentMemoryProject, p.Slug)
}

// --- Sibling Findings Flow ---

func TestService_SiblingFindings_FlowToNewChild(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Start parent.
	_, err := svc.SessionStart(ctx, testUserID, "main-task", DefaultMaxContextChars)
	require.NoError(t, err)

	// Start child A and complete it with findings.
	_, err = svc.SessionStart(ctx, testUserID, "main-task/child-a", DefaultMaxContextChars)
	require.NoError(t, err)
	err = svc.SessionEnd(ctx, testUserID, "main-task/child-a", "Child A found X and Y")
	require.NoError(t, err)

	// Start child B -- its briefing should contain child A's findings.
	briefing, err := svc.SessionStart(ctx, testUserID, "main-task/child-b", DefaultMaxContextChars)
	require.NoError(t, err)
	require.NotEmpty(t, briefing.SiblingFindings)
	require.Equal(t, "main-task/child-a", briefing.SiblingFindings[0].SessionName)
	require.Contains(t, briefing.SiblingFindings[0].Findings, "Child A found X and Y")
}

// --- Error Handling ---

func TestService_SessionEnd_AlreadyCompleted(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.SessionStart(ctx, testUserID, "double-end", DefaultMaxContextChars)
	require.NoError(t, err)
	err = svc.SessionEnd(ctx, testUserID, "double-end", "First end")
	require.NoError(t, err)

	// Ending again should fail.
	err = svc.SessionEnd(ctx, testUserID, "double-end", "Second end")
	require.ErrorIs(t, err, ErrSessionNotActive)
}

// --- Helpers to verify DB state ---

func openTestDB(t *testing.T, svc *Service, userID string) *sql.DB {
	t.Helper()
	db, err := svc.cfg.UserDBManager.Open(context.Background(), userID)
	require.NoError(t, err)
	return db
}

// Ensure the unused import is consumed.
var _ = fmt.Sprintf

// --- Additional Coverage Tests ---

func TestService_SessionEnd_UpdatesNoteStatusTags(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.SessionStart(ctx, testUserID, "tag-update", DefaultMaxContextChars)
	require.NoError(t, err)

	planID, err := svc.SessionPlanSet(ctx, testUserID, "tag-update", "The plan")
	require.NoError(t, err)
	progressID, err := svc.SessionProgressUpdate(ctx, testUserID, "tag-update", "task-1", "in_progress", "started")
	require.NoError(t, err)
	contextID, err := svc.SessionContextSet(ctx, testUserID, "tag-update", "Some context")
	require.NoError(t, err)

	// Verify active tags before ending.
	for _, id := range []string{planID, progressID, contextID} {
		n, getErr := svc.cfg.NoteService.Get(ctx, testUserID, id)
		require.NoError(t, getErr)
		require.Contains(t, n.Tags, "status:active")
	}

	err = svc.SessionEnd(ctx, testUserID, "tag-update", "Done with the work")
	require.NoError(t, err)

	// Verify tags changed to status:completed.
	for _, id := range []string{planID, progressID, contextID} {
		n, getErr := svc.cfg.NoteService.Get(ctx, testUserID, id)
		require.NoError(t, getErr)
		require.Contains(t, n.Tags, "status:completed", "note %s should have status:completed tag", id)
		require.NotContains(t, n.Tags, "status:active", "note %s should not have status:active tag", id)
	}
}

func TestService_SessionStart_WithCustomMaxContextChars(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	briefing, err := svc.SessionStart(ctx, testUserID, "small-budget", 100)
	require.NoError(t, err)
	require.NotNil(t, briefing)
	require.NotNil(t, briefing.Session)
	require.Equal(t, "small-budget", briefing.Session.Name)
	require.Equal(t, StatusActive, briefing.Session.Status)
}

func TestService_SessionStart_ZeroMaxContextChars(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// maxContextChars=0 should fall back to DefaultMaxContextChars.
	briefing, err := svc.SessionStart(ctx, testUserID, "zero-budget", 0)
	require.NoError(t, err)
	require.NotNil(t, briefing)
	require.Equal(t, "zero-budget", briefing.Session.Name)
}

func TestService_SessionStart_NegativeMaxContextChars(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// maxContextChars=-1 should fall back to DefaultMaxContextChars.
	briefing, err := svc.SessionStart(ctx, testUserID, "negative-budget", -1)
	require.NoError(t, err)
	require.NotNil(t, briefing)
	require.Equal(t, "negative-budget", briefing.Session.Name)
}

func TestService_SessionPlanSet_WithoutPriorStart(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Set a plan for a session that was never started via SessionStart.
	// The note should still be created (ensureAgentMemoryProject is called internally).
	noteID, err := svc.SessionPlanSet(ctx, testUserID, "unstarted-session", "## Plan\n- Step 1")
	require.NoError(t, err)
	require.NotEmpty(t, noteID)

	n, err := svc.cfg.NoteService.Get(ctx, testUserID, noteID)
	require.NoError(t, err)
	require.Equal(t, PlanNoteTitle("unstarted-session"), n.Title)
	require.Contains(t, n.Body, "Step 1")
}

func TestService_SessionProgressUpdate_MultipleEntries(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.SessionStart(ctx, testUserID, "multi-progress", DefaultMaxContextChars)
	require.NoError(t, err)

	noteID1, err := svc.SessionProgressUpdate(ctx, testUserID, "multi-progress",
		"task-a", "in_progress", "starting A")
	require.NoError(t, err)

	noteID2, err := svc.SessionProgressUpdate(ctx, testUserID, "multi-progress",
		"task-b", "in_progress", "starting B")
	require.NoError(t, err)
	require.Equal(t, noteID1, noteID2, "all progress entries share the same note")

	noteID3, err := svc.SessionProgressUpdate(ctx, testUserID, "multi-progress",
		"task-a", "completed", "finished A")
	require.NoError(t, err)
	require.Equal(t, noteID1, noteID3)

	// Read the note and verify all entries are present.
	n, err := svc.cfg.NoteService.Get(ctx, testUserID, noteID1)
	require.NoError(t, err)
	require.Contains(t, n.Body, "task-a")
	require.Contains(t, n.Body, "task-b")
	require.Contains(t, n.Body, "starting A")
	require.Contains(t, n.Body, "starting B")
	require.Contains(t, n.Body, "finished A")
}

func TestService_SessionList_All(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Create sessions: 2 active, 1 completed.
	_, err := svc.SessionStart(ctx, testUserID, "list-all-a", DefaultMaxContextChars)
	require.NoError(t, err)
	_, err = svc.SessionStart(ctx, testUserID, "list-all-b", DefaultMaxContextChars)
	require.NoError(t, err)
	_, err = svc.SessionStart(ctx, testUserID, "list-all-c", DefaultMaxContextChars)
	require.NoError(t, err)
	err = svc.SessionEnd(ctx, testUserID, "list-all-c", "Completed C")
	require.NoError(t, err)

	// List with empty status returns all sessions.
	sessions, err := svc.SessionList(ctx, testUserID, "", 20)
	require.NoError(t, err)
	require.Len(t, sessions, 3)

	// Verify mix of statuses.
	statuses := make(map[string]int)
	for _, s := range sessions {
		statuses[s.Status]++
	}
	require.Equal(t, 2, statuses[StatusActive])
	require.Equal(t, 1, statuses[StatusCompleted])
}

func TestService_SessionList_LimitWorks(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("limit-test-%d", i)
		_, err := svc.SessionStart(ctx, testUserID, name, DefaultMaxContextChars)
		require.NoError(t, err)
	}

	sessions, err := svc.SessionList(ctx, testUserID, "", 3)
	require.NoError(t, err)
	require.Len(t, sessions, 3)
}

func TestService_MemoryList_EmptyCategory(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Ensure the agent-memory project exists but has no knowledge notes.
	_, err := svc.ensureAgentMemoryProject(ctx, testUserID)
	require.NoError(t, err)

	items, err := svc.MemoryList(ctx, testUserID, "")
	require.NoError(t, err)
	require.Empty(t, items)
}

func TestService_MemoryAppend_MultipleAppends(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.MemoryWrite(ctx, testUserID, "go", "guidelines", "Rule 1: format with gofmt")
	require.NoError(t, err)

	err = svc.MemoryAppend(ctx, testUserID, "go", "guidelines", "\nRule 2: no global state")
	require.NoError(t, err)

	err = svc.MemoryAppend(ctx, testUserID, "go", "guidelines", "\nRule 3: wrap errors")
	require.NoError(t, err)

	_, body, err := svc.MemoryRead(ctx, testUserID, "go", "guidelines")
	require.NoError(t, err)
	require.Contains(t, body, "Rule 1: format with gofmt")
	require.Contains(t, body, "Rule 2: no global state")
	require.Contains(t, body, "Rule 3: wrap errors")

	// Verify ordering: Rule 1 appears before Rule 2, Rule 2 before Rule 3.
	idx1 := strings.Index(body, "Rule 1")
	idx2 := strings.Index(body, "Rule 2")
	idx3 := strings.Index(body, "Rule 3")
	require.Less(t, idx1, idx2)
	require.Less(t, idx2, idx3)
}

func TestService_MemoryWrite_DifferentCategories(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.MemoryWrite(ctx, testUserID, "go", "concurrency", "goroutines and channels")
	require.NoError(t, err)
	_, err = svc.MemoryWrite(ctx, testUserID, "go", "testing", "table-driven tests")
	require.NoError(t, err)
	_, err = svc.MemoryWrite(ctx, testUserID, "python", "typing", "use type hints")
	require.NoError(t, err)
	_, err = svc.MemoryWrite(ctx, testUserID, "rust", "ownership", "borrow checker")
	require.NoError(t, err)

	// List by "go" category.
	goItems, err := svc.MemoryList(ctx, testUserID, "go")
	require.NoError(t, err)
	require.Len(t, goItems, 2)
	for _, item := range goItems {
		require.Equal(t, "go", item.Category)
	}

	// List by "python" category.
	pyItems, err := svc.MemoryList(ctx, testUserID, "python")
	require.NoError(t, err)
	require.Len(t, pyItems, 1)
	require.Equal(t, "python", pyItems[0].Category)

	// List by "rust" category.
	rsItems, err := svc.MemoryList(ctx, testUserID, "rust")
	require.NoError(t, err)
	require.Len(t, rsItems, 1)
	require.Equal(t, "rust", rsItems[0].Category)

	// List all.
	allItems, err := svc.MemoryList(ctx, testUserID, "")
	require.NoError(t, err)
	require.Len(t, allItems, 4)
}

func TestService_ProjectCacheWorks(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// First call creates the project and caches the ID.
	id1, err := svc.ensureAgentMemoryProject(ctx, testUserID)
	require.NoError(t, err)
	require.NotEmpty(t, id1)

	// Second call should return the same ID (from cache).
	id2, err := svc.ensureAgentMemoryProject(ctx, testUserID)
	require.NoError(t, err)
	require.Equal(t, id1, id2)

	// Verify the cache actually has the entry.
	svc.projectCacheMu.RLock()
	cachedID, ok := svc.projectCache[testUserID]
	svc.projectCacheMu.RUnlock()
	require.True(t, ok, "project ID should be cached")
	require.Equal(t, id1, cachedID)
}

func TestService_HierarchicalSession_DeepNesting(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Create 3 levels: a -> a/b -> a/b/c.
	bA, err := svc.SessionStart(ctx, testUserID, "a", DefaultMaxContextChars)
	require.NoError(t, err)
	require.Empty(t, bA.Session.ParentSessionID, "root session has no parent")

	bAB, err := svc.SessionStart(ctx, testUserID, "a/b", DefaultMaxContextChars)
	require.NoError(t, err)
	require.Equal(t, bA.Session.ID, bAB.Session.ParentSessionID, "a/b parent should be a")

	bABC, err := svc.SessionStart(ctx, testUserID, "a/b/c", DefaultMaxContextChars)
	require.NoError(t, err)
	require.Equal(t, bAB.Session.ID, bABC.Session.ParentSessionID, "a/b/c parent should be a/b")

	// Verify each session has a unique ID.
	require.NotEqual(t, bA.Session.ID, bAB.Session.ID)
	require.NotEqual(t, bAB.Session.ID, bABC.Session.ID)
	require.NotEqual(t, bA.Session.ID, bABC.Session.ID)
}

func TestService_SiblingFindings_OnlyCompletedSiblingsIncluded(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Start parent.
	_, err := svc.SessionStart(ctx, testUserID, "parent", DefaultMaxContextChars)
	require.NoError(t, err)

	// Start child-1 and complete it with findings.
	_, err = svc.SessionStart(ctx, testUserID, "parent/child-1", DefaultMaxContextChars)
	require.NoError(t, err)
	err = svc.SessionEnd(ctx, testUserID, "parent/child-1", "child-1 findings here")
	require.NoError(t, err)

	// Start child-2 but leave it active (no findings).
	_, err = svc.SessionStart(ctx, testUserID, "parent/child-2", DefaultMaxContextChars)
	require.NoError(t, err)

	// Start child-3 -- briefing should only contain child-1's findings.
	briefing, err := svc.SessionStart(ctx, testUserID, "parent/child-3", DefaultMaxContextChars)
	require.NoError(t, err)
	require.Len(t, briefing.SiblingFindings, 1, "only completed siblings should appear")
	require.Equal(t, "parent/child-1", briefing.SiblingFindings[0].SessionName)
	require.Contains(t, briefing.SiblingFindings[0].Findings, "child-1 findings here")
}

func TestService_SessionStart_BriefingContainsPlan(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.SessionStart(ctx, testUserID, "plan-briefing", DefaultMaxContextChars)
	require.NoError(t, err)

	_, err = svc.SessionPlanSet(ctx, testUserID, "plan-briefing",
		"## Plan\n- Analyze the auth module\n- Refactor middleware")
	require.NoError(t, err)

	// Resume the session to get a briefing that includes the plan.
	briefing, err := svc.SessionStart(ctx, testUserID, "plan-briefing", DefaultMaxContextChars)
	require.NoError(t, err)
	require.Contains(t, briefing.Plan, "Analyze the auth module")
	require.Contains(t, briefing.Plan, "Refactor middleware")
}

func TestService_FindSessionNote_NonexistentType(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.SessionStart(ctx, testUserID, "find-note-test", DefaultMaxContextChars)
	require.NoError(t, err)

	// The session exists but has no notes (no plan, progress, or context set).
	// Searching for any type should return ErrNotFound because no notes
	// have the session tag.
	_, err = svc.findSessionNote(ctx, testUserID, "find-note-test", "plan")
	require.ErrorIs(t, err, ErrNotFound)

	_, err = svc.findSessionNote(ctx, testUserID, "find-note-test", "progress")
	require.ErrorIs(t, err, ErrNotFound)

	_, err = svc.findSessionNote(ctx, testUserID, "find-note-test", "context")
	require.ErrorIs(t, err, ErrNotFound)

	// An unknown type also returns ErrNotFound when no notes exist.
	_, err = svc.findSessionNote(ctx, testUserID, "find-note-test", "unknown")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestService_MemoryWrite_EmptyContent(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	noteID, err := svc.MemoryWrite(ctx, testUserID, "misc", "empty-note", "")
	require.NoError(t, err)
	require.NotEmpty(t, noteID)

	_, body, err := svc.MemoryRead(ctx, testUserID, "misc", "empty-note")
	require.NoError(t, err)
	require.Empty(t, body)
}

func TestService_MultipleUsers_Isolation(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	userA := "user-a-001"
	userB := "user-b-002"

	// Write knowledge for user A.
	_, err := svc.MemoryWrite(ctx, userA, "go", "error-handling", "Wrap errors with context")
	require.NoError(t, err)

	// Verify user A can read it.
	title, body, err := svc.MemoryRead(ctx, userA, "go", "error-handling")
	require.NoError(t, err)
	require.Equal(t, KnowledgeNoteTitle("go", "error-handling"), title)
	require.Contains(t, body, "Wrap errors")

	// User B should not be able to read user A's knowledge.
	// First ensure user B's agent-memory project exists.
	_, err = svc.ensureAgentMemoryProject(ctx, userB)
	require.NoError(t, err)

	_, _, err = svc.MemoryRead(ctx, userB, "go", "error-handling")
	require.ErrorIs(t, err, ErrNotFound)
}
