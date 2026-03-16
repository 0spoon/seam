package note

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// Domain error for version not found.
var ErrVersionNotFound = errors.New("version not found")

// NoteVersion represents a snapshot of a note at a point in time.
type NoteVersion struct {
	ID          string    `json:"id"`
	NoteID      string    `json:"note_id"`
	Version     int       `json:"version"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	ContentHash string    `json:"content_hash"`
	CreatedAt   time.Time `json:"created_at"`
}

// VersionStore provides data access methods for note versions.
type VersionStore struct{}

// NewVersionStore creates a new VersionStore.
func NewVersionStore() *VersionStore {
	return &VersionStore{}
}

// Create inserts a new note version into the database.
func (s *VersionStore) Create(ctx context.Context, db DBTX, v *NoteVersion) error {
	if v.ID == "" {
		id, idErr := ulid.New(ulid.Now(), rand.Reader)
		if idErr != nil {
			return fmt.Errorf("note.VersionStore.Create: generate id: %w", idErr)
		}
		v.ID = id.String()
	}
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now().UTC()
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO note_versions (id, note_id, version, title, body, content_hash, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.NoteID, v.Version, v.Title, v.Body, v.ContentHash,
		v.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("note.VersionStore.Create: %w", err)
	}
	return nil
}

// List returns versions for a note, newest first, along with total count.
func (s *VersionStore) List(ctx context.Context, db DBTX, noteID string, limit, offset int) ([]*NoteVersion, int, error) {
	// Count total versions.
	var total int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM note_versions WHERE note_id = ?`, noteID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("note.VersionStore.List: count: %w", err)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id, note_id, version, title, body, content_hash, created_at
		 FROM note_versions
		 WHERE note_id = ?
		 ORDER BY version DESC
		 LIMIT ? OFFSET ?`,
		noteID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("note.VersionStore.List: %w", err)
	}
	defer rows.Close()

	var versions []*NoteVersion
	for rows.Next() {
		v, scanErr := scanVersionRow(rows)
		if scanErr != nil {
			return nil, 0, fmt.Errorf("note.VersionStore.List: scan: %w", scanErr)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("note.VersionStore.List: rows: %w", err)
	}

	return versions, total, nil
}

// Get retrieves a specific version of a note.
func (s *VersionStore) Get(ctx context.Context, db DBTX, noteID string, version int) (*NoteVersion, error) {
	var v NoteVersion
	var createdAt string
	err := db.QueryRowContext(ctx,
		`SELECT id, note_id, version, title, body, content_hash, created_at
		 FROM note_versions
		 WHERE note_id = ? AND version = ?`,
		noteID, version,
	).Scan(&v.ID, &v.NoteID, &v.Version, &v.Title, &v.Body, &v.ContentHash, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("note.VersionStore.Get: %w", ErrVersionNotFound)
		}
		return nil, fmt.Errorf("note.VersionStore.Get: %w", err)
	}

	var parseErr error
	v.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
	if parseErr != nil {
		// Use zero time rather than failing.
		v.CreatedAt = time.Time{}
	}

	return &v, nil
}

// Cleanup deletes the oldest versions when the count exceeds maxVersions.
func (s *VersionStore) Cleanup(ctx context.Context, db DBTX, noteID string, maxVersions int) error {
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM note_versions WHERE note_id = ?`, noteID,
	).Scan(&count); err != nil {
		return fmt.Errorf("note.VersionStore.Cleanup: count: %w", err)
	}

	if count <= maxVersions {
		return nil
	}

	// Delete oldest versions beyond the limit. Keep the newest maxVersions.
	_, err := db.ExecContext(ctx,
		`DELETE FROM note_versions
		 WHERE note_id = ? AND version NOT IN (
			 SELECT version FROM note_versions
			 WHERE note_id = ?
			 ORDER BY version DESC
			 LIMIT ?
		 )`,
		noteID, noteID, maxVersions,
	)
	if err != nil {
		return fmt.Errorf("note.VersionStore.Cleanup: %w", err)
	}
	return nil
}

// NextVersion returns the next version number for a note (max + 1, or 1 if none).
func (s *VersionStore) NextVersion(ctx context.Context, db DBTX, noteID string) (int, error) {
	var maxVersion sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT MAX(version) FROM note_versions WHERE note_id = ?`, noteID,
	).Scan(&maxVersion)
	if err != nil {
		return 0, fmt.Errorf("note.VersionStore.NextVersion: %w", err)
	}

	if !maxVersion.Valid {
		return 1, nil
	}
	return int(maxVersion.Int64) + 1, nil
}

// scanVersionRow scans a NoteVersion from *sql.Rows.
func scanVersionRow(rows *sql.Rows) (*NoteVersion, error) {
	var v NoteVersion
	var createdAt string
	err := rows.Scan(&v.ID, &v.NoteID, &v.Version, &v.Title, &v.Body, &v.ContentHash, &createdAt)
	if err != nil {
		return nil, err
	}

	var parseErr error
	v.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
	if parseErr != nil {
		v.CreatedAt = time.Time{}
	}

	return &v, nil
}
