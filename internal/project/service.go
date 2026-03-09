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

// Service handles project business logic including filesystem operations.
type Service struct {
	store         *Store
	userDBManager userdb.Manager
	logger        *slog.Logger
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
	}

	if err := s.store.Update(ctx, db, existing); err != nil {
		return nil, fmt.Errorf("project.Service.Update: %w", err)
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

	switch cascade {
	case "inbox":
		// Move notes to inbox: move files to root notes dir, update DB paths.
		for _, n := range notes {
			oldAbs := filepath.Join(notesDir, n.filePath)
			filename := filepath.Base(n.filePath)
			newRelPath := filename
			newAbs := filepath.Join(notesDir, newRelPath)

			// Move file on disk.
			if mvErr := os.Rename(oldAbs, newAbs); mvErr != nil {
				if !os.IsNotExist(mvErr) {
					s.logger.Warn("project.Service.Delete: failed to move note file",
						"note_id", n.id, "from", oldAbs, "to", newAbs, "error", mvErr)
				}
			}

			// Update DB: clear project_id and set new file_path.
			_, updateErr := db.ExecContext(ctx,
				`UPDATE notes SET project_id = NULL, file_path = ? WHERE id = ?`,
				newRelPath, n.id)
			if updateErr != nil {
				return fmt.Errorf("project.Service.Delete: move note %s to inbox: %w", n.id, updateErr)
			}
		}

	case "delete":
		// Delete notes: remove files and DB rows.
		for _, n := range notes {
			absPath := filepath.Join(notesDir, n.filePath)
			if rmErr := os.Remove(absPath); rmErr != nil && !os.IsNotExist(rmErr) {
				s.logger.Warn("project.Service.Delete: failed to remove note file",
					"note_id", n.id, "path", absPath, "error", rmErr)
			}
			// Cascading delete on the project row handles note rows via
			// ON DELETE SET NULL, but we need to explicitly delete the note
			// rows since the FK action only NULLs project_id.
			if _, delErr := db.ExecContext(ctx,
				`DELETE FROM notes WHERE id = ?`, n.id); delErr != nil {
				return fmt.Errorf("project.Service.Delete: delete note %s: %w", n.id, delErr)
			}
		}
	}

	if err := s.store.Delete(ctx, db, projectID); err != nil {
		return fmt.Errorf("project.Service.Delete: %w", err)
	}

	// Remove project directory (should now be empty).
	if err := os.Remove(projectDir); err != nil && !os.IsNotExist(err) {
		// Directory not empty or other error -- log but do not fail.
		s.logger.Warn("could not remove project directory",
			"user_id", userID, "project_id", projectID, "path", projectDir, "error", err)
	}

	s.logger.Info("project deleted", "user_id", userID, "project_id", projectID, "cascade", cascade)
	return nil
}

// Slugify converts a name into a URL-safe slug.
// Rules: lowercase, spaces/underscores become hyphens, strip non-alphanumeric
// characters (except hyphens), collapse multiple hyphens, trim leading/trailing hyphens.
func Slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")

	// Remove anything that is not alphanumeric or hyphen.
	re := regexp.MustCompile(`[^a-z0-9-]`)
	s = re.ReplaceAllString(s, "")

	// Collapse multiple hyphens.
	multi := regexp.MustCompile(`-{2,}`)
	s = multi.ReplaceAllString(s, "-")

	s = strings.Trim(s, "-")
	return s
}
