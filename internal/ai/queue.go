package ai

import (
	"container/heap"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/katata/seam/internal/userdb"
	"github.com/katata/seam/internal/ws"
)

// TaskHandler processes a single AI task.
type TaskHandler func(ctx context.Context, task *Task) (json.RawMessage, error)

// DefaultMaxQueueSize is the default maximum number of tasks the in-memory queue will hold.
const DefaultMaxQueueSize = 10000

// DefaultTaskTimeout is the default per-task execution timeout.
const DefaultTaskTimeout = 5 * time.Minute

// ErrQueueFull is returned when the queue has reached its maximum capacity.
var ErrQueueFull = fmt.Errorf("ai task queue is full")

// Queue manages AI task execution with priority ordering and fair scheduling.
// Fair scheduling ensures round-robin across users within each priority level,
// so one user with many tasks cannot starve other users.
type Queue struct {
	mu           sync.Mutex
	cond         *sync.Cond
	pq           priorityQueue
	notify       chan struct{} // kept for backward compatibility with tests that send on it
	handlers     map[string]TaskHandler
	store        *TaskStore
	dbManager    userdb.Manager
	hub          *ws.Hub
	workers      int
	logger       *slog.Logger
	lastUserByPr map[int]string // tracks last-served user per priority level for round-robin
	maxQueueSize int            // maximum number of tasks in the in-memory queue
	taskTimeout  time.Duration  // per-task execution timeout
}

// NewQueue creates a new AI task queue. Optional maxQueueSize and taskTimeout
// can be passed; zero values use defaults (10000 and 5 minutes respectively).
func NewQueue(store *TaskStore, dbManager userdb.Manager, hub *ws.Hub, workers int, logger *slog.Logger, opts ...func(*Queue)) *Queue {
	if workers <= 0 {
		workers = 1
	}
	if logger == nil {
		logger = slog.Default()
	}
	q := &Queue{
		pq:           make(priorityQueue, 0),
		notify:       make(chan struct{}, 1),
		handlers:     make(map[string]TaskHandler),
		store:        store,
		dbManager:    dbManager,
		hub:          hub,
		workers:      workers,
		logger:       logger,
		lastUserByPr: make(map[int]string),
		maxQueueSize: DefaultMaxQueueSize,
		taskTimeout:  DefaultTaskTimeout,
	}
	for _, opt := range opts {
		opt(q)
	}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// WithMaxQueueSize returns an option that sets the maximum queue size.
func WithMaxQueueSize(size int) func(*Queue) {
	return func(q *Queue) {
		if size > 0 {
			q.maxQueueSize = size
		}
	}
}

// WithTaskTimeout returns an option that sets the per-task execution timeout.
func WithTaskTimeout(d time.Duration) func(*Queue) {
	return func(q *Queue) {
		if d > 0 {
			q.taskTimeout = d
		}
	}
}

// RegisterHandler registers a handler for a task type. Must be called before
// Run() starts processing tasks (i.e., during initialization only).
func (q *Queue) RegisterHandler(taskType string, handler TaskHandler) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[taskType] = handler
}

// Enqueue adds a task to the queue. It persists the task in the user's DB
// and adds it to the in-memory priority queue.
func (q *Queue) Enqueue(ctx context.Context, task *Task) error {
	if task.ID == "" {
		idVal, idErr := ulid.New(ulid.Now(), rand.Reader)
		if idErr != nil {
			return fmt.Errorf("ai.Queue.Enqueue: generate id: %w", idErr)
		}
		task.ID = idVal.String()
	}
	if task.Status == "" {
		task.Status = TaskStatusPending
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}

	// Persist to user's DB.
	db, err := q.dbManager.Open(ctx, task.UserID)
	if err != nil {
		return fmt.Errorf("ai.Queue.Enqueue: open db: %w", err)
	}
	if err := q.store.Create(ctx, db, task); err != nil {
		return fmt.Errorf("ai.Queue.Enqueue: create task: %w", err)
	}

	// Add to in-memory queue with backpressure check.
	q.mu.Lock()
	if q.pq.Len() >= q.maxQueueSize {
		q.mu.Unlock()
		return fmt.Errorf("ai.Queue.Enqueue: %w (max %d)", ErrQueueFull, q.maxQueueSize)
	}
	heap.Push(&q.pq, &pqItem{
		task:     task,
		priority: task.Priority,
	})
	q.cond.Broadcast()
	q.mu.Unlock()

	// Also signal via channel for backward compatibility with tests.
	select {
	case q.notify <- struct{}{}:
	default:
	}

	return nil
}

// LoadPending reloads pending/running tasks from all user databases
// into the in-memory queue. Called on startup.
func (q *Queue) LoadPending(ctx context.Context) error {
	users, err := q.dbManager.ListUsers(ctx)
	if err != nil {
		return fmt.Errorf("ai.Queue.LoadPending: list users: %w", err)
	}

	// Collect tasks and status resets outside the lock to avoid holding
	// the queue mutex during DB operations (SCAN4-M9).
	type statusReset struct {
		db     *sql.DB
		taskID string
	}
	var items []*pqItem
	var resets []statusReset

	for _, userID := range users {
		db, err := q.dbManager.Open(ctx, userID)
		if err != nil {
			q.logger.Warn("ai.Queue.LoadPending: failed to open user db",
				"user_id", userID, "error", err)
			continue
		}
		tasks, err := q.store.ListPending(ctx, db)
		if err != nil {
			q.logger.Warn("ai.Queue.LoadPending: failed to list pending tasks",
				"user_id", userID, "error", err)
			continue
		}
		for _, t := range tasks {
			t.UserID = userID
			if t.Status == TaskStatusRunning {
				t.Status = TaskStatusPending
				resets = append(resets, statusReset{db: db, taskID: t.ID})
			}
			items = append(items, &pqItem{
				task:     t,
				priority: t.Priority,
			})
		}
		if len(tasks) > 0 {
			q.logger.Info("ai.Queue.LoadPending: loaded tasks",
				"user_id", userID, "count", len(tasks))
		}
	}

	// Perform status resets outside the lock.
	for _, r := range resets {
		q.store.UpdateStatus(ctx, r.db, r.taskID, TaskStatusPending, nil, "")
	}

	// Now acquire the lock and push items, respecting maxQueueSize (SCAN4-M8).
	q.mu.Lock()
	defer q.mu.Unlock()

	loaded := 0
	for _, item := range items {
		if q.pq.Len() >= q.maxQueueSize {
			q.logger.Warn("ai.Queue.LoadPending: queue full, skipping remaining tasks",
				"max_size", q.maxQueueSize, "skipped", len(items)-loaded)
			break
		}
		heap.Push(&q.pq, item)
		loaded++
	}

	return nil
}

// Run starts the queue workers. This blocks until the context is cancelled.
func (q *Queue) Run(ctx context.Context) error {
	// Bridge: convert legacy notify channel signals into cond broadcasts
	// so tests that send on q.notify still wake workers.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-q.notify:
				q.cond.Broadcast()
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < q.workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			q.worker(ctx, workerID)
		}(i)
	}
	wg.Wait()
	return nil
}

// worker processes tasks from the queue, one at a time. Each worker blocks
// on the condition variable until a task is available, then dequeues and
// processes exactly one task before looping. This ensures multiple workers
// provide real parallelism.
func (q *Queue) worker(ctx context.Context, workerID int) {
	for {
		task := q.waitForTask(ctx)
		if task == nil {
			return // context cancelled
		}
		q.processTask(ctx, task)
	}
}

// waitForTask blocks until a task is available or the context is cancelled.
// Returns nil when the context is done.
//
// NOTE: This spawns a goroutine per wait to bridge context cancellation with
// sync.Cond. This is acceptable because the number of concurrent waiters
// equals the number of workers (typically 1-4), and the goroutine exits
// promptly when the wait completes or context is cancelled.
func (q *Queue) waitForTask(ctx context.Context) *Task {
	// Spawn a goroutine that broadcasts on the cond when the context is
	// cancelled, so the cond.Wait unblocks.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			q.cond.Broadcast()
		case <-done:
		}
	}()

	q.mu.Lock()
	defer q.mu.Unlock()

	for q.pq.Len() == 0 {
		if ctx.Err() != nil {
			return nil
		}
		q.cond.Wait()
	}

	if ctx.Err() != nil {
		return nil
	}

	return q.dequeueLocked()
}

// dequeueLocked removes and returns the highest-priority task, applying fair
// round-robin scheduling across users within the same priority level.
// The caller must hold q.mu.
func (q *Queue) dequeueLocked() *Task {
	if q.pq.Len() == 0 {
		return nil
	}

	// Peek at the top priority level.
	topPriority := q.pq[0].priority
	lastUser := q.lastUserByPr[topPriority]

	// If there is a last-served user for this priority level, scan ALL
	// elements at the top priority (not just up to the first non-matching
	// one) because a heap does not sort siblings. We look for a task from
	// a different user to enforce round-robin fairness.
	//
	// NOTE: This does an O(n) scan of the entire heap. This is acceptable
	// for expected queue sizes (typically hundreds to low thousands of tasks).
	// A more efficient approach would require a secondary index by user,
	// but the added complexity is not warranted at current scale.
	if lastUser != "" {
		bestIdx := -1
		for i, item := range q.pq {
			if item.priority != topPriority {
				continue // skip non-top-priority items; heap order is not sorted
			}
			if item.task.UserID != lastUser {
				bestIdx = i
				break
			}
			if bestIdx == -1 {
				bestIdx = i // fallback: all tasks at this priority are from lastUser
			}
		}
		if bestIdx > 0 {
			// Remove the selected item from the heap.
			item := q.pq[bestIdx]
			heap.Remove(&q.pq, bestIdx)
			q.lastUserByPr[topPriority] = item.task.UserID
			return item.task
		}
	}

	// Default: pop the top item (first task at this priority, FIFO).
	item := heap.Pop(&q.pq).(*pqItem)
	q.lastUserByPr[topPriority] = item.task.UserID
	return item.task
}

// processTask executes a single task.
func (q *Queue) processTask(ctx context.Context, task *Task) {
	q.mu.Lock()
	handler, ok := q.handlers[task.Type]
	q.mu.Unlock()
	if !ok {
		q.logger.Error("ai.Queue: no handler for task type", "type", task.Type, "task_id", task.ID)
		q.failTask(ctx, task, "no handler registered for task type: "+task.Type)
		return
	}

	q.logger.Info("ai.Queue: processing task",
		"task_id", task.ID, "type", task.Type, "user_id", task.UserID, "priority", task.Priority)

	// Mark as running.
	db, err := q.dbManager.Open(ctx, task.UserID)
	if err != nil {
		q.logger.Error("ai.Queue: failed to open db for task",
			"task_id", task.ID, "error", err, "retries", task.retries)
		if task.retries >= maxRetries {
			q.logger.Error("ai.Queue: task exceeded max retries, dropping",
				"task_id", task.ID, "type", task.Type, "retries", task.retries)
			return
		}
		task.retries++
		q.mu.Lock()
		heap.Push(&q.pq, &pqItem{
			task:     task,
			priority: task.Priority,
		})
		q.cond.Broadcast()
		q.mu.Unlock()
		return
	}
	if err := q.store.UpdateStatus(ctx, db, task.ID, TaskStatusRunning, nil, ""); err != nil {
		q.logger.Error("ai.Queue.processTask: failed to update status to running",
			"task_id", task.ID, "error", err)
	}

	// Send progress event.
	q.sendEvent(task.UserID, TaskEvent{
		TaskID: task.ID,
		UserID: task.UserID,
		Type:   "progress",
	})

	// Execute handler with a per-task timeout.
	taskCtx, taskCancel := context.WithTimeout(ctx, q.taskTimeout)
	defer taskCancel()

	result, err := handler(taskCtx, task)
	if err != nil {
		q.failTask(ctx, task, err.Error())
		return
	}

	// Mark as done.
	if err := q.store.UpdateStatus(ctx, db, task.ID, TaskStatusDone, result, ""); err != nil {
		q.logger.Error("ai.Queue.processTask: failed to update status to done",
			"task_id", task.ID, "error", err)
	}
	q.logger.Info("ai.Queue: task completed", "task_id", task.ID, "type", task.Type)

	// Send complete event.
	q.sendEvent(task.UserID, TaskEvent{
		TaskID:  task.ID,
		UserID:  task.UserID,
		Type:    "complete",
		Payload: result,
	})
}

// failTask marks a task as failed.
func (q *Queue) failTask(ctx context.Context, task *Task, errMsg string) {
	q.logger.Error("ai.Queue: task failed",
		"task_id", task.ID, "type", task.Type, "error", errMsg)

	db, err := q.dbManager.Open(ctx, task.UserID)
	if err != nil {
		q.logger.Error("ai.Queue.failTask: failed to open db",
			"task_id", task.ID, "error", err)
		return
	}
	if err := q.store.UpdateStatus(ctx, db, task.ID, TaskStatusFailed, nil, errMsg); err != nil {
		q.logger.Error("ai.Queue.failTask: failed to update status to failed",
			"task_id", task.ID, "error", err)
	}

	q.sendEvent(task.UserID, TaskEvent{
		TaskID:  task.ID,
		UserID:  task.UserID,
		Type:    "failed",
		Payload: mustJSON(map[string]string{"error": errMsg}),
	})
}

// sendEvent pushes a task event via WebSocket.
func (q *Queue) sendEvent(userID string, event TaskEvent) {
	if q.hub == nil {
		return
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}

	msgType := ws.MsgTypeTaskProgress
	switch event.Type {
	case "complete":
		msgType = ws.MsgTypeTaskComplete
	case "failed":
		msgType = ws.MsgTypeTaskFailed
	}

	q.hub.Send(userID, ws.Message{
		Type:    msgType,
		Payload: payload,
	})
}

func mustJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// -- Priority queue implementation using container/heap --

type pqItem struct {
	task     *Task
	priority int
	index    int
}

// maxRetries is the maximum number of times a task can be re-enqueued
// due to transient failures (e.g. DB unavailable) before being dropped.
const maxRetries = 3

type priorityQueue []*pqItem

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	// Lower priority number = higher priority.
	if pq[i].priority != pq[j].priority {
		return pq[i].priority < pq[j].priority
	}
	// Same priority: FIFO by creation time.
	return pq[i].task.CreatedAt.Before(pq[j].task.CreatedAt)
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *priorityQueue) Push(x interface{}) {
	item := x.(*pqItem)
	item.index = len(*pq)
	*pq = append(*pq, item)
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[:n-1]
	return item
}
