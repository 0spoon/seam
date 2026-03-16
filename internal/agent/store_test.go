package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/testutil"
)

func TestSQLStore_CreateSession_Success(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	s := &Session{
		ID:        "01TEST00000000000000000001",
		Name:      "refactor-auth",
		Status:    StatusActive,
		Metadata:  Metadata{AgentName: "claude-code"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	err := store.CreateSession(ctx, db, s)
	require.NoError(t, err)

	got, err := store.GetSession(ctx, db, s.ID)
	require.NoError(t, err)
	require.Equal(t, s.ID, got.ID)
	require.Equal(t, s.Name, got.Name)
	require.Equal(t, StatusActive, got.Status)
	require.Equal(t, "claude-code", got.Metadata.AgentName)
	require.Empty(t, got.ParentSessionID)
	require.Empty(t, got.Findings)
}

func TestSQLStore_CreateSession_DuplicateName(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	s1 := &Session{
		ID: "01TEST00000000000000000001", Name: "duplicate",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	s2 := &Session{
		ID: "01TEST00000000000000000002", Name: "duplicate",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}

	err := store.CreateSession(ctx, db, s1)
	require.NoError(t, err)

	err = store.CreateSession(ctx, db, s2)
	require.Error(t, err)
}

func TestSQLStore_CreateSession_WithParent(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	parent := &Session{
		ID: "01TEST00000000000000000001", Name: "parent-session",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	child := &Session{
		ID: "01TEST00000000000000000002", Name: "parent-session/child",
		ParentSessionID: parent.ID,
		Status:          StatusActive, CreatedAt: now, UpdatedAt: now,
	}

	require.NoError(t, store.CreateSession(ctx, db, parent))
	require.NoError(t, store.CreateSession(ctx, db, child))

	got, err := store.GetSession(ctx, db, child.ID)
	require.NoError(t, err)
	require.Equal(t, parent.ID, got.ParentSessionID)
}

func TestSQLStore_GetSession_NotFound(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	_, err := store.GetSession(ctx, db, "NONEXISTENT")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestSQLStore_GetSessionByName_Success(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	s := &Session{
		ID: "01TEST00000000000000000001", Name: "find-by-name",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, s))

	got, err := store.GetSessionByName(ctx, db, "find-by-name")
	require.NoError(t, err)
	require.Equal(t, s.ID, got.ID)
}

func TestSQLStore_GetSessionByName_NotFound(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	_, err := store.GetSessionByName(ctx, db, "nonexistent")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestSQLStore_UpdateSession_Success(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	s := &Session{
		ID: "01TEST00000000000000000001", Name: "update-me",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, s))

	s.Status = StatusCompleted
	s.Findings = "Found important things"
	s.UpdatedAt = now.Add(time.Minute)
	require.NoError(t, store.UpdateSession(ctx, db, s))

	got, err := store.GetSession(ctx, db, s.ID)
	require.NoError(t, err)
	require.Equal(t, StatusCompleted, got.Status)
	require.Equal(t, "Found important things", got.Findings)
}

func TestSQLStore_UpdateSession_NotFound(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	s := &Session{
		ID: "NONEXISTENT", Name: "x", Status: StatusActive,
		UpdatedAt: time.Now().UTC(),
	}
	err := store.UpdateSession(ctx, db, s)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestSQLStore_ListSessions_FilterByStatus(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	active := &Session{
		ID: "01TEST00000000000000000001", Name: "active-one",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	completed := &Session{
		ID: "01TEST00000000000000000002", Name: "done-one",
		Status: StatusCompleted, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	}
	require.NoError(t, store.CreateSession(ctx, db, active))
	require.NoError(t, store.CreateSession(ctx, db, completed))

	// List only active.
	sessions, err := store.ListSessions(ctx, db, StatusActive, 10, 0)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, "active-one", sessions[0].Name)

	// List only completed.
	sessions, err = store.ListSessions(ctx, db, StatusCompleted, 10, 0)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, "done-one", sessions[0].Name)
}

func TestSQLStore_ListSessions_All(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	for i, name := range []string{"s1", "s2", "s3"} {
		s := &Session{
			ID: "01TEST0000000000000000000" + string(rune('1'+i)), Name: name,
			Status: StatusActive, CreatedAt: now.Add(time.Duration(i) * time.Second),
			UpdatedAt: now.Add(time.Duration(i) * time.Second),
		}
		require.NoError(t, store.CreateSession(ctx, db, s))
	}

	// List all (empty status).
	sessions, err := store.ListSessions(ctx, db, "", 10, 0)
	require.NoError(t, err)
	require.Len(t, sessions, 3)
}

func TestSQLStore_ListSessions_Pagination(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 5; i++ {
		s := &Session{
			ID:        "01TEST000000000000000000" + string(rune('A'+i)),
			Name:      "sess-" + string(rune('a'+i)),
			Status:    StatusActive,
			CreatedAt: now.Add(time.Duration(i) * time.Second),
			UpdatedAt: now.Add(time.Duration(i) * time.Second),
		}
		require.NoError(t, store.CreateSession(ctx, db, s))
	}

	page1, err := store.ListSessions(ctx, db, "", 2, 0)
	require.NoError(t, err)
	require.Len(t, page1, 2)

	page2, err := store.ListSessions(ctx, db, "", 2, 2)
	require.NoError(t, err)
	require.Len(t, page2, 2)

	// Pages should have different sessions.
	require.NotEqual(t, page1[0].ID, page2[0].ID)
}

func TestSQLStore_ListChildSessions(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	parent := &Session{
		ID: "01TESTPARENT0000000000001", Name: "parent",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	child1 := &Session{
		ID: "01TESTCHILD00000000000001", Name: "parent/child-a",
		ParentSessionID: parent.ID,
		Status:          StatusCompleted, Findings: "Child A done",
		CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	}
	child2 := &Session{
		ID: "01TESTCHILD00000000000002", Name: "parent/child-b",
		ParentSessionID: parent.ID,
		Status:          StatusActive,
		CreatedAt:       now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second),
	}
	unrelated := &Session{
		ID: "01TESTOTHER00000000000001", Name: "other-session",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}

	require.NoError(t, store.CreateSession(ctx, db, parent))
	require.NoError(t, store.CreateSession(ctx, db, child1))
	require.NoError(t, store.CreateSession(ctx, db, child2))
	require.NoError(t, store.CreateSession(ctx, db, unrelated))

	children, err := store.ListChildSessions(ctx, db, parent.ID)
	require.NoError(t, err)
	require.Len(t, children, 2)
}

func TestSQLStore_ReconcileChildren(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	// Create orphan children first (started before parent).
	orphan1 := &Session{
		ID: "01TESTORPHAN0000000000001", Name: "late-parent/orphan-a",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	orphan2 := &Session{
		ID: "01TESTORPHAN0000000000002", Name: "late-parent/orphan-b",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	// A non-child session with a similar name prefix but not a child.
	notChild := &Session{
		ID: "01TESTNOTCHILD00000000001", Name: "late-parent-extra",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}

	require.NoError(t, store.CreateSession(ctx, db, orphan1))
	require.NoError(t, store.CreateSession(ctx, db, orphan2))
	require.NoError(t, store.CreateSession(ctx, db, notChild))

	// Now the parent session starts late.
	parent := &Session{
		ID: "01TESTPARENT0000000000001", Name: "late-parent",
		Status: StatusActive, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute),
	}
	require.NoError(t, store.CreateSession(ctx, db, parent))

	// Reconcile orphan children.
	reconciled, err := store.ReconcileChildren(ctx, db, parent.ID, parent.Name)
	require.NoError(t, err)
	require.Equal(t, int64(2), reconciled)

	// Verify orphans now have the parent.
	got1, err := store.GetSession(ctx, db, orphan1.ID)
	require.NoError(t, err)
	require.Equal(t, parent.ID, got1.ParentSessionID)

	got2, err := store.GetSession(ctx, db, orphan2.ID)
	require.NoError(t, err)
	require.Equal(t, parent.ID, got2.ParentSessionID)

	// The non-child should NOT be linked.
	notChildGot, err := store.GetSession(ctx, db, notChild.ID)
	require.NoError(t, err)
	require.Empty(t, notChildGot.ParentSessionID)
}

func TestSQLStore_ReconcileChildren_SkipsAlreadyLinked(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	parent := &Session{
		ID: "01TESTPARENT0000000000001", Name: "p",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	otherParent := &Session{
		ID: "01TESTOTHER00000000000001", Name: "other",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	child := &Session{
		ID: "01TESTCHILD00000000000001", Name: "p/already-linked",
		ParentSessionID: otherParent.ID,
		Status:          StatusActive, CreatedAt: now, UpdatedAt: now,
	}

	require.NoError(t, store.CreateSession(ctx, db, parent))
	require.NoError(t, store.CreateSession(ctx, db, otherParent))
	require.NoError(t, store.CreateSession(ctx, db, child))

	// Reconcile should not touch already-linked children.
	reconciled, err := store.ReconcileChildren(ctx, db, parent.ID, parent.Name)
	require.NoError(t, err)
	require.Equal(t, int64(0), reconciled)

	got, err := store.GetSession(ctx, db, child.ID)
	require.NoError(t, err)
	require.Equal(t, otherParent.ID, got.ParentSessionID)
}

func TestSQLStore_LogToolCall_Success(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	// Create a session for the tool call.
	s := &Session{
		ID: "01TESTSESSION000000000001", Name: "tool-test",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, s))

	tc := &ToolCallRecord{
		ID:         "01TESTTOOL000000000000001",
		SessionID:  s.ID,
		ToolName:   "session_start",
		Arguments:  `{"name":"test"}`,
		Result:     `{"status":"ok"}`,
		DurationMs: 42,
		CreatedAt:  now,
	}
	err := store.LogToolCall(ctx, db, tc)
	require.NoError(t, err)

	calls, err := store.ListToolCalls(ctx, db, s.ID, 10)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	require.Equal(t, "session_start", calls[0].ToolName)
	require.Equal(t, `{"name":"test"}`, calls[0].Arguments)
	require.Equal(t, `{"status":"ok"}`, calls[0].Result)
	require.Equal(t, int64(42), calls[0].DurationMs)
}

func TestSQLStore_LogToolCall_WithoutSession(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	// Tool calls without a session (e.g., notes_search outside a session).
	tc := &ToolCallRecord{
		ID:        "01TESTTOOL000000000000001",
		ToolName:  "notes_search",
		Arguments: `{"query":"test"}`,
		CreatedAt: now,
	}
	err := store.LogToolCall(ctx, db, tc)
	require.NoError(t, err)
}

func TestSQLStore_LogToolCall_WithError(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	s := &Session{
		ID: "01TESTSESSION000000000001", Name: "error-test",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, s))

	tc := &ToolCallRecord{
		ID:         "01TESTTOOL000000000000001",
		SessionID:  s.ID,
		ToolName:   "memory_read",
		Arguments:  `{"category":"go","name":"patterns"}`,
		Error:      "not found",
		DurationMs: 5,
		CreatedAt:  now,
	}
	err := store.LogToolCall(ctx, db, tc)
	require.NoError(t, err)

	calls, err := store.ListToolCalls(ctx, db, s.ID, 10)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	require.Equal(t, "not found", calls[0].Error)
	require.Empty(t, calls[0].Result)
}

func TestSQLStore_ListToolCalls_LimitAndOrder(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	s := &Session{
		ID: "01TESTSESSION000000000001", Name: "limit-test",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, s))

	for i := 0; i < 5; i++ {
		tc := &ToolCallRecord{
			ID:        "01TESTTOOL00000000000000" + string(rune('A'+i)),
			SessionID: s.ID,
			ToolName:  "tool_" + string(rune('a'+i)),
			Arguments: "{}",
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		}
		require.NoError(t, store.LogToolCall(ctx, db, tc))
	}

	// Limit to 3 -- should return the 3 most recent.
	calls, err := store.ListToolCalls(ctx, db, s.ID, 3)
	require.NoError(t, err)
	require.Len(t, calls, 3)
	// Most recent first (DESC order).
	require.Equal(t, "tool_e", calls[0].ToolName)
}

func TestSQLStore_CascadeDelete_ToolCallsOnSessionDelete(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	s := &Session{
		ID: "01TESTSESSION000000000001", Name: "cascade-test",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, s))

	tc := &ToolCallRecord{
		ID: "01TESTTOOL000000000000001", SessionID: s.ID,
		ToolName: "test_tool", Arguments: "{}", CreatedAt: now,
	}
	require.NoError(t, store.LogToolCall(ctx, db, tc))

	// Delete the session directly via SQL (simulating cascade).
	_, err := db.ExecContext(ctx, "DELETE FROM agent_sessions WHERE id = ?", s.ID)
	require.NoError(t, err)

	// Tool calls should be cascade-deleted.
	calls, err := store.ListToolCalls(ctx, db, s.ID, 10)
	require.NoError(t, err)
	require.Empty(t, calls)
}

func TestSQLStore_ParentDeleteSetsNull(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	parent := &Session{
		ID: "01TESTPARENT0000000000001", Name: "will-delete",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	child := &Session{
		ID: "01TESTCHILD00000000000001", Name: "will-delete/child",
		ParentSessionID: parent.ID,
		Status:          StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, parent))
	require.NoError(t, store.CreateSession(ctx, db, child))

	// Delete parent via SQL.
	_, err := db.ExecContext(ctx, "DELETE FROM agent_sessions WHERE id = ?", parent.ID)
	require.NoError(t, err)

	// Child should still exist with NULL parent.
	got, err := store.GetSession(ctx, db, child.ID)
	require.NoError(t, err)
	require.Empty(t, got.ParentSessionID)
}

func TestSQLStore_CreateSession_MetadataRoundTrip(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	tests := []struct {
		name     string
		id       string
		sessName string
		meta     Metadata
		wantName string
	}{
		{
			name:     "populated metadata",
			id:       "01TESTMETA000000000000001",
			sessName: "meta-populated",
			meta:     Metadata{AgentName: "test-agent"},
			wantName: "test-agent",
		},
		{
			name:     "empty metadata",
			id:       "01TESTMETA000000000000002",
			sessName: "meta-empty",
			meta:     Metadata{},
			wantName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Session{
				ID:        tt.id,
				Name:      tt.sessName,
				Status:    StatusActive,
				Metadata:  tt.meta,
				CreatedAt: now,
				UpdatedAt: now,
			}
			require.NoError(t, store.CreateSession(ctx, db, s))

			got, err := store.GetSession(ctx, db, s.ID)
			require.NoError(t, err)
			require.Equal(t, tt.wantName, got.Metadata.AgentName)
		})
	}
}

func TestSQLStore_CreateSession_WithFindings(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	s := &Session{
		ID:        "01TESTFINDINGS0000000001",
		Name:      "findings-test",
		Status:    StatusCompleted,
		Findings:  "Discovered 3 critical bugs in the auth module.",
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, s))

	got, err := store.GetSession(ctx, db, s.ID)
	require.NoError(t, err)
	require.Equal(t, "Discovered 3 critical bugs in the auth module.", got.Findings)
}

func TestSQLStore_UpdateSession_MetadataUpdate(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	s := &Session{
		ID:        "01TESTMETAUPD00000000001",
		Name:      "meta-update",
		Status:    StatusActive,
		Metadata:  Metadata{AgentName: "original-agent"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, s))

	// Update metadata to a new agent name.
	s.Metadata = Metadata{AgentName: "updated-agent"}
	s.UpdatedAt = now.Add(time.Minute)
	require.NoError(t, store.UpdateSession(ctx, db, s))

	got, err := store.GetSession(ctx, db, s.ID)
	require.NoError(t, err)
	require.Equal(t, "updated-agent", got.Metadata.AgentName)
}

func TestSQLStore_ListSessions_OrderByUpdatedAt(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	base := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

	// Create 3 sessions with distinct updated_at timestamps.
	// Intentionally insert out of order to verify sorting.
	sessions := []struct {
		id   string
		name string
		at   time.Time
	}{
		{"01TESTORDER00000000000002", "middle", base.Add(1 * time.Hour)},
		{"01TESTORDER00000000000003", "newest", base.Add(2 * time.Hour)},
		{"01TESTORDER00000000000001", "oldest", base},
	}

	for _, ss := range sessions {
		s := &Session{
			ID: ss.id, Name: ss.name,
			Status: StatusActive, CreatedAt: ss.at, UpdatedAt: ss.at,
		}
		require.NoError(t, store.CreateSession(ctx, db, s))
	}

	got, err := store.ListSessions(ctx, db, "", 10, 0)
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, "newest", got[0].Name)
	require.Equal(t, "middle", got[1].Name)
	require.Equal(t, "oldest", got[2].Name)
}

func TestSQLStore_ListSessions_EmptyResult(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	s := &Session{
		ID: "01TESTEMPTY000000000000001", Name: "active-only",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, s))

	// Query for a status with no matching sessions.
	got, err := store.ListSessions(ctx, db, StatusArchived, 10, 0)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestSQLStore_ListChildSessions_NoChildren(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	parent := &Session{
		ID: "01TESTNOCHILD00000000001", Name: "lonely-parent",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, parent))

	children, err := store.ListChildSessions(ctx, db, parent.ID)
	require.NoError(t, err)
	require.Empty(t, children)
}

func TestSQLStore_ListChildSessions_OnlyCompletedSiblings(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	parent := &Session{
		ID: "01TESTMIXED00000000000001", Name: "mixed-parent",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, parent))

	child1 := &Session{
		ID: "01TESTMIXED00000000000002", Name: "mixed-parent/done-a",
		ParentSessionID: parent.ID,
		Status:          StatusCompleted, Findings: "Done A",
		CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	}
	child2 := &Session{
		ID: "01TESTMIXED00000000000003", Name: "mixed-parent/done-b",
		ParentSessionID: parent.ID,
		Status:          StatusCompleted, Findings: "Done B",
		CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second),
	}
	child3 := &Session{
		ID: "01TESTMIXED00000000000004", Name: "mixed-parent/still-active",
		ParentSessionID: parent.ID,
		Status:          StatusActive,
		CreatedAt:       now.Add(3 * time.Second), UpdatedAt: now.Add(3 * time.Second),
	}

	require.NoError(t, store.CreateSession(ctx, db, child1))
	require.NoError(t, store.CreateSession(ctx, db, child2))
	require.NoError(t, store.CreateSession(ctx, db, child3))

	// ListChildSessions returns ALL children regardless of status.
	children, err := store.ListChildSessions(ctx, db, parent.ID)
	require.NoError(t, err)
	require.Len(t, children, 3)

	// Verify order is by created_at ASC.
	require.Equal(t, "mixed-parent/done-a", children[0].Name)
	require.Equal(t, "mixed-parent/done-b", children[1].Name)
	require.Equal(t, "mixed-parent/still-active", children[2].Name)
}

func TestSQLStore_ReconcileChildren_NoOrphans(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	parent := &Session{
		ID: "01TESTNOORPHAN0000000001", Name: "no-orphans",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, parent))

	reconciled, err := store.ReconcileChildren(ctx, db, parent.ID, parent.Name)
	require.NoError(t, err)
	require.Equal(t, int64(0), reconciled)
}

func TestSQLStore_ReconcileChildren_DeepHierarchy(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	// Create orphans with deep hierarchical names before the parent.
	orphanC := &Session{
		ID: "01TESTDEEP000000000000001", Name: "a/b/c",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	orphanD := &Session{
		ID: "01TESTDEEP000000000000002", Name: "a/b/d",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, orphanC))
	require.NoError(t, store.CreateSession(ctx, db, orphanD))

	// Create the intermediate parent "a/b".
	parentAB := &Session{
		ID: "01TESTDEEP000000000000003", Name: "a/b",
		Status: StatusActive, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute),
	}
	require.NoError(t, store.CreateSession(ctx, db, parentAB))

	// Reconcile "a/b" -- should pick up both orphans.
	reconciled, err := store.ReconcileChildren(ctx, db, parentAB.ID, parentAB.Name)
	require.NoError(t, err)
	require.Equal(t, int64(2), reconciled)

	// Verify both orphans are linked to "a/b".
	gotC, err := store.GetSession(ctx, db, orphanC.ID)
	require.NoError(t, err)
	require.Equal(t, parentAB.ID, gotC.ParentSessionID)

	gotD, err := store.GetSession(ctx, db, orphanD.ID)
	require.NoError(t, err)
	require.Equal(t, parentAB.ID, gotD.ParentSessionID)

	// Now create the root parent "a".
	parentA := &Session{
		ID: "01TESTDEEP000000000000004", Name: "a",
		Status: StatusActive, CreatedAt: now.Add(2 * time.Minute), UpdatedAt: now.Add(2 * time.Minute),
	}
	require.NoError(t, store.CreateSession(ctx, db, parentA))

	// Reconcile "a" -- "a/b" itself is a direct child name match,
	// but "a/b/c" and "a/b/d" also match "a/%" LIKE pattern.
	// However, "a/b/c" and "a/b/d" already have parent_session_id set (to "a/b"),
	// so only "a/b" (which has NULL parent) should be reconciled.
	reconciled, err = store.ReconcileChildren(ctx, db, parentA.ID, parentA.Name)
	require.NoError(t, err)
	require.Equal(t, int64(1), reconciled)

	gotAB, err := store.GetSession(ctx, db, parentAB.ID)
	require.NoError(t, err)
	require.Equal(t, parentA.ID, gotAB.ParentSessionID)
}

func TestSQLStore_ReconcileChildren_SkipsGrandchildren(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	// Create an orphan child and an orphan grandchild before the parent.
	orphanChild := &Session{
		ID: "01TESTGRAND0000000000001", Name: "root/child",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	orphanGrandchild := &Session{
		ID: "01TESTGRAND0000000000002", Name: "root/child/grandchild",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, orphanChild))
	require.NoError(t, store.CreateSession(ctx, db, orphanGrandchild))

	// Create the parent "root".
	parent := &Session{
		ID: "01TESTGRAND0000000000003", Name: "root",
		Status: StatusActive, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute),
	}
	require.NoError(t, store.CreateSession(ctx, db, parent))

	// Reconcile "root" -- should only pick up direct child "root/child",
	// NOT grandchild "root/child/grandchild".
	reconciled, err := store.ReconcileChildren(ctx, db, parent.ID, parent.Name)
	require.NoError(t, err)
	require.Equal(t, int64(1), reconciled, "should only reconcile direct child, not grandchild")

	// Verify child is linked to parent.
	gotChild, err := store.GetSession(ctx, db, orphanChild.ID)
	require.NoError(t, err)
	require.Equal(t, parent.ID, gotChild.ParentSessionID)

	// Verify grandchild is NOT linked to parent (still orphaned).
	gotGrandchild, err := store.GetSession(ctx, db, orphanGrandchild.ID)
	require.NoError(t, err)
	require.Empty(t, gotGrandchild.ParentSessionID, "grandchild should not be reconciled to grandparent")
}

func TestSQLStore_LogToolCall_AllFields(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	s := &Session{
		ID: "01TESTALLFIELDS000000001", Name: "all-fields",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, s))

	tc := &ToolCallRecord{
		ID:         "01TESTTOOLALL00000000001",
		SessionID:  s.ID,
		ToolName:   "notes_create",
		Arguments:  `{"title":"Test Note","body":"Hello world"}`,
		Result:     `{"id":"01NOTE00000000000000001","title":"Test Note"}`,
		Error:      "",
		DurationMs: 123,
		CreatedAt:  now,
	}
	require.NoError(t, store.LogToolCall(ctx, db, tc))

	calls, err := store.ListToolCalls(ctx, db, s.ID, 10)
	require.NoError(t, err)
	require.Len(t, calls, 1)

	got := calls[0]
	require.Equal(t, tc.ID, got.ID)
	require.Equal(t, tc.SessionID, got.SessionID)
	require.Equal(t, "notes_create", got.ToolName)
	require.Equal(t, tc.Arguments, got.Arguments)
	require.Equal(t, tc.Result, got.Result)
	require.Empty(t, got.Error)
	require.Equal(t, int64(123), got.DurationMs)
	require.Equal(t, now, got.CreatedAt)
}

func TestSQLStore_ListToolCalls_EmptyForSession(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	s := &Session{
		ID: "01TESTEMPTYTC00000000001", Name: "no-tool-calls",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, s))

	calls, err := store.ListToolCalls(ctx, db, s.ID, 10)
	require.NoError(t, err)
	require.Empty(t, calls)
}

func TestSQLStore_ListToolCalls_DefaultLimit(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	s := &Session{
		ID: "01TESTDEFLIMIT0000000001", Name: "default-limit",
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(ctx, db, s))

	// Insert 55 tool calls.
	for i := 0; i < 55; i++ {
		tc := &ToolCallRecord{
			ID:        fmt.Sprintf("01TESTDEFTOOL%013d", i),
			SessionID: s.ID,
			ToolName:  fmt.Sprintf("tool_%03d", i),
			Arguments: "{}",
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		}
		require.NoError(t, store.LogToolCall(ctx, db, tc))
	}

	// Limit 0 should default to 50.
	calls, err := store.ListToolCalls(ctx, db, s.ID, 0)
	require.NoError(t, err)
	require.Len(t, calls, 50)

	// Negative limit should also default to 50.
	calls, err = store.ListToolCalls(ctx, db, s.ID, -1)
	require.NoError(t, err)
	require.Len(t, calls, 50)
}

func TestSQLStore_GetSession_TimestampPrecision(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	// Use a specific timestamp truncated to second (RFC3339 precision).
	created := time.Date(2025, 3, 15, 10, 30, 45, 0, time.UTC)
	updated := time.Date(2025, 3, 15, 11, 0, 0, 0, time.UTC)

	s := &Session{
		ID:        "01TESTTSTAMP0000000000001",
		Name:      "timestamp-precision",
		Status:    StatusActive,
		CreatedAt: created,
		UpdatedAt: updated,
	}
	require.NoError(t, store.CreateSession(ctx, db, s))

	got, err := store.GetSession(ctx, db, s.ID)
	require.NoError(t, err)
	require.True(t, created.Equal(got.CreatedAt),
		"created_at mismatch: want %v, got %v", created, got.CreatedAt)
	require.True(t, updated.Equal(got.UpdatedAt),
		"updated_at mismatch: want %v, got %v", updated, got.UpdatedAt)
}

func TestSQLStore_CreateSession_InvalidParentFK(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewSQLStore()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	s := &Session{
		ID:              "01TESTBADFK00000000000001",
		Name:            "orphan-bad-fk",
		ParentSessionID: "NONEXISTENT_PARENT_ID",
		Status:          StatusActive,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	err := store.CreateSession(ctx, db, s)
	require.Error(t, err)
}
