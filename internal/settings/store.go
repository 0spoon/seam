// Package settings manages per-user key-value settings in SQLite.
package settings

import (
	"context"
	"database/sql"
	"fmt"
)

// DBTX is an interface satisfied by both *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// Store defines data access methods for settings against a per-user SQLite DB.
type Store struct{}

// NewStore creates a new settings Store.
func NewStore() *Store {
	return &Store{}
}

// GetAll retrieves all settings as a key-value map.
func (s *Store) GetAll(ctx context.Context, db DBTX) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("settings.Store.GetAll: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("settings.Store.GetAll: scan: %w", err)
		}
		result[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings.Store.GetAll: rows: %w", err)
	}
	return result, nil
}

// Set upserts a single setting.
func (s *Store) Set(ctx context.Context, db DBTX, key, value string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("settings.Store.Set: %w", err)
	}
	return nil
}

// Delete removes a single setting by key.
func (s *Store) Delete(ctx context.Context, db DBTX, key string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("settings.Store.Delete: %w", err)
	}
	return nil
}
