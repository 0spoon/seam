package ai

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/testutil"
)

func TestTaskStore_Create_And_Get(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := NewTaskStore()
	ctx := context.Background()

	task := &Task{
		ID:        "task-001",
		Type:      TaskTypeEmbed,
		Priority:  PriorityBackground,
		Status:    TaskStatusPending,
		Payload:   json.RawMessage(`{"note_id":"note-001"}`),
		CreatedAt: time.Now().UTC(),
	}

	err := store.Create(ctx, db, task)
	require.NoError(t, err)

	got, err := store.Get(ctx, db, "task-001")
	require.NoError(t, err)
	require.Equal(t, "task-001", got.ID)
	require.Equal(t, TaskTypeEmbed, got.Type)
	require.Equal(t, PriorityBackground, got.Priority)
	require.Equal(t, TaskStatusPending, got.Status)
	require.JSONEq(t, `{"note_id":"note-001"}`, string(got.Payload))
}

func TestTaskStore_Get_NotFound(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := NewTaskStore()

	_, err := store.Get(context.Background(), db, "nonexistent")
	require.ErrorIs(t, err, ErrTaskNotFound)
}

func TestTaskStore_UpdateStatus(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := NewTaskStore()
	ctx := context.Background()

	task := &Task{
		ID:        "task-002",
		Type:      TaskTypeChat,
		Priority:  PriorityInteractive,
		Status:    TaskStatusPending,
		Payload:   json.RawMessage(`{"query":"test"}`),
		CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, store.Create(ctx, db, task))

	// Mark as running.
	err := store.UpdateStatus(ctx, db, "task-002", TaskStatusRunning, nil, "")
	require.NoError(t, err)

	got, err := store.Get(ctx, db, "task-002")
	require.NoError(t, err)
	require.Equal(t, TaskStatusRunning, got.Status)
	require.False(t, got.StartedAt.IsZero())

	// Mark as done with result.
	result := json.RawMessage(`{"answer":"42"}`)
	err = store.UpdateStatus(ctx, db, "task-002", TaskStatusDone, result, "")
	require.NoError(t, err)

	got, err = store.Get(ctx, db, "task-002")
	require.NoError(t, err)
	require.Equal(t, TaskStatusDone, got.Status)
	require.JSONEq(t, `{"answer":"42"}`, string(got.Result))
	require.False(t, got.FinishedAt.IsZero())
}

func TestTaskStore_UpdateStatus_Failed(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := NewTaskStore()
	ctx := context.Background()

	task := &Task{
		ID:        "task-003",
		Type:      TaskTypeEmbed,
		Priority:  PriorityBackground,
		Status:    TaskStatusPending,
		Payload:   json.RawMessage(`{}`),
		CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, store.Create(ctx, db, task))

	err := store.UpdateStatus(ctx, db, "task-003", TaskStatusFailed, nil, "connection refused")
	require.NoError(t, err)

	got, err := store.Get(ctx, db, "task-003")
	require.NoError(t, err)
	require.Equal(t, TaskStatusFailed, got.Status)
	require.Equal(t, "connection refused", got.Error)
}

func TestTaskStore_UpdateStatus_NotFound(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := NewTaskStore()

	err := store.UpdateStatus(context.Background(), db, "nonexistent", TaskStatusDone, nil, "")
	require.ErrorIs(t, err, ErrTaskNotFound)
}

func TestTaskStore_ListPending(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := NewTaskStore()
	ctx := context.Background()

	now := time.Now().UTC()

	// Insert tasks in different states and priorities.
	tasks := []*Task{
		{ID: "t1", Type: TaskTypeEmbed, Priority: PriorityBackground, Status: TaskStatusPending,
			Payload: json.RawMessage(`{}`), CreatedAt: now.Add(-3 * time.Minute)},
		{ID: "t2", Type: TaskTypeChat, Priority: PriorityInteractive, Status: TaskStatusPending,
			Payload: json.RawMessage(`{}`), CreatedAt: now.Add(-2 * time.Minute)},
		{ID: "t3", Type: TaskTypeEmbed, Priority: PriorityBackground, Status: TaskStatusDone,
			Payload: json.RawMessage(`{}`), CreatedAt: now.Add(-1 * time.Minute)},
		{ID: "t4", Type: TaskTypeSynthesize, Priority: PriorityUserTriggered, Status: TaskStatusRunning,
			Payload: json.RawMessage(`{}`), CreatedAt: now},
	}
	for _, task := range tasks {
		require.NoError(t, store.Create(ctx, db, task))
	}

	pending, err := store.ListPending(ctx, db)
	require.NoError(t, err)
	require.Len(t, pending, 3) // t1, t2, t4 (pending or running)

	// Should be ordered by priority (asc) then created_at (asc).
	require.Equal(t, "t2", pending[0].ID) // priority 0 (interactive)
	require.Equal(t, "t4", pending[1].ID) // priority 1 (user-triggered)
	require.Equal(t, "t1", pending[2].ID) // priority 2 (background)
}
