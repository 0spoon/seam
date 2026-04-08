package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Task types.
const (
	TaskTypeEmbed               = "embed"
	TaskTypeDeleteEmbed         = "delete_embed"
	TaskTypeSynthesize          = "synthesize"
	TaskTypeAutolink            = "autolink"
	TaskTypeChat                = "chat"
	TaskTypeAssist              = "assist"
	TaskTypeSummarizeTranscript = "summarize_transcript"
)

// Task statuses.
const (
	TaskStatusPending = "pending"
	TaskStatusRunning = "running"
	TaskStatusDone    = "done"
	TaskStatusFailed  = "failed"
)

// Priority levels.
const (
	PriorityInteractive   = 0
	PriorityUserTriggered = 1
	PriorityBackground    = 2
)

// Task represents an AI task in the queue.
type Task struct {
	ID         string          `json:"id"`
	UserID     string          `json:"user_id"` // populated at runtime, not stored in per-user DB
	Type       string          `json:"type"`
	Priority   int             `json:"priority"`
	Status     string          `json:"status"`
	Payload    json.RawMessage `json:"payload"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	StartedAt  time.Time       `json:"started_at,omitempty"`
	FinishedAt time.Time       `json:"finished_at,omitempty"`

	// retries is a transient field (not persisted) that tracks how many
	// times this task has been re-enqueued due to transient errors.
	retries int `json:"-"`
}

// TaskEvent is pushed via WebSocket when a task changes status.
type TaskEvent struct {
	TaskID  string          `json:"task_id"`
	UserID  string          `json:"-"`
	Type    string          `json:"type"` // "progress", "complete", "failed"
	Payload json.RawMessage `json:"payload"`
}

// EmbedPayload is the JSON payload for embed tasks.
type EmbedPayload struct {
	NoteID string `json:"note_id"`
	Scope  string `json:"scope,omitempty"` // "agent" or "user"; empty defaults to "user"
}

// DeleteEmbedPayload is the JSON payload for delete_embed tasks.
type DeleteEmbedPayload struct {
	NoteID string `json:"note_id"`
}

// SynthesizePayload is the JSON payload for synthesis tasks.
type SynthesizePayload struct {
	Scope     string `json:"scope"` // "project" or "tag"
	ProjectID string `json:"project_id,omitempty"`
	Tag       string `json:"tag,omitempty"`
	Prompt    string `json:"prompt"`
}

// AutolinkPayload is the JSON payload for autolink tasks.
type AutolinkPayload struct {
	NoteID string `json:"note_id"`
}

// ChatPayload is the JSON payload for chat tasks.
type ChatPayload struct {
	Query   string        `json:"query"`
	History []ChatMessage `json:"history,omitempty"`
	// Summary, when non-empty, is a digest of older conversation
	// turns that have been folded out of the verbatim history window.
	// It is included in the system prompt for long conversations so
	// the model retains context beyond the recent-message cap.
	Summary string `json:"summary,omitempty"`
}

// SummarizeTranscriptPayload is the JSON payload for summarize_transcript tasks.
type SummarizeTranscriptPayload struct {
	NoteID string `json:"note_id"`
}

// Domain errors for tasks.
var ErrTaskNotFound = errors.New("task not found")

// TaskStore provides CRUD operations for AI tasks in a per-user SQLite DB.
type TaskStore struct{}

// NewTaskStore creates a new TaskStore.
func NewTaskStore() *TaskStore {
	return &TaskStore{}
}

// Create inserts a new task into the ai_tasks table.
func (s *TaskStore) Create(ctx context.Context, db *sql.DB, t *Task) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO ai_tasks (id, type, priority, status, payload, result, error, created_at, started_at, finished_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Type, t.Priority, t.Status,
		string(t.Payload), nullString(t.Result), nullStr(t.Error),
		t.CreatedAt.Format(time.RFC3339Nano),
		nullTime(t.StartedAt),
		nullTime(t.FinishedAt),
	)
	if err != nil {
		return fmt.Errorf("ai.TaskStore.Create: %w", err)
	}
	return nil
}

// Get retrieves a task by ID.
func (s *TaskStore) Get(ctx context.Context, db *sql.DB, id string) (*Task, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, type, priority, status, payload, result, error, created_at, started_at, finished_at
		 FROM ai_tasks WHERE id = ?`, id)

	t, err := scanTask(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTaskNotFound
		}
		return nil, fmt.Errorf("ai.TaskStore.Get: %w", err)
	}
	return t, nil
}

// UpdateStatus updates a task's status and related timestamps.
func (s *TaskStore) UpdateStatus(ctx context.Context, db *sql.DB, id, status string, result json.RawMessage, errMsg string) error {
	now := time.Now().UTC()

	var query string
	var args []interface{}

	switch status {
	case TaskStatusRunning:
		query = `UPDATE ai_tasks SET status = ?, started_at = ? WHERE id = ?`
		args = []interface{}{status, now.Format(time.RFC3339Nano), id}
	case TaskStatusDone:
		query = `UPDATE ai_tasks SET status = ?, result = ?, finished_at = ? WHERE id = ?`
		args = []interface{}{status, nullString(result), now.Format(time.RFC3339Nano), id}
	case TaskStatusFailed:
		query = `UPDATE ai_tasks SET status = ?, error = ?, finished_at = ? WHERE id = ?`
		args = []interface{}{status, errMsg, now.Format(time.RFC3339Nano), id}
	default:
		query = `UPDATE ai_tasks SET status = ? WHERE id = ?`
		args = []interface{}{status, id}
	}

	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("ai.TaskStore.UpdateStatus: %w", err)
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("ai.TaskStore.UpdateStatus: rows affected: %w", raErr)
	}
	if n == 0 {
		return ErrTaskNotFound
	}
	return nil
}

// ListPending returns all pending and running tasks ordered by priority
// and creation time.
func (s *TaskStore) ListPending(ctx context.Context, db *sql.DB) ([]*Task, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, type, priority, status, payload, result, error, created_at, started_at, finished_at
		 FROM ai_tasks
		 WHERE status IN (?, ?)
		 ORDER BY priority ASC, created_at ASC`,
		TaskStatusPending, TaskStatusRunning,
	)
	if err != nil {
		return nil, fmt.Errorf("ai.TaskStore.ListPending: %w", err)
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		t, err := scanTaskRows(rows)
		if err != nil {
			return nil, fmt.Errorf("ai.TaskStore.ListPending: scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// scanTask scans a single task from a sql.Row.
func scanTask(row *sql.Row) (*Task, error) {
	var t Task
	var payload, result, errMsg sql.NullString
	var createdAt, startedAt, finishedAt sql.NullString

	err := row.Scan(&t.ID, &t.Type, &t.Priority, &t.Status,
		&payload, &result, &errMsg,
		&createdAt, &startedAt, &finishedAt)
	if err != nil {
		return nil, err
	}

	if payload.Valid {
		t.Payload = json.RawMessage(payload.String)
	}
	if result.Valid {
		t.Result = json.RawMessage(result.String)
	}
	if errMsg.Valid {
		t.Error = errMsg.String
	}
	if createdAt.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, createdAt.String)
		if parseErr != nil {
			slog.Warn("ai.scanTask: failed to parse created_at",
				"task_id", t.ID, "value", createdAt.String, "error", parseErr)
		} else {
			t.CreatedAt = parsed
		}
	}
	if startedAt.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, startedAt.String)
		if parseErr != nil {
			slog.Warn("ai.scanTask: failed to parse started_at",
				"task_id", t.ID, "value", startedAt.String, "error", parseErr)
		} else {
			t.StartedAt = parsed
		}
	}
	if finishedAt.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, finishedAt.String)
		if parseErr != nil {
			slog.Warn("ai.scanTask: failed to parse finished_at",
				"task_id", t.ID, "value", finishedAt.String, "error", parseErr)
		} else {
			t.FinishedAt = parsed
		}
	}

	return &t, nil
}

// scanTaskRows scans a single task from sql.Rows.
func scanTaskRows(rows *sql.Rows) (*Task, error) {
	var t Task
	var payload, result, errMsg sql.NullString
	var createdAt, startedAt, finishedAt sql.NullString

	err := rows.Scan(&t.ID, &t.Type, &t.Priority, &t.Status,
		&payload, &result, &errMsg,
		&createdAt, &startedAt, &finishedAt)
	if err != nil {
		return nil, err
	}

	if payload.Valid {
		t.Payload = json.RawMessage(payload.String)
	}
	if result.Valid {
		t.Result = json.RawMessage(result.String)
	}
	if errMsg.Valid {
		t.Error = errMsg.String
	}
	if createdAt.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, createdAt.String)
		if parseErr != nil {
			slog.Warn("ai.scanTaskRows: failed to parse created_at",
				"task_id", t.ID, "value", createdAt.String, "error", parseErr)
		} else {
			t.CreatedAt = parsed
		}
	}
	if startedAt.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, startedAt.String)
		if parseErr != nil {
			slog.Warn("ai.scanTaskRows: failed to parse started_at",
				"task_id", t.ID, "value", startedAt.String, "error", parseErr)
		} else {
			t.StartedAt = parsed
		}
	}
	if finishedAt.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, finishedAt.String)
		if parseErr != nil {
			slog.Warn("ai.scanTaskRows: failed to parse finished_at",
				"task_id", t.ID, "value", finishedAt.String, "error", parseErr)
		} else {
			t.FinishedAt = parsed
		}
	}

	return &t, nil
}

// Helper functions for nullable fields.
func nullString(b json.RawMessage) interface{} {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t.Format(time.RFC3339Nano)
}
