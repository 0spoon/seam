package project

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/katata/seam/internal/userdb"
)

// FrontmatterUpdater clears the project field in a note's YAML frontmatter
// on disk. Implemented by note.Service via a closure to avoid circular imports.
type FrontmatterUpdater func(notesDir, filePath string) error

// Service handles project business logic including filesystem operations.
type Service struct {
	store              *Store
	userDBManager      userdb.Manager
	logger             *slog.Logger
	frontmatterUpdater FrontmatterUpdater
}

// NewService creates a new project Service.
func NewService(store *Store, userDBManager userdb.Manager, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:         store,
		userDBManager: userDBManager,
		logger:        logger,
	}
}

// SetFrontmatterUpdater sets the callback used to clear the project field
// from YAML frontmatter when cascading notes to inbox. Called after note
// service is created to break the circular dependency.
func (s *Service) SetFrontmatterUpdater(fn FrontmatterUpdater) {
	s.frontmatterUpdater = fn
}

// Create creates a new project with the given name and description.
// It generates a slug from the name, creates the project directory on disk,
// and inserts the project into the user's database.
func (s *Service) Create(ctx context.Context, userID, name, description string) (*Project, error) {
	if name == "" {
		return nil, fmt.Errorf("project.Service.Create: name is required")
	}

	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("project.Service.Create: open db: %w", err)
	}

	slug := Slugify(name)
	if slug == "" {
		return nil, fmt.Errorf("project.Service.Create: name produces empty slug")
	}

	now := time.Now().UTC()
	p := &Project{
		ID:          ulid.MustNew(ulid.Now(), rand.Reader).String(),
		Name:        name,
		Slug:        slug,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Create project directory on disk.
	projectDir := filepath.Join(s.userDBManager.UserNotesDir(userID), slug)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return nil, fmt.Errorf("project.Service.Create: mkdir: %w", err)
	}

	if err := s.store.Create(ctx, db, p); err != nil {
		// Clean up directory on DB insert failure.
		os.Remove(projectDir)
		return nil, fmt.Errorf("project.Service.Create: %w", err)
	}

	s.logger.Info("project created", "user_id", userID, "project_id", p.ID, "slug", slug)
	return p, nil
}

// Get retrieves a project by ID for the given user.
func (s *Service) Get(ctx context.Context, userID, projectID string) (*Project, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("project.Service.Get: open db: %w", err)
	}

	p, err := s.store.Get(ctx, db, projectID)
	if err != nil {
		return nil, fmt.Errorf("project.Service.Get: %w", err)
	}
	return p, nil
}

// List returns all projects for the given user.
func (s *Service) List(ctx context.Context, userID string) ([]*Project, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("project.Service.List: open db: %w", err)
	}

	projects, err := s.store.List(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("project.Service.List: %w", err)
	}
	return projects, nil
}

// Update modifies an existing project. Only non-nil fields are updated.
// If the name changes, the slug changes and the project directory is renamed.
func (s *Service) Update(ctx context.Context, userID, projectID string, name, description *string) (*Project, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("project.Service.Update: open db: %w", err)
	}

	existing, err := s.store.Get(ctx, db, projectID)
	if err != nil {
		return nil, fmt.Errorf("project.Service.Update: %w", err)
	}

	oldSlug := existing.Slug

	if name != nil {
		existing.Name = *name
		newSlug := Slugify(*name)
		if newSlug == "" {
			return nil, fmt.Errorf("project.Service.Update: name produces empty slug")
		}
		existing.Slug = newSlug
	}
	if description != nil {
		existing.Description = *description
	}

	existing.UpdatedAt = time.Now().UTC()

	// Wrap all DB writes in a transaction for atomicity.
	tx, txErr := db.BeginTx(ctx, nil)
	if txErr != nil {
		return nil, fmt.Errorf("project.Service.Update: begin tx: %w", txErr)
	}
	defer tx.Rollback() //nolint:errcheck // rollback is a no-op after commit

	// Rename directory if slug changed.
	if existing.Slug != oldSlug {
		notesDir := s.userDBManager.UserNotesDir(userID)
		oldDir := filepath.Join(notesDir, oldSlug)
		newDir := filepath.Join(notesDir, existing.Slug)
		if err := os.Rename(oldDir, newDir); err != nil {
			// If old dir does not exist, create the new one instead.
			if os.IsNotExist(err) {
				if mkErr := os.MkdirAll(newDir, 0o755); mkErr != nil {
					return nil, fmt.Errorf("project.Service.Update: mkdir: %w", mkErr)
				}
			} else {
				return nil, fmt.Errorf("project.Service.Update: rename dir: %w", err)
			}
		}

		// A-9: Update file_path for all notes in this project to reflect
		// the new slug. Without this, note.Get would fail with file-not-found
		// because the DB still has the old slug prefix.
		oldPrefix := oldSlug + "/"
		newPrefix := existing.Slug + "/"
		_, updateErr := tx.ExecContext(ctx,
			`UPDATE notes SET file_path = ? || SUBSTR(file_path, ?)
			 WHERE project_id = ? AND file_path LIKE ?`,
			newPrefix, len(oldPrefix)+1, projectID, oldPrefix+"%",
		)
		if updateErr != nil {
			return nil, fmt.Errorf("project.Service.Update: update note paths: %w", updateErr)
		}
	}

	if err := s.store.Update(ctx, tx, existing); err != nil {
		return nil, fmt.Errorf("project.Service.Update: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("project.Service.Update: commit: %w", err)
	}

	s.logger.Info("project updated", "user_id", userID, "project_id", projectID)
	return existing, nil
}

// Delete removes a project. The cascade parameter controls what happens
// to notes in the deleted project:
//   - "inbox":  move notes to inbox (update file_path, set project_id to NULL)
//   - "delete": delete all notes in the project (files + DB rows)
func (s *Service) Delete(ctx context.Context, userID, projectID string, cascade string) error {
	if cascade != "inbox" && cascade != "delete" {
		return fmt.Errorf("project.Service.Delete: cascade must be \"inbox\" or \"delete\"")
	}

	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("project.Service.Delete: open db: %w", err)
	}

	// Fetch project to get slug for directory cleanup.
	existing, err := s.store.Get(ctx, db, projectID)
	if err != nil {
		return fmt.Errorf("project.Service.Delete: %w", err)
	}

	notesDir := s.userDBManager.UserNotesDir(userID)
	projectDir := filepath.Join(notesDir, existing.Slug)

	// Handle notes in this project before deleting the project row.
	// Query notes belonging to this project.
	rows, qErr := db.QueryContext(ctx,
		`SELECT id, file_path FROM notes WHERE project_id = ?`, projectID)
	if qErr != nil {
		return fmt.Errorf("project.Service.Delete: query notes: %w", qErr)
	}

	type noteInfo struct {
		id       string
		filePath string
	}
	var notes []noteInfo
	for rows.Next() {
		var n noteInfo
		if scanErr := rows.Scan(&n.id, &n.filePath); scanErr != nil {
			rows.Close()
			return fmt.Errorf("project.Service.Delete: scan note: %w", scanErr)
		}
		notes = append(notes, n)
	}
	rows.Close()
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("project.Service.Delete: rows error: %w", rowsErr)
	}

	// I-H12: Perform all DB operations first inside the transaction, then
	// do file operations after commit. This prevents a transaction rollback
	// from leaving files in the wrong directories. The watcher reconciliation
	// system will fix any file/DB drift if file operations partially fail.

	// A-7: Wrap all DB writes in a transaction for atomicity.
	tx, txErr := db.BeginTx(ctx, nil)
	if txErr != nil {
		return fmt.Errorf("project.Service.Delete: begin tx: %w", txErr)
	}
	defer tx.Rollback() //nolint:errcheck // rollback is a no-op after commit

	// Collect pending file operations to execute after commit.
	type fileMove struct {
		noteID     string
		oldAbs     string
		newRelPath string
		newAbs     string
	}
	var pendingMoves []fileMove
	var pendingDeletes []string

	switch cascade {
	case "inbox":
		// Update DB first: clear project_id and set new file_path.
		for _, n := range notes {
			filename := filepath.Base(n.filePath)
			newRelPath := filename

			_, updateErr := tx.ExecContext(ctx,
				`UPDATE notes SET project_id = NULL, file_path = ? WHERE id = ?`,
				newRelPath, n.id)
			if updateErr != nil {
				return fmt.Errorf("project.Service.Delete: move note %s to inbox: %w", n.id, updateErr)
			}

			pendingMoves = append(pendingMoves, fileMove{
				noteID:     n.id,
				oldAbs:     filepath.Join(notesDir, n.filePath),
				newRelPath: newRelPath,
				newAbs:     filepath.Join(notesDir, newRelPath),
			})
		}

	case "delete":
		// Delete DB rows first, collect file paths for later removal.
		for _, n := range notes {
			if _, delErr := tx.ExecContext(ctx,
				`DELETE FROM notes WHERE id = ?`, n.id); delErr != nil {
				return fmt.Errorf("project.Service.Delete: delete note %s: %w", n.id, delErr)
			}
			pendingDeletes = append(pendingDeletes, filepath.Join(notesDir, n.filePath))
		}
	}

	if err := s.store.Delete(ctx, tx, projectID); err != nil {
		return fmt.Errorf("project.Service.Delete: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("project.Service.Delete: commit: %w", err)
	}

	// --- Transaction committed; now perform file operations ---
	// Failures here are logged but do not roll back the DB. The watcher
	// reconciliation system will eventually correct any file/DB drift.

	switch cascade {
	case "inbox":
		for _, m := range pendingMoves {
			if mvErr := os.Rename(m.oldAbs, m.newAbs); mvErr != nil {
				if !os.IsNotExist(mvErr) {
					s.logger.Warn("project.Service.Delete: failed to move note file",
						"note_id", m.noteID, "from", m.oldAbs, "to", m.newAbs, "error", mvErr)
				}
			}

			// Clear the project field from YAML frontmatter on disk.
			if s.frontmatterUpdater != nil {
				if fmErr := s.frontmatterUpdater(notesDir, m.newRelPath); fmErr != nil {
					s.logger.Warn("project.Service.Delete: failed to clear project in frontmatter",
						"note_id", m.noteID, "file", m.newRelPath, "error", fmErr)
				}
			}
		}

	case "delete":
		for _, absPath := range pendingDeletes {
			if rmErr := os.Remove(absPath); rmErr != nil && !os.IsNotExist(rmErr) {
				s.logger.Warn("project.Service.Delete: failed to remove note file",
					"path", absPath, "error", rmErr)
			}
		}
	}

	// Remove project directory and any remaining files (e.g., .DS_Store).
	if err := os.RemoveAll(projectDir); err != nil {
		s.logger.Warn("could not remove project directory",
			"user_id", userID, "project_id", projectID, "path", projectDir, "error", err)
	}

	s.logger.Info("project deleted", "user_id", userID, "project_id", projectID, "cascade", cascade)
	return nil
}

// Pre-compiled regexps for slug generation. Avoids recompiling on every
// Slugify call (C-17).
var (
	slugNonAlphanumRe = regexp.MustCompile(`[^a-z0-9-]`)
	slugMultiHyphenRe = regexp.MustCompile(`-{2,}`)
)

// Slugify converts a name into a URL-safe slug.
// Rules: lowercase, spaces/underscores become hyphens, strip non-alphanumeric
// characters (except hyphens), collapse multiple hyphens, trim leading/trailing hyphens.
func Slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")

	// Remove anything that is not alphanumeric or hyphen.
	s = slugNonAlphanumRe.ReplaceAllString(s, "")

	// Collapse multiple hyphens.
	s = slugMultiHyphenRe.ReplaceAllString(s, "-")

	s = strings.Trim(s, "-")
	return s
}
