// Package task extracts and tracks checkbox items from note bodies
// into a queryable task index.
package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Domain errors.
var ErrNotFound = errors.New("not found")

// Task represents a checkbox item extracted from a note.
type Task struct {
	ID         string    `json:"id"`
	NoteID     string    `json:"note_id"`
	LineNumber int       `json:"line_number"`
	Content    string    `json:"content"`
	Done       bool      `json:"done"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// TaskFilter controls listing.
type TaskFilter struct {
	NoteID      string
	ProjectID   string
	ProjectSlug string // filter by project slug (joins projects table)
	Tag         string
	Done        *bool // nil = all, true = done only, false = open only
	Limit       int
	Offset      int
}

// TaskSummary provides aggregate counts.
type TaskSummary struct {
	Total int `json:"total"`
	Done  int `json:"done"`
	Open  int `json:"open"`
}

// DBTX is satisfied by both *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// Store provides data access methods for tasks.
type Store struct{}

// NewStore creates a new Store.
func NewStore() *Store { return &Store{} }

// Upsert inserts or replaces a task.
func (s *Store) Upsert(ctx context.Context, db DBTX, t *Task) error {
	done := 0
	if t.Done {
		done = 1
	}
	_, err := db.ExecContext(ctx,
		`INSERT OR REPLACE INTO tasks (id, note_id, line_number, content, done, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.NoteID, t.LineNumber, t.Content, done,
		t.CreatedAt.Format(time.RFC3339), t.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("task.Store.Upsert: %w", err)
	}
	return nil
}

// DeleteByNote removes all tasks for a given note.
func (s *Store) DeleteByNote(ctx context.Context, db DBTX, noteID string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM tasks WHERE note_id = ?`, noteID)
	if err != nil {
		return fmt.Errorf("task.Store.DeleteByNote: %w", err)
	}
	return nil
}

// Delete removes a single task by ID.
func (s *Store) Delete(ctx context.Context, db DBTX, id string) error {
	result, err := db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("task.Store.Delete: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("task.Store.Delete: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Get returns a single task by ID.
func (s *Store) Get(ctx context.Context, db DBTX, id string) (*Task, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, note_id, line_number, content, done, created_at, updated_at
		 FROM tasks WHERE id = ?`, id)

	t, err := scanTask(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("task.Store.Get: %w", err)
	}
	return t, nil
}

// List returns tasks matching the filter and the total count.
func (s *Store) List(ctx context.Context, db DBTX, filter TaskFilter) ([]*Task, int, error) {
	baseFrom, where, args := buildFilterClauses(filter)

	// Count total.
	countQuery := "SELECT COUNT(*) FROM " + baseFrom
	if len(where) > 0 {
		countQuery += " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("task.Store.List: count: %w", err)
	}

	// Select tasks.
	selectQuery := "SELECT t.id, t.note_id, t.line_number, t.content, t.done, t.created_at, t.updated_at FROM " + baseFrom
	if len(where) > 0 {
		selectQuery += " WHERE " + strings.Join(where, " AND ")
	}
	selectQuery += " ORDER BY t.done ASC, t.updated_at DESC"

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	selectQuery += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, filter.Offset)

	rows, err := db.QueryContext(ctx, selectQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("task.Store.List: %w", err)
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		var t Task
		var done int
		var createdAt, updatedAt string
		if err := rows.Scan(&t.ID, &t.NoteID, &t.LineNumber, &t.Content, &done, &createdAt, &updatedAt); err != nil {
			return nil, 0, fmt.Errorf("task.Store.List: scan: %w", err)
		}
		t.Done = done != 0
		if parsed, err := time.Parse(time.RFC3339, createdAt); err != nil {
			slog.Warn("task.Store: failed to parse created_at", "value", createdAt, "error", err)
		} else {
			t.CreatedAt = parsed
		}
		if parsed, err := time.Parse(time.RFC3339, updatedAt); err != nil {
			slog.Warn("task.Store: failed to parse updated_at", "value", updatedAt, "error", err)
		} else {
			t.UpdatedAt = parsed
		}
		tasks = append(tasks, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("task.Store.List: rows: %w", err)
	}
	return tasks, total, nil
}

// Summary returns aggregate task counts matching the filter.
func (s *Store) Summary(ctx context.Context, db DBTX, filter TaskFilter) (*TaskSummary, error) {
	baseFrom, where, args := buildFilterClauses(filter)

	query := fmt.Sprintf(
		`SELECT COUNT(*) AS total,
		        SUM(CASE WHEN t.done = 1 THEN 1 ELSE 0 END) AS done,
		        SUM(CASE WHEN t.done = 0 THEN 1 ELSE 0 END) AS open
		 FROM %s`, baseFrom)
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	var summary TaskSummary
	var doneNull, openNull sql.NullInt64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&summary.Total, &doneNull, &openNull); err != nil {
		return nil, fmt.Errorf("task.Store.Summary: %w", err)
	}
	summary.Done = int(doneNull.Int64)
	summary.Open = int(openNull.Int64)
	return &summary, nil
}

// UpdateDone toggles the done status for a task.
func (s *Store) UpdateDone(ctx context.Context, db DBTX, id string, done bool) error {
	d := 0
	if done {
		d = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.ExecContext(ctx,
		`UPDATE tasks SET done = ?, updated_at = ? WHERE id = ?`, d, now, id)
	if err != nil {
		return fmt.Errorf("task.Store.UpdateDone: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("task.Store.UpdateDone: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// buildFilterClauses constructs the FROM clause, WHERE conditions, and args
// for task queries based on the filter.
func buildFilterClauses(filter TaskFilter) (string, []string, []interface{}) {
	baseFrom := "tasks t"
	var where []string
	var args []interface{}

	if filter.NoteID != "" {
		where = append(where, "t.note_id = ?")
		args = append(args, filter.NoteID)
	}
	if filter.ProjectID != "" {
		baseFrom += " JOIN notes n ON n.id = t.note_id"
		where = append(where, "n.project_id = ?")
		args = append(args, filter.ProjectID)
	}
	if filter.ProjectSlug != "" {
		if !strings.Contains(baseFrom, "JOIN notes") {
			baseFrom += " JOIN notes n ON n.id = t.note_id"
		}
		baseFrom += " JOIN projects p ON p.id = n.project_id"
		where = append(where, "p.slug = ?")
		args = append(args, filter.ProjectSlug)
	}
	if filter.Tag != "" {
		if !strings.Contains(baseFrom, "JOIN notes") {
			baseFrom += " JOIN notes n ON n.id = t.note_id"
		}
		baseFrom += " JOIN note_tags nt ON nt.note_id = n.id JOIN tags tg ON tg.id = nt.tag_id"
		where = append(where, "tg.name = ?")
		args = append(args, filter.Tag)
	}
	if filter.Done != nil {
		if *filter.Done {
			where = append(where, "t.done = 1")
		} else {
			where = append(where, "t.done = 0")
		}
	}
	return baseFrom, where, args
}

// scanTask scans a single task row.
func scanTask(row *sql.Row) (*Task, error) {
	var t Task
	var done int
	var createdAt, updatedAt string
	if err := row.Scan(&t.ID, &t.NoteID, &t.LineNumber, &t.Content, &done, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	t.Done = done != 0
	if parsed, err := time.Parse(time.RFC3339, createdAt); err != nil {
		slog.Warn("task.Store: failed to parse created_at", "value", createdAt, "error", err)
	} else {
		t.CreatedAt = parsed
	}
	if parsed, err := time.Parse(time.RFC3339, updatedAt); err != nil {
		slog.Warn("task.Store: failed to parse updated_at", "value", updatedAt, "error", err)
	} else {
		t.UpdatedAt = parsed
	}
	return &t, nil
}
