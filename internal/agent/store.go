package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SQLStore implements the Store interface using SQLite.
type SQLStore struct{}

// NewSQLStore creates a new SQLStore.
func NewSQLStore() *SQLStore {
	return &SQLStore{}
}

// CreateSession inserts a new agent session.
func (s *SQLStore) CreateSession(ctx context.Context, db DBTX, sess *Session) error {
	metaJSON, err := json.Marshal(sess.Metadata)
	if err != nil {
		return fmt.Errorf("agent.SQLStore.CreateSession: marshal metadata: %w", err)
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO agent_sessions (id, name, parent_session_id, status, findings, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Name, nullString(sess.ParentSessionID),
		sess.Status, nullString(sess.Findings),
		string(metaJSON),
		sess.CreatedAt.Format(time.RFC3339), sess.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("agent.SQLStore.CreateSession: %w", err)
	}
	return nil
}

// GetSession retrieves a session by ID.
func (s *SQLStore) GetSession(ctx context.Context, db DBTX, id string) (*Session, error) {
	return s.scanSession(db.QueryRowContext(ctx,
		`SELECT id, name, parent_session_id, status, findings, metadata, created_at, updated_at
		 FROM agent_sessions WHERE id = ?`, id,
	))
}

// GetSessionByName retrieves a session by its unique name.
func (s *SQLStore) GetSessionByName(ctx context.Context, db DBTX, name string) (*Session, error) {
	return s.scanSession(db.QueryRowContext(ctx,
		`SELECT id, name, parent_session_id, status, findings, metadata, created_at, updated_at
		 FROM agent_sessions WHERE name = ?`, name,
	))
}

// UpdateSession modifies an existing session.
func (s *SQLStore) UpdateSession(ctx context.Context, db DBTX, sess *Session) error {
	metaJSON, err := json.Marshal(sess.Metadata)
	if err != nil {
		return fmt.Errorf("agent.SQLStore.UpdateSession: marshal metadata: %w", err)
	}

	result, err := db.ExecContext(ctx,
		`UPDATE agent_sessions SET status = ?, findings = ?, metadata = ?, updated_at = ?
		 WHERE id = ?`,
		sess.Status, nullString(sess.Findings),
		string(metaJSON),
		sess.UpdatedAt.Format(time.RFC3339), sess.ID,
	)
	if err != nil {
		return fmt.Errorf("agent.SQLStore.UpdateSession: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("agent.SQLStore.UpdateSession: rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("agent.SQLStore.UpdateSession: %w", ErrNotFound)
	}
	return nil
}

// ListSessions returns sessions filtered by status, ordered by updated_at DESC.
// If status is empty, all sessions are returned.
func (s *SQLStore) ListSessions(ctx context.Context, db DBTX, status string, limit, offset int) ([]*Session, error) {
	query := `SELECT id, name, parent_session_id, status, findings, metadata, created_at, updated_at
		 FROM agent_sessions`
	var args []interface{}

	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY updated_at DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	if offset > 0 {
		query += " OFFSET ?"
		args = append(args, offset)
	}

	return s.querySessionRows(ctx, db, query, args...)
}

// ListChildSessions returns all sessions whose parent_session_id matches parentID.
func (s *SQLStore) ListChildSessions(ctx context.Context, db DBTX, parentID string) ([]*Session, error) {
	return s.querySessionRows(ctx, db,
		`SELECT id, name, parent_session_id, status, findings, metadata, created_at, updated_at
		 FROM agent_sessions WHERE parent_session_id = ? ORDER BY created_at ASC`,
		parentID,
	)
}

// ReconcileChildren links orphan child sessions to a newly created parent.
// Orphans are sessions that are direct children (name starts with parentName + "/"
// and contain no further "/" after that prefix) and have NULL parent_session_id.
// Returns the number of reconciled rows.
//
// Uses LIKE with an additional NOT LIKE to exclude grandchildren:
// "parent/%" matches all descendants, but NOT "parent/%/%" excludes
// sessions with further nesting (grandchildren and deeper).
func (s *SQLStore) ReconcileChildren(ctx context.Context, db DBTX, parentID, parentName string) (int64, error) {
	result, err := db.ExecContext(ctx,
		`UPDATE agent_sessions SET parent_session_id = ?
		 WHERE name LIKE ? AND name NOT LIKE ?
		 AND parent_session_id IS NULL AND id != ?`,
		parentID, parentName+"/%", parentName+"/%/%", parentID,
	)
	if err != nil {
		return 0, fmt.Errorf("agent.SQLStore.ReconcileChildren: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("agent.SQLStore.ReconcileChildren: rows affected: %w", err)
	}
	return n, nil
}

// LogToolCall records a tool call in the audit log.
func (s *SQLStore) LogToolCall(ctx context.Context, db DBTX, tc *ToolCallRecord) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO agent_tool_calls (id, session_id, tool_name, arguments, result, error, duration_ms, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		tc.ID, nullString(tc.SessionID), tc.ToolName, tc.Arguments,
		nullString(tc.Result), nullString(tc.Error),
		tc.DurationMs,
		tc.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("agent.SQLStore.LogToolCall: %w", err)
	}
	return nil
}

// ListToolCalls returns tool calls for a session, ordered by created_at DESC.
func (s *SQLStore) ListToolCalls(ctx context.Context, db DBTX, sessionID string, limit int) ([]*ToolCallRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, session_id, tool_name, arguments, result, error, duration_ms, created_at
		 FROM agent_tool_calls WHERE session_id = ? ORDER BY created_at DESC LIMIT ?`,
		sessionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("agent.SQLStore.ListToolCalls: %w", err)
	}
	defer rows.Close()

	var calls []*ToolCallRecord
	for rows.Next() {
		tc := &ToolCallRecord{}
		var sessionIDVal, resultVal, errorVal sql.NullString
		var durationMs sql.NullInt64
		var createdAt string

		if err := rows.Scan(&tc.ID, &sessionIDVal, &tc.ToolName, &tc.Arguments,
			&resultVal, &errorVal, &durationMs, &createdAt); err != nil {
			return nil, fmt.Errorf("agent.SQLStore.ListToolCalls: scan: %w", err)
		}
		tc.SessionID = sessionIDVal.String
		tc.Result = resultVal.String
		tc.Error = errorVal.String
		tc.DurationMs = durationMs.Int64
		tc.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		calls = append(calls, tc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agent.SQLStore.ListToolCalls: rows: %w", err)
	}
	return calls, nil
}

// scanSession scans a single session row from a *sql.Row.
func (s *SQLStore) scanSession(row *sql.Row) (*Session, error) {
	sess := &Session{}
	var parentID, findings sql.NullString
	var metaJSON string
	var createdAt, updatedAt string

	err := row.Scan(&sess.ID, &sess.Name, &parentID, &sess.Status,
		&findings, &metaJSON, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("agent.SQLStore: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("agent.SQLStore: %w", err)
	}

	sess.ParentSessionID = parentID.String
	sess.Findings = findings.String

	if metaJSON != "" {
		if jsonErr := json.Unmarshal([]byte(metaJSON), &sess.Metadata); jsonErr != nil {
			// Non-fatal: use default metadata.
			sess.Metadata = Metadata{}
		}
	}

	sess.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	sess.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return sess, nil
}

// querySessionRows queries multiple session rows.
func (s *SQLStore) querySessionRows(ctx context.Context, db DBTX, query string, args ...interface{}) ([]*Session, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("agent.SQLStore: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		sess := &Session{}
		var parentID, findings sql.NullString
		var metaJSON string
		var createdAt, updatedAt string

		if err := rows.Scan(&sess.ID, &sess.Name, &parentID, &sess.Status,
			&findings, &metaJSON, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("agent.SQLStore: scan: %w", err)
		}

		sess.ParentSessionID = parentID.String
		sess.Findings = findings.String

		if metaJSON != "" {
			_ = json.Unmarshal([]byte(metaJSON), &sess.Metadata)
		}

		sess.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		sess.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		sessions = append(sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agent.SQLStore: rows: %w", err)
	}
	return sessions, nil
}

// nullString converts a Go string to sql.NullString (empty -> NULL).
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
