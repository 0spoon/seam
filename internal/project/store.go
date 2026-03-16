// Package project manages projects in per-user SQLite databases.
// Projects group notes into directories on the filesystem.
package project

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
var (
	ErrNotFound   = errors.New("not found")
	ErrSlugExists = errors.New("slug already exists")
)

// Project represents a note-grouping project.
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// DBTX is an interface satisfied by both *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// Store defines data access methods for projects against a per-user SQLite DB.
type Store struct{}

// NewStore creates a new project Store.
func NewStore() *Store {
	return &Store{}
}

// Create inserts a new project. Returns ErrSlugExists if the slug is taken.
func (s *Store) Create(ctx context.Context, db DBTX, p *Project) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO projects (id, name, slug, description, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Slug, p.Description,
		p.CreatedAt.Format(time.RFC3339), p.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("project.Store.Create: %w", ErrSlugExists)
		}
		return fmt.Errorf("project.Store.Create: %w", err)
	}
	return nil
}

// Get retrieves a project by ID. Returns ErrNotFound if no project matches.
func (s *Store) Get(ctx context.Context, db DBTX, id string) (*Project, error) {
	p := &Project{}
	var createdAt, updatedAt string
	err := db.QueryRowContext(ctx,
		`SELECT id, name, slug, description, created_at, updated_at
		 FROM projects WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("project.Store.Get: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("project.Store.Get: %w", err)
	}
	if parsed, err := time.Parse(time.RFC3339, createdAt); err != nil {
		slog.Warn("project.Store.Get: failed to parse created_at", "value", createdAt, "error", err)
	} else {
		p.CreatedAt = parsed
	}
	if parsed, err := time.Parse(time.RFC3339, updatedAt); err != nil {
		slog.Warn("project.Store.Get: failed to parse updated_at", "value", updatedAt, "error", err)
	} else {
		p.UpdatedAt = parsed
	}
	return p, nil
}

// GetBySlug retrieves a project by slug. Returns ErrNotFound if no project matches.
func (s *Store) GetBySlug(ctx context.Context, db DBTX, slug string) (*Project, error) {
	p := &Project{}
	var createdAt, updatedAt string
	err := db.QueryRowContext(ctx,
		`SELECT id, name, slug, description, created_at, updated_at
		 FROM projects WHERE slug = ?`, slug,
	).Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("project.Store.GetBySlug: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("project.Store.GetBySlug: %w", err)
	}
	if parsed, err := time.Parse(time.RFC3339, createdAt); err != nil {
		slog.Warn("project.Store.GetBySlug: failed to parse created_at", "value", createdAt, "error", err)
	} else {
		p.CreatedAt = parsed
	}
	if parsed, err := time.Parse(time.RFC3339, updatedAt); err != nil {
		slog.Warn("project.Store.GetBySlug: failed to parse updated_at", "value", updatedAt, "error", err)
	} else {
		p.UpdatedAt = parsed
	}
	return p, nil
}

// List returns all projects ordered by creation time descending.
func (s *Store) List(ctx context.Context, db DBTX) ([]*Project, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, name, slug, description, created_at, updated_at
		 FROM projects ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("project.Store.List: %w", err)
	}
	defer rows.Close()

	var projects []*Project
	for rows.Next() {
		p := &Project{}
		var createdAt, updatedAt string
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("project.Store.List: scan: %w", err)
		}
		if parsed, err := time.Parse(time.RFC3339, createdAt); err != nil {
			slog.Warn("project.Store.List: failed to parse created_at", "value", createdAt, "error", err)
		} else {
			p.CreatedAt = parsed
		}
		if parsed, err := time.Parse(time.RFC3339, updatedAt); err != nil {
			slog.Warn("project.Store.List: failed to parse updated_at", "value", updatedAt, "error", err)
		} else {
			p.UpdatedAt = parsed
		}
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("project.Store.List: rows: %w", err)
	}
	return projects, nil
}

// Update modifies an existing project. Returns ErrNotFound if the project
// does not exist, or ErrSlugExists if the updated slug conflicts.
func (s *Store) Update(ctx context.Context, db DBTX, p *Project) error {
	result, err := db.ExecContext(ctx,
		`UPDATE projects SET name = ?, slug = ?, description = ?, updated_at = ?
		 WHERE id = ?`,
		p.Name, p.Slug, p.Description,
		p.UpdatedAt.Format(time.RFC3339), p.ID,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("project.Store.Update: %w", ErrSlugExists)
		}
		return fmt.Errorf("project.Store.Update: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("project.Store.Update: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("project.Store.Update: %w", ErrNotFound)
	}
	return nil
}

// Delete removes a project by ID. Returns ErrNotFound if the project does not exist.
func (s *Store) Delete(ctx context.Context, db DBTX, id string) error {
	result, err := db.ExecContext(ctx,
		`DELETE FROM projects WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("project.Store.Delete: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("project.Store.Delete: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("project.Store.Delete: %w", ErrNotFound)
	}
	return nil
}

// isUniqueConstraintError checks if a SQLite error is a UNIQUE constraint violation.
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
