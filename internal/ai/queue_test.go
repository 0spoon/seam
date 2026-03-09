package ai

import (
	"container/heap"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPriorityQueue_Ordering(t *testing.T) {
	now := time.Now()

	pq := make(priorityQueue, 0)
	heap.Init(&pq)

	// Push items with different priorities.
	items := []*pqItem{
		{task: &Task{ID: "bg1", Priority: PriorityBackground, CreatedAt: now.Add(-1 * time.Minute)}, priority: PriorityBackground},
		{task: &Task{ID: "interactive1", Priority: PriorityInteractive, CreatedAt: now}, priority: PriorityInteractive},
		{task: &Task{ID: "user1", Priority: PriorityUserTriggered, CreatedAt: now.Add(-2 * time.Minute)}, priority: PriorityUserTriggered},
		{task: &Task{ID: "interactive2", Priority: PriorityInteractive, CreatedAt: now.Add(-3 * time.Minute)}, priority: PriorityInteractive},
	}

	for _, item := range items {
		heap.Push(&pq, item)
	}

	// Pop in priority order; within same priority, FIFO (earlier created first).
	first := heap.Pop(&pq).(*pqItem)
	require.Equal(t, "interactive2", first.task.ID) // priority 0, earlier
	second := heap.Pop(&pq).(*pqItem)
	require.Equal(t, "interactive1", second.task.ID) // priority 0, later
	third := heap.Pop(&pq).(*pqItem)
	require.Equal(t, "user1", third.task.ID) // priority 1
	fourth := heap.Pop(&pq).(*pqItem)
	require.Equal(t, "bg1", fourth.task.ID) // priority 2
}

func TestQueue_Enqueue_And_Process(t *testing.T) {
	store := NewTaskStore()
	mockMgr := newMockDBManager()
	q := NewQueue(store, mockMgr, nil, 1, nil)

	// Use a channel to synchronize instead of time.Sleep.
	processed := make(chan string, 10)

	q.RegisterHandler(TaskTypeEmbed, func(ctx context.Context, task *Task) (json.RawMessage, error) {
		processed <- task.ID
		return json.RawMessage(`{"ok":true}`), nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start queue processing in background.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		q.Run(ctx)
	}()

	// Enqueue a task.
	task := &Task{
		ID:       "test-task-1",
		UserID:   "user1",
		Type:     TaskTypeEmbed,
		Priority: PriorityBackground,
		Payload:  json.RawMessage(`{"note_id":"n1"}`),
	}
	err := q.Enqueue(ctx, task)
	require.NoError(t, err)

	// Wait for task to be processed via channel.
	select {
	case id := <-processed:
		require.Equal(t, "test-task-1", id)
	case <-time.After(5 * time.Second):
		t.Fatal("task was not processed within timeout")
	}

	// Verify task is marked done in DB.
	db, _ := mockMgr.Open(ctx, "user1")
	got, err := store.Get(ctx, db, "test-task-1")
	require.NoError(t, err)
	require.Equal(t, TaskStatusDone, got.Status)

	cancel()
	wg.Wait()
}

func TestQueue_PriorityExecution(t *testing.T) {
	store := NewTaskStore()
	mockMgr := newMockDBManager()
	q := NewQueue(store, mockMgr, nil, 1, nil)

	// Use a channel to track processing order.
	orderCh := make(chan string, 10)

	q.RegisterHandler(TaskTypeEmbed, func(ctx context.Context, task *Task) (json.RawMessage, error) {
		orderCh <- task.ID
		return nil, nil
	})
	q.RegisterHandler(TaskTypeChat, func(ctx context.Context, task *Task) (json.RawMessage, error) {
		orderCh <- task.ID
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Enqueue tasks at different priorities BEFORE starting the queue.
	bgTask := &Task{
		ID: "bg-task", UserID: "user1", Type: TaskTypeEmbed,
		Priority: PriorityBackground, Payload: json.RawMessage(`{}`),
		CreatedAt: time.Now().Add(-1 * time.Second),
	}
	intTask := &Task{
		ID: "int-task", UserID: "user1", Type: TaskTypeChat,
		Priority: PriorityInteractive, Payload: json.RawMessage(`{}`),
		CreatedAt: time.Now(),
	}

	// Directly push to in-memory queue without persisting (to control timing).
	q.mu.Lock()
	heap.Push(&q.pq, &pqItem{task: bgTask, priority: bgTask.Priority})
	heap.Push(&q.pq, &pqItem{task: intTask, priority: intTask.Priority})
	q.mu.Unlock()

	// Also persist them so processTask can update status.
	db, _ := mockMgr.Open(ctx, "user1")
	bgTask.Status = TaskStatusPending
	intTask.Status = TaskStatusPending
	store.Create(ctx, db, bgTask)
	store.Create(ctx, db, intTask)

	// Signal the queue.
	select {
	case q.notify <- struct{}{}:
	default:
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		q.Run(ctx)
	}()

	// Collect the processing order via channels.
	var order []string
	for i := 0; i < 2; i++ {
		select {
		case id := <-orderCh:
			order = append(order, id)
		case <-time.After(5 * time.Second):
			t.Fatal("task processing timed out")
		}
	}

	// Interactive task should be processed before background task.
	require.Equal(t, []string{"int-task", "bg-task"}, order)

	cancel()
	wg.Wait()
}

func TestQueue_FairScheduling(t *testing.T) {
	store := NewTaskStore()
	mockMgr := newMockDBManager()
	q := NewQueue(store, mockMgr, nil, 1, nil)

	orderCh := make(chan string, 20)

	q.RegisterHandler(TaskTypeEmbed, func(ctx context.Context, task *Task) (json.RawMessage, error) {
		orderCh <- task.ID
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Now()

	// Create tasks: 3 for user A, 1 for user B, all at the same priority.
	tasks := []*Task{
		{ID: "a1", UserID: "userA", Type: TaskTypeEmbed, Priority: PriorityBackground,
			Payload: json.RawMessage(`{}`), Status: TaskStatusPending, CreatedAt: now.Add(-4 * time.Second)},
		{ID: "a2", UserID: "userA", Type: TaskTypeEmbed, Priority: PriorityBackground,
			Payload: json.RawMessage(`{}`), Status: TaskStatusPending, CreatedAt: now.Add(-3 * time.Second)},
		{ID: "b1", UserID: "userB", Type: TaskTypeEmbed, Priority: PriorityBackground,
			Payload: json.RawMessage(`{}`), Status: TaskStatusPending, CreatedAt: now.Add(-2 * time.Second)},
		{ID: "a3", UserID: "userA", Type: TaskTypeEmbed, Priority: PriorityBackground,
			Payload: json.RawMessage(`{}`), Status: TaskStatusPending, CreatedAt: now.Add(-1 * time.Second)},
	}

	// Persist and push to in-memory queue.
	for _, task := range tasks {
		db, _ := mockMgr.Open(ctx, task.UserID)
		store.Create(ctx, db, task)
		q.mu.Lock()
		heap.Push(&q.pq, &pqItem{task: task, priority: task.Priority})
		q.mu.Unlock()
	}

	// Signal the queue.
	select {
	case q.notify <- struct{}{}:
	default:
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		q.Run(ctx)
	}()

	// Collect the processing order.
	var order []string
	for i := 0; i < 4; i++ {
		select {
		case id := <-orderCh:
			order = append(order, id)
		case <-time.After(5 * time.Second):
			t.Fatalf("task processing timed out after %d tasks", i)
		}
	}

	// Fair scheduling should interleave users: a1, b1, a2, a3
	require.Equal(t, "a1", order[0], "first task should be a1 (earliest)")
	require.Equal(t, "b1", order[1], "second task should be b1 (round-robin to different user)")
	require.Equal(t, "a2", order[2], "third task should be a2 (back to user A)")
	require.Equal(t, "a3", order[3], "fourth task should be a3 (only user A tasks remain)")

	cancel()
	wg.Wait()
}

func TestQueue_FailedTask(t *testing.T) {
	store := NewTaskStore()
	mockMgr := newMockDBManager()
	q := NewQueue(store, mockMgr, nil, 1, nil)

	failedCh := make(chan string, 1)

	q.RegisterHandler(TaskTypeEmbed, func(ctx context.Context, task *Task) (json.RawMessage, error) {
		failedCh <- task.ID
		return nil, context.Canceled
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		q.Run(ctx)
	}()

	task := &Task{
		ID:       "fail-task",
		UserID:   "user1",
		Type:     TaskTypeEmbed,
		Priority: PriorityBackground,
		Payload:  json.RawMessage(`{"note_id":"n1"}`),
	}
	err := q.Enqueue(ctx, task)
	require.NoError(t, err)

	select {
	case id := <-failedCh:
		require.Equal(t, "fail-task", id)
	case <-time.After(5 * time.Second):
		t.Fatal("task was not processed within timeout")
	}

	// Poll for the failure to be recorded.
	db, _ := mockMgr.Open(ctx, "user1")
	var got *Task
	for range 100 {
		got, err = store.Get(ctx, db, "fail-task")
		if err == nil && got.Status == TaskStatusFailed {
			break
		}
		time.Sleep(time.Millisecond)
	}
	require.NoError(t, err)
	require.Equal(t, TaskStatusFailed, got.Status)
	require.Contains(t, got.Error, "context canceled")

	cancel()
	wg.Wait()
}

func TestQueue_NoHandler(t *testing.T) {
	store := NewTaskStore()
	mockMgr := newMockDBManager()
	q := NewQueue(store, mockMgr, nil, 1, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		q.Run(ctx)
	}()

	task := &Task{
		ID:       "no-handler-task",
		UserID:   "user1",
		Type:     "unknown_type",
		Priority: PriorityBackground,
		Payload:  json.RawMessage(`{}`),
	}
	err := q.Enqueue(ctx, task)
	require.NoError(t, err)

	// Poll for the task to be marked as failed.
	db, _ := mockMgr.Open(ctx, "user1")
	var got *Task
	for range 200 {
		got, err = store.Get(ctx, db, "no-handler-task")
		if err == nil && got.Status == TaskStatusFailed {
			break
		}
		time.Sleep(time.Millisecond)
	}
	require.NoError(t, err)
	require.Equal(t, TaskStatusFailed, got.Status)
	require.Contains(t, got.Error, "no handler")

	cancel()
	wg.Wait()
}

func TestQueue_ContextCancellation(t *testing.T) {
	store := NewTaskStore()
	mockMgr := newMockDBManager()
	q := NewQueue(store, mockMgr, nil, 1, nil)

	q.RegisterHandler(TaskTypeEmbed, func(ctx context.Context, task *Task) (json.RawMessage, error) {
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())

	doneCh := make(chan struct{})
	go func() {
		q.Run(ctx)
		close(doneCh)
	}()

	// Cancel immediately and verify the queue shuts down.
	cancel()

	select {
	case <-doneCh:
		// ok, queue stopped
	case <-time.After(5 * time.Second):
		t.Fatal("queue did not stop after context cancellation")
	}
}

func TestQueue_LoadPending(t *testing.T) {
	store := NewTaskStore()
	mockMgr := newMockDBManager()

	ctx := context.Background()

	// Pre-seed some tasks in a user's DB.
	db, _ := mockMgr.Open(ctx, "user1")
	now := time.Now().UTC()
	store.Create(ctx, db, &Task{
		ID: "pending-1", Type: TaskTypeEmbed, Priority: PriorityBackground,
		Status: TaskStatusPending, Payload: json.RawMessage(`{}`), CreatedAt: now,
	})
	store.Create(ctx, db, &Task{
		ID: "running-1", Type: TaskTypeEmbed, Priority: PriorityBackground,
		Status: TaskStatusRunning, Payload: json.RawMessage(`{}`), CreatedAt: now,
	})
	store.Create(ctx, db, &Task{
		ID: "done-1", Type: TaskTypeEmbed, Priority: PriorityBackground,
		Status: TaskStatusDone, Payload: json.RawMessage(`{}`), CreatedAt: now,
	})

	q := NewQueue(store, mockMgr, nil, 1, nil)
	err := q.LoadPending(ctx)
	require.NoError(t, err)

	// Should have loaded 2 tasks (pending + running which was reset to pending).
	q.mu.Lock()
	require.Equal(t, 2, q.pq.Len())
	q.mu.Unlock()

	// Verify the running task was reset to pending.
	got, err := store.Get(ctx, db, "running-1")
	require.NoError(t, err)
	require.Equal(t, TaskStatusPending, got.Status)
}

func TestQueue_EnqueueAssignsDefaults(t *testing.T) {
	store := NewTaskStore()
	mockMgr := newMockDBManager()
	q := NewQueue(store, mockMgr, nil, 1, nil)

	ctx := context.Background()

	// Enqueue with no ID, no status, no CreatedAt -- should get defaults.
	task := &Task{
		UserID:   "user1",
		Type:     TaskTypeEmbed,
		Priority: PriorityBackground,
		Payload:  json.RawMessage(`{}`),
	}

	err := q.Enqueue(ctx, task)
	require.NoError(t, err)

	require.NotEmpty(t, task.ID, "ID should be auto-assigned")
	require.Equal(t, TaskStatusPending, task.Status, "status should default to pending")
	require.False(t, task.CreatedAt.IsZero(), "CreatedAt should be set")

	// Verify it was persisted.
	db, _ := mockMgr.Open(ctx, "user1")
	got, err := store.Get(ctx, db, task.ID)
	require.NoError(t, err)
	require.Equal(t, TaskStatusPending, got.Status)
}
