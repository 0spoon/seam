package assistant

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// Action represents a logged assistant action for audit.
type Action struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	ToolName       string    `json:"tool_name"`
	Arguments      string    `json:"arguments"` // JSON
	Result         string    `json:"result"`    // JSON
	Status         string    `json:"status"`    // pending, approved, executed, rejected, failed
	CreatedAt      time.Time `json:"created_at"`
	ExecutedAt     time.Time `json:"executed_at,omitempty"`
}

// Action statuses.
const (
	ActionStatusPending  = "pending"
	ActionStatusApproved = "approved"
	ActionStatusExecuted = "executed"
	ActionStatusRejected = "rejected"
	ActionStatusFailed   = "failed"
)

// Store provides CRUD operations for assistant actions.
type Store struct{}

// NewStore creates a new assistant Store.
func NewStore() *Store {
	return &Store{}
}

// RecordAction inserts an action into the assistant_actions table.
func (s *Store) RecordAction(ctx context.Context, db *sql.DB, a *Action) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO assistant_actions (id, conversation_id, tool_name, arguments, result, status, created_at, executed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.ConversationID, a.ToolName, a.Arguments,
		nullableStr(a.Result), a.Status,
		a.CreatedAt.Format(time.RFC3339Nano),
		nullableTime(a.ExecutedAt),
	)
	if err != nil {
		return fmt.Errorf("assistant.Store.RecordAction: %w", err)
	}
	return nil
}

// UpdateActionStatus updates an action's status and optionally its result.
func (s *Store) UpdateActionStatus(ctx context.Context, db *sql.DB, id, status, result string) error {
	now := time.Now().UTC()
	var q string
	var args []interface{}

	switch status {
	case ActionStatusExecuted, ActionStatusFailed:
		q = `UPDATE assistant_actions SET status = ?, result = ?, executed_at = ? WHERE id = ?`
		args = []interface{}{status, nullableStr(result), now.Format(time.RFC3339Nano), id}
	default:
		q = `UPDATE assistant_actions SET status = ? WHERE id = ?`
		args = []interface{}{status, id}
	}

	res, err := db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("assistant.Store.UpdateActionStatus: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("assistant.Store.UpdateActionStatus: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListActions returns actions for a conversation.
func (s *Store) ListActions(ctx context.Context, db *sql.DB, conversationID string, limit int) ([]*Action, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, conversation_id, tool_name, arguments, result, status, created_at, executed_at
		 FROM assistant_actions
		 WHERE conversation_id = ?
		 ORDER BY created_at ASC
		 LIMIT ?`, conversationID, limit)
	if err != nil {
		return nil, fmt.Errorf("assistant.Store.ListActions: %w", err)
	}
	defer rows.Close()

	var actions []*Action
	for rows.Next() {
		a, scanErr := scanAction(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("assistant.Store.ListActions: scan: %w", scanErr)
		}
		actions = append(actions, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("assistant.Store.ListActions: rows: %w", err)
	}
	return actions, nil
}

// ListActionsByID returns a single action by its ID.
func (s *Store) ListActionsByID(ctx context.Context, db *sql.DB, actionID string) (*Action, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, conversation_id, tool_name, arguments, result, status, created_at, executed_at
		 FROM assistant_actions
		 WHERE id = ?`, actionID)
	if err != nil {
		return nil, fmt.Errorf("assistant.Store.ListActionsByID: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("assistant.Store.ListActionsByID: rows: %w", err)
		}
		return nil, nil
	}
	a, scanErr := scanAction(rows)
	if scanErr != nil {
		return nil, fmt.Errorf("assistant.Store.ListActionsByID: scan: %w", scanErr)
	}
	return a, nil
}

func scanAction(rows *sql.Rows) (*Action, error) {
	var a Action
	var arguments, result, status sql.NullString
	var createdAt, executedAt sql.NullString

	err := rows.Scan(&a.ID, &a.ConversationID, &a.ToolName,
		&arguments, &result, &status, &createdAt, &executedAt)
	if err != nil {
		return nil, err
	}

	if arguments.Valid {
		a.Arguments = arguments.String
	}
	if result.Valid {
		a.Result = result.String
	}
	if status.Valid {
		a.Status = status.String
	}
	if createdAt.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, createdAt.String)
		if parseErr != nil {
			slog.Warn("assistant.scanAction: failed to parse created_at",
				"action_id", a.ID, "value", createdAt.String, "error", parseErr)
		} else {
			a.CreatedAt = parsed
		}
	}
	if executedAt.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, executedAt.String)
		if parseErr != nil {
			slog.Warn("assistant.scanAction: failed to parse executed_at",
				"action_id", a.ID, "value", executedAt.String, "error", parseErr)
		} else {
			a.ExecutedAt = parsed
		}
	}

	return &a, nil
}

func nullableStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullableTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t.Format(time.RFC3339)
}
