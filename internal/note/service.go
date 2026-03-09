package note

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/userdb"
	"github.com/katata/seam/internal/validate"
)

// WriteSuppressor is used to tell the file watcher to ignore writes we
// perform ourselves, avoiding a re-index loop.
type WriteSuppressor interface {
	IgnoreNext(filePath string)
}

// CreateNoteReq is the request payload for creating a note.
type CreateNoteReq struct {
	Title            string   `json:"title"`
	Body             string   `json:"body"`
	ProjectID        string   `json:"project_id,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	SourceURL        string   `json:"source_url,omitempty"`
	Template         string   `json:"template,omitempty"`          // template name (Phase 3)
	TranscriptSource bool     `json:"transcript_source,omitempty"` // true for voice-captured notes
}

// UpdateNoteReq is the request payload for updating a note.
type UpdateNoteReq struct {
	Title     *string   `json:"title,omitempty"`
	Body      *string   `json:"body,omitempty"`
	ProjectID *string   `json:"project_id,omitempty"`
	Tags      *[]string `json:"tags,omitempty"`
}

// Service implements note business logic including filesystem operations.
type Service struct {
	store         *SQLStore
	projectStore  *project.Store
	userDBManager userdb.Manager
	suppressorMu  sync.RWMutex
	suppressor    WriteSuppressor
	logger        *slog.Logger
}

// NewService creates a new note Service.
func NewService(
	store *SQLStore,
	projectStore *project.Store,
	userDBManager userdb.Manager,
	suppressor WriteSuppressor,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:         store,
		projectStore:  projectStore,
		userDBManager: userDBManager,
		suppressor:    suppressor,
		logger:        logger,
	}
}

// SetSuppressor sets the write suppressor (file watcher) after construction.
// This is used when the watcher is created after the note service to break
// the circular dependency during server startup.
func (s *Service) SetSuppressor(suppressor WriteSuppressor) {
	s.suppressorMu.Lock()
	defer s.suppressorMu.Unlock()
	s.suppressor = suppressor
}

// getSuppressor returns the current write suppressor under a read lock.
func (s *Service) getSuppressor() WriteSuppressor {
	s.suppressorMu.RLock()
	defer s.suppressorMu.RUnlock()
	return s.suppressor
}

// Create creates a new note, writes the .md file to disk, and inserts it
// into the user's database.
func (s *Service) Create(ctx context.Context, userID string, req CreateNoteReq) (*Note, error) {
	if req.Title == "" {
		return nil, fmt.Errorf("note.Service.Create: title is required")
	}

	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("note.Service.Create: open db: %w", err)
	}

	notesDir := s.userDBManager.UserNotesDir(userID)
	now := time.Now().UTC()
	id := ulid.MustNew(ulid.Now(), rand.Reader).String()

	// Determine the directory: project slug or root (inbox).
	subDir := ""
	projectSlug := ""
	if req.ProjectID != "" {
		p, pErr := s.projectStore.Get(ctx, db, req.ProjectID)
		if pErr != nil {
			return nil, fmt.Errorf("note.Service.Create: resolve project: %w", pErr)
		}
		subDir = p.Slug
		projectSlug = p.Slug
	}

	// Generate unique filename from title.
	slug := project.Slugify(req.Title)
	if slug == "" {
		slug = id
	}
	filename := s.uniqueFilename(notesDir, subDir, slug)

	var relPath string
	if subDir != "" {
		relPath = subDir + "/" + filename
	} else {
		relPath = filename
	}

	// Build frontmatter.
	fm := &Frontmatter{
		ID:               id,
		Title:            req.Title,
		Project:          projectSlug,
		Tags:             req.Tags,
		Created:          now,
		Modified:         now,
		SourceURL:        req.SourceURL,
		TranscriptSource: req.TranscriptSource,
	}

	content, err := SerializeFrontmatter(fm, req.Body)
	if err != nil {
		return nil, fmt.Errorf("note.Service.Create: serialize: %w", err)
	}

	// Ensure parent directory exists.
	absPath := filepath.Join(notesDir, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, fmt.Errorf("note.Service.Create: mkdir: %w", err)
	}

	// Suppress watcher event for our own write.
	if sup := s.getSuppressor(); sup != nil {
		sup.IgnoreNext(absPath)
	}

	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("note.Service.Create: write file: %w", err)
	}

	hash := computeHash(content)

	n := &Note{
		ID:               id,
		Title:            req.Title,
		ProjectID:        req.ProjectID,
		FilePath:         relPath,
		Body:             req.Body,
		ContentHash:      hash,
		SourceURL:        req.SourceURL,
		TranscriptSource: req.TranscriptSource,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	// A-7: Wrap all DB writes in a transaction for atomicity.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		os.Remove(absPath)
		return nil, fmt.Errorf("note.Service.Create: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback is a no-op after commit

	if err := s.store.Create(ctx, tx, n); err != nil {
		os.Remove(absPath)
		return nil, fmt.Errorf("note.Service.Create: %w", err)
	}

	// Parse and save tags.
	allTags := ParseTags(req.Body, req.Tags)
	if err := s.store.UpdateTags(ctx, tx, id, allTags); err != nil {
		os.Remove(absPath)
		return nil, fmt.Errorf("note.Service.Create: update tags: %w", err)
	}
	n.Tags = allTags

	// Parse and save wikilinks.
	links := ParseWikilinks(req.Body)
	if err := s.store.UpdateLinks(ctx, tx, id, links); err != nil {
		os.Remove(absPath)
		return nil, fmt.Errorf("note.Service.Create: update links: %w", err)
	}

	// Resolve any dangling links pointing to this new note.
	if err := s.store.ResolveDanglingLinks(ctx, tx, id, req.Title, relPath); err != nil {
		os.Remove(absPath)
		return nil, fmt.Errorf("note.Service.Create: resolve dangling links: %w", err)
	}

	if err := tx.Commit(); err != nil {
		os.Remove(absPath)
		return nil, fmt.Errorf("note.Service.Create: commit: %w", err)
	}

	s.logger.Info("note created", "user_id", userID, "note_id", id, "file_path", relPath)
	return n, nil
}

// Get retrieves a note by ID. The body is read from disk (source of truth).
func (s *Service) Get(ctx context.Context, userID, noteID string) (*Note, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("note.Service.Get: open db: %w", err)
	}

	n, err := s.store.Get(ctx, db, noteID)
	if err != nil {
		return nil, fmt.Errorf("note.Service.Get: %w", err)
	}

	// Read body from disk (source of truth).
	notesDir := s.userDBManager.UserNotesDir(userID)
	absPath := filepath.Join(notesDir, n.FilePath)
	content, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File gone but DB has it; return DB body.
			return n, nil
		}
		return nil, fmt.Errorf("note.Service.Get: read file: %w", err)
	}

	_, body, err := ParseFrontmatter(string(content))
	if err != nil {
		return nil, fmt.Errorf("note.Service.Get: parse frontmatter: %w", err)
	}
	n.Body = body

	return n, nil
}

// List returns notes matching the filter. Bodies come from the DB.
func (s *Service) List(ctx context.Context, userID string, filter NoteFilter) ([]*Note, int, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("note.Service.List: open db: %w", err)
	}

	notes, total, err := s.store.List(ctx, db, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("note.Service.List: %w", err)
	}
	return notes, total, nil
}

// GetBacklinks returns all notes that link to the given note.
func (s *Service) GetBacklinks(ctx context.Context, userID, noteID string) ([]*Note, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("note.Service.GetBacklinks: open db: %w", err)
	}

	// Verify note exists.
	if _, err := s.store.Get(ctx, db, noteID); err != nil {
		return nil, fmt.Errorf("note.Service.GetBacklinks: %w", err)
	}

	notes, err := s.store.GetBacklinks(ctx, db, noteID)
	if err != nil {
		return nil, fmt.Errorf("note.Service.GetBacklinks: %w", err)
	}
	return notes, nil
}

// ListTags returns all tags with note counts.
func (s *Service) ListTags(ctx context.Context, userID string) ([]TagCount, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("note.Service.ListTags: open db: %w", err)
	}

	tags, err := s.store.ListTags(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("note.Service.ListTags: %w", err)
	}
	return tags, nil
}

// Update modifies an existing note, rewriting the file and updating the DB.
//
// I-H11: The DB transaction wraps the file write so that a transaction failure
// cannot leave a modified file with stale DB data. Sequence:
//  1. Read old file content (for rollback).
//  2. Start DB transaction; perform all DB writes.
//  3. Write file to disk.
//  4. If file write fails, rollback the transaction.
//  5. Commit the transaction.
//  6. If commit fails, restore old file content (best effort).
func (s *Service) Update(ctx context.Context, userID, noteID string, req UpdateNoteReq) (*Note, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("note.Service.Update: open db: %w", err)
	}

	existing, err := s.store.Get(ctx, db, noteID)
	if err != nil {
		return nil, fmt.Errorf("note.Service.Update: %w", err)
	}

	notesDir := s.userDBManager.UserNotesDir(userID)

	// Apply changes.
	if req.Title != nil {
		existing.Title = *req.Title
	}
	if req.Body != nil {
		existing.Body = *req.Body
	}

	// Track whether a file move is needed so we can defer it.
	var pendingMove struct {
		needed bool
		oldAbs string
		newAbs string
	}

	// Handle project change -- compute new path but defer the rename.
	if req.ProjectID != nil {
		oldPath := existing.FilePath
		newProjectID := *req.ProjectID

		newSubDir := ""
		if newProjectID != "" {
			p, pErr := s.projectStore.Get(ctx, db, newProjectID)
			if pErr != nil {
				return nil, fmt.Errorf("note.Service.Update: resolve project: %w", pErr)
			}
			newSubDir = p.Slug
		}

		// Compute new file path, preserving the filename.
		filename := filepath.Base(existing.FilePath)
		var newRelPath string
		if newSubDir != "" {
			newRelPath = newSubDir + "/" + filename
		} else {
			newRelPath = filename
		}

		if newRelPath != oldPath {
			pendingMove.needed = true
			pendingMove.oldAbs = filepath.Join(notesDir, oldPath)
			pendingMove.newAbs = filepath.Join(notesDir, newRelPath)
			existing.FilePath = newRelPath
		}

		existing.ProjectID = newProjectID
	}

	// Determine project slug for frontmatter.
	projectSlug := ""
	if existing.ProjectID != "" {
		p, pErr := s.projectStore.Get(ctx, db, existing.ProjectID)
		if pErr == nil {
			projectSlug = p.Slug
		}
	}

	existing.UpdatedAt = time.Now().UTC()

	// A-8: Compute merged tags BEFORE building frontmatter so the file
	// on disk always has the correct tags. Previously, existing.Tags (the OLD
	// tags from store.Get) were written to frontmatter, causing reindex to
	// revert tag changes.
	if req.Body != nil || req.Tags != nil {
		fmTags := existing.Tags
		if req.Tags != nil {
			fmTags = *req.Tags
		}
		existing.Tags = ParseTags(existing.Body, fmTags)
	}

	// Build the new .md file content with correct (merged) tags.
	fm := &Frontmatter{
		ID:               existing.ID,
		Title:            existing.Title,
		Project:          projectSlug,
		Tags:             existing.Tags,
		Created:          existing.CreatedAt,
		Modified:         existing.UpdatedAt,
		SourceURL:        existing.SourceURL,
		TranscriptSource: existing.TranscriptSource,
	}

	content, err := SerializeFrontmatter(fm, existing.Body)
	if err != nil {
		return nil, fmt.Errorf("note.Service.Update: serialize: %w", err)
	}

	existing.ContentHash = computeHash(content)

	// Read old file content before any mutations so we can restore on failure.
	absPath := filepath.Join(notesDir, existing.FilePath)
	oldFileAbs := absPath
	if pendingMove.needed {
		oldFileAbs = pendingMove.oldAbs
	}
	oldContent, readErr := os.ReadFile(oldFileAbs)
	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("note.Service.Update: read old file: %w", readErr)
	}

	// --- Begin DB transaction ---
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("note.Service.Update: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback is a no-op after commit

	if err := s.store.Update(ctx, tx, existing); err != nil {
		return nil, fmt.Errorf("note.Service.Update: %w", err)
	}

	// Write already-computed tags to DB (tags were merged above before file write).
	if req.Body != nil || req.Tags != nil {
		if err := s.store.UpdateTags(ctx, tx, noteID, existing.Tags); err != nil {
			return nil, fmt.Errorf("note.Service.Update: update tags: %w", err)
		}
	}

	// Re-parse wikilinks if body changed.
	if req.Body != nil {
		links := ParseWikilinks(existing.Body)
		if err := s.store.UpdateLinks(ctx, tx, noteID, links); err != nil {
			return nil, fmt.Errorf("note.Service.Update: update links: %w", err)
		}
	}

	// --- DB operations succeeded; now perform file operations ---

	// Move the file if the project changed.
	if pendingMove.needed {
		if err := os.MkdirAll(filepath.Dir(pendingMove.newAbs), 0o755); err != nil {
			return nil, fmt.Errorf("note.Service.Update: mkdir: %w", err)
		}
		if sup := s.getSuppressor(); sup != nil {
			sup.IgnoreNext(pendingMove.oldAbs)
			sup.IgnoreNext(pendingMove.newAbs)
		}
		if err := os.Rename(pendingMove.oldAbs, pendingMove.newAbs); err != nil {
			return nil, fmt.Errorf("note.Service.Update: rename file: %w", err)
		}
	}

	// Write the updated .md file.
	if sup := s.getSuppressor(); sup != nil {
		sup.IgnoreNext(absPath)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		// File write failed -- undo the move if we did one.
		if pendingMove.needed {
			if undoErr := os.Rename(pendingMove.newAbs, pendingMove.oldAbs); undoErr != nil {
				s.logger.Error("note.Service.Update: failed to undo file move after write error",
					"from", pendingMove.newAbs, "to", pendingMove.oldAbs, "error", undoErr)
			}
		}
		return nil, fmt.Errorf("note.Service.Update: write file: %w", err)
	}

	// --- Commit ---
	if err := tx.Commit(); err != nil {
		// Commit failed: restore old file content (best effort).
		if oldContent != nil {
			if restoreErr := os.WriteFile(oldFileAbs, oldContent, 0o644); restoreErr != nil {
				s.logger.Error("note.Service.Update: failed to restore file after commit error",
					"path", oldFileAbs, "error", restoreErr)
			}
			// Undo the move if we did one.
			if pendingMove.needed {
				if undoErr := os.Rename(pendingMove.newAbs, pendingMove.oldAbs); undoErr != nil {
					s.logger.Error("note.Service.Update: failed to undo file move after commit error",
						"from", pendingMove.newAbs, "to", pendingMove.oldAbs, "error", undoErr)
				}
			}
		}
		return nil, fmt.Errorf("note.Service.Update: commit: %w", err)
	}

	s.logger.Info("note updated", "user_id", userID, "note_id", noteID)
	return existing, nil
}

// Delete removes a note from disk and the database.
func (s *Service) Delete(ctx context.Context, userID, noteID string) error {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("note.Service.Delete: open db: %w", err)
	}

	existing, err := s.store.Get(ctx, db, noteID)
	if err != nil {
		return fmt.Errorf("note.Service.Delete: %w", err)
	}

	// Delete file from disk.
	notesDir := s.userDBManager.UserNotesDir(userID)
	absPath := filepath.Join(notesDir, existing.FilePath)
	if sup := s.getSuppressor(); sup != nil {
		sup.IgnoreNext(absPath)
	}
	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("note.Service.Delete: remove file: %w", err)
	}

	if err := s.store.Delete(ctx, db, noteID); err != nil {
		return fmt.Errorf("note.Service.Delete: %w", err)
	}

	s.logger.Info("note deleted", "user_id", userID, "note_id", noteID)
	return nil
}

// Reindex is called by the file watcher when an external edit is detected.
// It reads the file, parses frontmatter, and creates or updates the DB record.
func (s *Service) Reindex(ctx context.Context, userID, filePath string) error {
	// A-1: Validate filePath to prevent path traversal.
	if err := validate.Path(filePath); err != nil {
		return fmt.Errorf("note.Service.Reindex: %w", err)
	}

	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("note.Service.Reindex: open db: %w", err)
	}

	notesDir := s.userDBManager.UserNotesDir(userID)

	// A-1: Verify the resolved path stays within the notes directory.
	if err := validate.PathWithinDir(filePath, notesDir); err != nil {
		return fmt.Errorf("note.Service.Reindex: %w", err)
	}

	absPath := filepath.Join(notesDir, filePath)

	content, readErr := os.ReadFile(absPath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			// File was deleted externally. Remove from DB if it exists.
			existing, getErr := s.store.GetByFilePath(ctx, db, filePath)
			if getErr != nil {
				return nil // Not in DB either, nothing to do.
			}
			return s.store.Delete(ctx, db, existing.ID)
		}
		return fmt.Errorf("note.Service.Reindex: read file: %w", readErr)
	}

	hash := computeHash(string(content))

	// Check if note already exists in DB.
	existing, getErr := s.store.GetByFilePath(ctx, db, filePath)
	if getErr == nil {
		// Note exists -- check if content changed.
		if existing.ContentHash == hash {
			return nil // No change.
		}

		// Content changed: update.
		fm, body, parseErr := ParseFrontmatter(string(content))
		if parseErr != nil {
			return fmt.Errorf("note.Service.Reindex: parse frontmatter: %w", parseErr)
		}

		existing.Title = fm.Title
		existing.Body = body
		existing.ContentHash = hash
		// Use modified timestamp from frontmatter if available, else current time.
		if !fm.Modified.IsZero() {
			existing.UpdatedAt = fm.Modified
		} else {
			existing.UpdatedAt = time.Now().UTC()
		}
		if fm.SourceURL != "" {
			existing.SourceURL = fm.SourceURL
		}

		// Re-resolve project from frontmatter slug on content change.
		if fm.Project != "" {
			p, pErr := s.projectStore.GetBySlug(ctx, db, fm.Project)
			if pErr == nil {
				existing.ProjectID = p.ID
			} else {
				s.logger.Warn("note.Service.Reindex: project slug not found, treating as inbox",
					"slug", fm.Project, "file_path", filePath)
				existing.ProjectID = ""
			}
		} else {
			existing.ProjectID = ""
		}

		// A-7: Wrap all DB writes in a transaction for atomicity.
		tx, txErr := db.BeginTx(ctx, nil)
		if txErr != nil {
			return fmt.Errorf("note.Service.Reindex: begin tx: %w", txErr)
		}
		defer tx.Rollback() //nolint:errcheck

		if err := s.store.Update(ctx, tx, existing); err != nil {
			return fmt.Errorf("note.Service.Reindex: update: %w", err)
		}

		allTags := ParseTags(body, fm.Tags)
		if err := s.store.UpdateTags(ctx, tx, existing.ID, allTags); err != nil {
			return fmt.Errorf("note.Service.Reindex: update tags: %w", err)
		}

		links := ParseWikilinks(body)
		if err := s.store.UpdateLinks(ctx, tx, existing.ID, links); err != nil {
			return fmt.Errorf("note.Service.Reindex: update links: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("note.Service.Reindex: commit: %w", err)
		}
		return nil
	}

	// Note is new: create from file.
	fm, body, parseErr := ParseFrontmatter(string(content))
	if parseErr != nil {
		return fmt.Errorf("note.Service.Reindex: parse frontmatter: %w", parseErr)
	}

	now := time.Now().UTC()
	id := fm.ID
	if id == "" {
		id = ulid.MustNew(ulid.Now(), rand.Reader).String()
	}
	title := fm.Title
	if title == "" {
		// Derive title from filename.
		base := filepath.Base(filePath)
		title = strings.TrimSuffix(base, ".md")
	}

	// Use timestamps from frontmatter if available, else current time.
	createdAt := now
	if !fm.Created.IsZero() {
		createdAt = fm.Created
	}
	updatedAt := now
	if !fm.Modified.IsZero() {
		updatedAt = fm.Modified
	}

	n := &Note{
		ID:               id,
		Title:            title,
		FilePath:         filePath,
		Body:             body,
		ContentHash:      hash,
		SourceURL:        fm.SourceURL,
		TranscriptSource: fm.TranscriptSource,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
	}

	// Resolve project from frontmatter slug.
	if fm.Project != "" {
		p, pErr := s.projectStore.GetBySlug(ctx, db, fm.Project)
		if pErr == nil {
			n.ProjectID = p.ID
		}
	}

	// A-7: Wrap all DB writes in a transaction for atomicity.
	tx, txErr := db.BeginTx(ctx, nil)
	if txErr != nil {
		return fmt.Errorf("note.Service.Reindex: begin tx: %w", txErr)
	}
	defer tx.Rollback() //nolint:errcheck

	if err := s.store.Create(ctx, tx, n); err != nil {
		return fmt.Errorf("note.Service.Reindex: create: %w", err)
	}

	allTags := ParseTags(body, fm.Tags)
	if err := s.store.UpdateTags(ctx, tx, id, allTags); err != nil {
		return fmt.Errorf("note.Service.Reindex: update tags: %w", err)
	}

	links := ParseWikilinks(body)
	if err := s.store.UpdateLinks(ctx, tx, id, links); err != nil {
		return fmt.Errorf("note.Service.Reindex: update links: %w", err)
	}

	if err := s.store.ResolveDanglingLinks(ctx, tx, id, title, filePath); err != nil {
		return fmt.Errorf("note.Service.Reindex: resolve dangling links: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("note.Service.Reindex: commit: %w", err)
	}

	// Write ULID back to file if the frontmatter was missing an ID.
	// This prevents duplicate DB entries on next restart.
	// Done after commit since it is a best-effort operation.
	if fm.ID == "" {
		fm.ID = id
		fm.Title = title
		fm.Created = createdAt
		fm.Modified = updatedAt
		newContent, serErr := SerializeFrontmatter(fm, body)
		if serErr != nil {
			s.logger.Warn("note.Service.Reindex: failed to serialize frontmatter for ID writeback",
				"file_path", filePath, "error", serErr)
		} else {
			if sup := s.getSuppressor(); sup != nil {
				sup.IgnoreNext(absPath)
			}
			if writeErr := os.WriteFile(absPath, []byte(newContent), 0o644); writeErr != nil {
				s.logger.Warn("note.Service.Reindex: failed to write ID back to file",
					"file_path", filePath, "error", writeErr)
			}
		}
	}

	return nil
}

// uniqueFilename generates a unique filename in the given directory, appending
// a numeric suffix if a file with that name already exists. Falls back to a
// ULID-based name if no unique name is found within 10000 attempts.
func (s *Service) uniqueFilename(notesDir, subDir, slug string) string {
	dir := notesDir
	if subDir != "" {
		dir = filepath.Join(notesDir, subDir)
	}

	candidate := slug + ".md"
	path := filepath.Join(dir, candidate)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return candidate
	} else if err != nil && !os.IsNotExist(err) {
		// Unexpected error (e.g. permission denied); fall back to ULID name.
		fallback := ulid.MustNew(ulid.Now(), rand.Reader).String() + ".md"
		s.logger.Warn("note.Service.uniqueFilename: stat error, using ULID fallback",
			"path", path, "error", err)
		return fallback
	}

	const maxAttempts = 10000
	for i := 2; i <= maxAttempts; i++ {
		candidate = fmt.Sprintf("%s-%d.md", slug, i)
		path = filepath.Join(dir, candidate)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return candidate
		} else if err != nil && !os.IsNotExist(err) {
			fallback := ulid.MustNew(ulid.Now(), rand.Reader).String() + ".md"
			s.logger.Warn("note.Service.uniqueFilename: stat error, using ULID fallback",
				"path", path, "error", err)
			return fallback
		}
	}

	// Exhausted all attempts; use a ULID-based name.
	fallback := ulid.MustNew(ulid.Now(), rand.Reader).String() + ".md"
	s.logger.Warn("note.Service.uniqueFilename: exhausted attempts, using ULID fallback",
		"slug", slug, "max_attempts", maxAttempts)
	return fallback
}

// computeHash returns the hex-encoded SHA-256 hash of content.
func computeHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h)
}
