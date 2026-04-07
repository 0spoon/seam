// Package scheduler runs cron-based and one-shot jobs that fire AI
// briefings, automations, and other proactive actions.
package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// ErrNotFound is returned when a schedule is not found.
var ErrNotFound = errors.New("not found")

// ActionType is the kind of work a schedule fires when it runs.
type ActionType string

const (
	// ActionBriefing produces a daily briefing note.
	ActionBriefing ActionType = "briefing"
	// ActionAutomation runs an automation rule (placeholder for Phase 5).
	ActionAutomation ActionType = "automation"
	// ActionReminder sends a reminder (placeholder for Phase 4).
	ActionReminder ActionType = "reminder"
)

// ValidActionTypes lists all action types the scheduler currently supports.
var ValidActionTypes = map[ActionType]bool{
	ActionBriefing:   true,
	ActionAutomation: true,
	ActionReminder:   true,
}

// Schedule represents a row in the schedules table.
type Schedule struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	CronExpr     string     `json:"cron_expr,omitempty"`
	RunAt        *time.Time `json:"run_at,omitempty"`
	ActionType   ActionType `json:"action_type"`
	ActionConfig string     `json:"action_config"` // raw JSON document
	Enabled      bool       `json:"enabled"`
	LastRunAt    *time.Time `json:"last_run_at,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
	NextRunAt    *time.Time `json:"next_run_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// IsRecurring reports whether the schedule has a cron expression.
func (s *Schedule) IsRecurring() bool {
	return strings.TrimSpace(s.CronExpr) != ""
}

// IsOneShot reports whether the schedule is a single-run job.
func (s *Schedule) IsOneShot() bool {
	return s.RunAt != nil && !s.IsRecurring()
}

// DBTX is satisfied by *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// Store provides data access for schedules.
type Store struct{}

// NewStore creates a new scheduler Store.
func NewStore() *Store { return &Store{} }

// Create inserts a new schedule.
func (s *Store) Create(ctx context.Context, db DBTX, sch *Schedule) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO schedules (id, name, cron_expr, run_at, action_type, action_config,
			enabled, last_run_at, last_error, next_run_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sch.ID,
		sch.Name,
		sch.CronExpr,
		nullTimePtr(sch.RunAt),
		string(sch.ActionType),
		nonEmpty(sch.ActionConfig, "{}"),
		boolToInt(sch.Enabled),
		nullTimePtr(sch.LastRunAt),
		nullableStr(sch.LastError),
		nullTimePtr(sch.NextRunAt),
		sch.CreatedAt.UTC().Format(time.RFC3339Nano),
		sch.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("scheduler.Store.Create: %w", err)
	}
	return nil
}

// Get returns a schedule by ID.
func (s *Store) Get(ctx context.Context, db DBTX, id string) (*Schedule, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, name, cron_expr, run_at, action_type, action_config,
			enabled, last_run_at, last_error, next_run_at, created_at, updated_at
		 FROM schedules WHERE id = ?`, id)
	sch, err := scanScheduleRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("scheduler.Store.Get: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("scheduler.Store.Get: %w", err)
	}
	return sch, nil
}

// List returns all schedules ordered by next_run_at then created_at.
// When enabledOnly is true, disabled schedules are skipped.
func (s *Store) List(ctx context.Context, db DBTX, enabledOnly bool) ([]*Schedule, error) {
	q := `SELECT id, name, cron_expr, run_at, action_type, action_config,
			enabled, last_run_at, last_error, next_run_at, created_at, updated_at
		 FROM schedules`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY (next_run_at IS NULL), next_run_at ASC, created_at ASC`

	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("scheduler.Store.List: %w", err)
	}
	defer rows.Close()

	var out []*Schedule
	for rows.Next() {
		sch, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("scheduler.Store.List: scan: %w", err)
		}
		out = append(out, sch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scheduler.Store.List: rows: %w", err)
	}
	return out, nil
}

// ListDue returns enabled schedules whose next_run_at is at or before now.
// One-shot schedules that have already run (last_run_at is set) are excluded
// even if their next_run_at predates now.
func (s *Store) ListDue(ctx context.Context, db DBTX, now time.Time) ([]*Schedule, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, name, cron_expr, run_at, action_type, action_config,
			enabled, last_run_at, last_error, next_run_at, created_at, updated_at
		 FROM schedules
		 WHERE enabled = 1
		   AND next_run_at IS NOT NULL
		   AND next_run_at <= ?
		   AND NOT (cron_expr = '' AND last_run_at IS NOT NULL)
		 ORDER BY next_run_at ASC`,
		now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("scheduler.Store.ListDue: %w", err)
	}
	defer rows.Close()

	var out []*Schedule
	for rows.Next() {
		sch, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("scheduler.Store.ListDue: scan: %w", err)
		}
		out = append(out, sch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scheduler.Store.ListDue: rows: %w", err)
	}
	return out, nil
}

// Update writes back the mutable fields of a schedule.
func (s *Store) Update(ctx context.Context, db DBTX, sch *Schedule) error {
	res, err := db.ExecContext(ctx,
		`UPDATE schedules
		 SET name = ?, cron_expr = ?, run_at = ?, action_type = ?, action_config = ?,
		     enabled = ?, next_run_at = ?, updated_at = ?
		 WHERE id = ?`,
		sch.Name,
		sch.CronExpr,
		nullTimePtr(sch.RunAt),
		string(sch.ActionType),
		nonEmpty(sch.ActionConfig, "{}"),
		boolToInt(sch.Enabled),
		nullTimePtr(sch.NextRunAt),
		sch.UpdatedAt.UTC().Format(time.RFC3339Nano),
		sch.ID,
	)
	if err != nil {
		return fmt.Errorf("scheduler.Store.Update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("scheduler.Store.Update: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkRun records a successful run and the next firing time.
func (s *Store) MarkRun(ctx context.Context, db DBTX, id string, ranAt time.Time, nextRun *time.Time) error {
	res, err := db.ExecContext(ctx,
		`UPDATE schedules
		 SET last_run_at = ?, last_error = NULL, next_run_at = ?, updated_at = ?
		 WHERE id = ?`,
		ranAt.UTC().Format(time.RFC3339Nano),
		nullTimePtr(nextRun),
		time.Now().UTC().Format(time.RFC3339Nano),
		id,
	)
	if err != nil {
		return fmt.Errorf("scheduler.Store.MarkRun: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("scheduler.Store.MarkRun: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkError records a failed run while still advancing next_run_at so the
// scheduler does not get stuck retrying the same minute forever.
func (s *Store) MarkError(ctx context.Context, db DBTX, id string, ranAt time.Time, nextRun *time.Time, errMsg string) error {
	res, err := db.ExecContext(ctx,
		`UPDATE schedules
		 SET last_run_at = ?, last_error = ?, next_run_at = ?, updated_at = ?
		 WHERE id = ?`,
		ranAt.UTC().Format(time.RFC3339Nano),
		nullableStr(errMsg),
		nullTimePtr(nextRun),
		time.Now().UTC().Format(time.RFC3339Nano),
		id,
	)
	if err != nil {
		return fmt.Errorf("scheduler.Store.MarkError: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("scheduler.Store.MarkError: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a schedule.
func (s *Store) Delete(ctx context.Context, db DBTX, id string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("scheduler.Store.Delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("scheduler.Store.Delete: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// scanSchedule scans a schedule row from *sql.Rows.
func scanSchedule(rows *sql.Rows) (*Schedule, error) {
	sch := &Schedule{}
	var (
		runAt, lastRunAt, lastError, nextRunAt sql.NullString
		createdAt, updatedAt                   string
		enabled                                int
		actionType                             string
	)
	if err := rows.Scan(&sch.ID, &sch.Name, &sch.CronExpr, &runAt,
		&actionType, &sch.ActionConfig, &enabled,
		&lastRunAt, &lastError, &nextRunAt,
		&createdAt, &updatedAt); err != nil {
		return nil, err
	}
	hydrate(sch, runAt, lastRunAt, lastError, nextRunAt, createdAt, updatedAt, enabled, actionType)
	return sch, nil
}

// scanScheduleRow scans a schedule from *sql.Row.
func scanScheduleRow(row *sql.Row) (*Schedule, error) {
	sch := &Schedule{}
	var (
		runAt, lastRunAt, lastError, nextRunAt sql.NullString
		createdAt, updatedAt                   string
		enabled                                int
		actionType                             string
	)
	if err := row.Scan(&sch.ID, &sch.Name, &sch.CronExpr, &runAt,
		&actionType, &sch.ActionConfig, &enabled,
		&lastRunAt, &lastError, &nextRunAt,
		&createdAt, &updatedAt); err != nil {
		return nil, err
	}
	hydrate(sch, runAt, lastRunAt, lastError, nextRunAt, createdAt, updatedAt, enabled, actionType)
	return sch, nil
}

func hydrate(sch *Schedule,
	runAt, lastRunAt, lastError, nextRunAt sql.NullString,
	createdAt, updatedAt string,
	enabled int, actionType string,
) {
	sch.ActionType = ActionType(actionType)
	sch.Enabled = enabled != 0
	if sch.ActionConfig == "" {
		sch.ActionConfig = "{}"
	}
	if t, ok := parseTime(runAt); ok {
		sch.RunAt = &t
	}
	if t, ok := parseTime(lastRunAt); ok {
		sch.LastRunAt = &t
	}
	if lastError.Valid {
		sch.LastError = lastError.String
	}
	if t, ok := parseTime(nextRunAt); ok {
		sch.NextRunAt = &t
	}
	if t, err := time.Parse(time.RFC3339Nano, createdAt); err != nil {
		slog.Warn("scheduler.Store: parse created_at",
			"id", sch.ID, "value", createdAt, "error", err)
	} else {
		sch.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		slog.Warn("scheduler.Store: parse updated_at",
			"id", sch.ID, "value", updatedAt, "error", err)
	} else {
		sch.UpdatedAt = t
	}
}

func parseTime(s sql.NullString) (time.Time, bool) {
	if !s.Valid || s.String == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, s.String); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, s.String); err == nil {
		return t, true
	}
	slog.Warn("scheduler.Store: failed to parse time", "value", s.String)
	return time.Time{}, false
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullableStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullTimePtr(t *time.Time) interface{} {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func nonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
