package note

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
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
	versionStore  *VersionStore
	projectStore  *project.Store
	userDBManager userdb.Manager
	suppressorMu  sync.RWMutex
	suppressor    WriteSuppressor
	logger        *slog.Logger
}

// NewService creates a new note Service.
func NewService(
	store *SQLStore,
	versionStore *VersionStore,
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
		versionStore:  versionStore,
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
	if err := validate.Name(req.Title); err != nil {
		return nil, fmt.Errorf("note.Service.Create: %w", err)
	}

	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("note.Service.Create: open db: %w", err)
	}

	notesDir := s.userDBManager.UserNotesDir(userID)
	now := time.Now().UTC()
	idVal, idErr := ulid.New(ulid.Now(), rand.Reader)
	if idErr != nil {
		return nil, fmt.Errorf("note.Service.Create: generate id: %w", idErr)
	}
	id := idVal.String()

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

	if err := atomicWriteFile(absPath, []byte(content), 0o644); err != nil {
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
	if err := validate.PathWithinDir(n.FilePath, notesDir); err != nil {
		return nil, fmt.Errorf("note.Service.Get: %w", err)
	}
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

// ResolveWikilink resolves a wikilink target string to a note ID.
func (s *Service) ResolveWikilink(ctx context.Context, userID, title string) (string, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("note.Service.ResolveWikilink: %w", err)
	}
	return s.store.ResolveLink(ctx, db, title)
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
	if req.Title != nil {
		if err := validate.Name(*req.Title); err != nil {
			return nil, fmt.Errorf("note.Service.Update: %w", err)
		}
	}

	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("note.Service.Update: open db: %w", err)
	}

	existing, err := s.store.Get(ctx, db, noteID)
	if err != nil {
		return nil, fmt.Errorf("note.Service.Update: %w", err)
	}

	// Capture pre-update state for version history.
	oldTitle := existing.Title
	oldBody := existing.Body
	oldContentHash := existing.ContentHash

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
	if err := atomicWriteFile(absPath, []byte(content), 0o644); err != nil {
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
			if restoreErr := atomicWriteFile(oldFileAbs, oldContent, 0o644); restoreErr != nil {
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

	// Create a version snapshot from the pre-update content if the hash changed.
	if s.versionStore != nil && existing.ContentHash != oldContentHash {
		nextVer, verErr := s.versionStore.NextVersion(ctx, db, noteID)
		if verErr != nil {
			s.logger.Error("note.Service.Update: failed to get next version number",
				"note_id", noteID, "error", verErr)
		} else {
			v := &NoteVersion{
				NoteID:      noteID,
				Version:     nextVer,
				Title:       oldTitle,
				Body:        oldBody,
				ContentHash: oldContentHash,
			}
			if createErr := s.versionStore.Create(ctx, db, v); createErr != nil {
				s.logger.Error("note.Service.Update: failed to create version",
					"note_id", noteID, "version", nextVer, "error", createErr)
			} else {
				// Cleanup old versions, keeping at most 50.
				if cleanupErr := s.versionStore.Cleanup(ctx, db, noteID, 50); cleanupErr != nil {
					s.logger.Error("note.Service.Update: version cleanup failed",
						"note_id", noteID, "error", cleanupErr)
				}
			}
		}
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

	// Validate file path before any operations.
	notesDir := s.userDBManager.UserNotesDir(userID)
	if err := validate.PathWithinDir(existing.FilePath, notesDir); err != nil {
		return fmt.Errorf("note.Service.Delete: %w", err)
	}
	absPath := filepath.Join(notesDir, existing.FilePath)

	// Delete DB row first, then remove file on success (matching BulkAction pattern).
	if err := s.store.Delete(ctx, db, noteID); err != nil {
		return fmt.Errorf("note.Service.Delete: %w", err)
	}

	// After successful DB delete, remove file from disk.
	if sup := s.getSuppressor(); sup != nil {
		sup.IgnoreNext(absPath)
	}
	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		s.logger.Error("note.Service.Delete: remove file after db delete",
			"path", absPath, "error", err)
	}

	s.logger.Info("note deleted", "user_id", userID, "note_id", noteID)
	return nil
}

// BulkAction performs a bulk operation on multiple notes within a single
// transaction. Supported actions: add_tag, remove_tag, move, delete.
func (s *Service) BulkAction(ctx context.Context, userID string, req BulkActionReq) (*BulkActionResult, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("note.Service.BulkAction: open db: %w", err)
	}

	result := &BulkActionResult{}

	// Validate action and params.
	switch req.Action {
	case "add_tag", "remove_tag":
		if req.Params.Tag == "" {
			return nil, fmt.Errorf("note.Service.BulkAction: tag parameter is required")
		}
		if err := validate.Name(req.Params.Tag); err != nil {
			return nil, fmt.Errorf("note.Service.BulkAction: invalid tag name: %w", err)
		}
	case "move":
		// project_id can be empty (= inbox)
	case "delete":
		// no params needed
	default:
		return nil, fmt.Errorf("note.Service.BulkAction: unknown action %q", req.Action)
	}

	// Execute within a transaction.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("note.Service.BulkAction: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback is a no-op after commit

	// For delete, collect file paths to remove after the DB transaction commits.
	var filesToDelete []string
	notesDir := s.userDBManager.UserNotesDir(userID)

	for _, noteID := range req.NoteIDs {
		if err := s.executeBulkAction(ctx, tx, db, userID, noteID, req, notesDir, &filesToDelete); err != nil {
			result.Failed++
			// Sanitize error: map known domain errors to safe messages.
			errMsg := "internal error"
			if errors.Is(err, ErrNotFound) {
				errMsg = "not found"
			} else if errors.Is(err, validate.ErrUnsafeName) {
				errMsg = "validation error"
			}
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", noteID, errMsg))
			continue
		}
		result.Success++
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("note.Service.BulkAction: commit: %w", err)
	}

	// After successful commit, delete files from disk for bulk delete.
	for _, absPath := range filesToDelete {
		if sup := s.getSuppressor(); sup != nil {
			sup.IgnoreNext(absPath)
		}
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			s.logger.Error("note.Service.BulkAction: remove file after delete",
				"path", absPath, "error", err)
		}
	}

	s.logger.Info("bulk action completed",
		"user_id", userID, "action", req.Action,
		"success", result.Success, "failed", result.Failed)
	return result, nil
}

// executeBulkAction handles a single note within a bulk operation transaction.
func (s *Service) executeBulkAction(
	ctx context.Context,
	tx *sql.Tx,
	db *sql.DB,
	userID, noteID string,
	req BulkActionReq,
	notesDir string,
	filesToDelete *[]string,
) error {
	switch req.Action {
	case "add_tag":
		existing, err := s.store.Get(ctx, tx, noteID)
		if err != nil {
			return err
		}
		// Check if tag already present.
		for _, t := range existing.Tags {
			if t == req.Params.Tag {
				return nil // Already has the tag, no-op.
			}
		}
		tags := append(existing.Tags, req.Params.Tag)
		return s.store.UpdateTags(ctx, tx, noteID, tags)

	case "remove_tag":
		existing, err := s.store.Get(ctx, tx, noteID)
		if err != nil {
			return err
		}
		tags := make([]string, 0, len(existing.Tags))
		for _, t := range existing.Tags {
			if t != req.Params.Tag {
				tags = append(tags, t)
			}
		}
		return s.store.UpdateTags(ctx, tx, noteID, tags)

	case "move":
		existing, err := s.store.Get(ctx, tx, noteID)
		if err != nil {
			return err
		}

		newProjectID := req.Params.ProjectID
		if existing.ProjectID == newProjectID {
			return nil // Already in the target project, no-op.
		}

		// Compute new file path for the moved note.
		newSubDir := ""
		if newProjectID != "" {
			p, pErr := s.projectStore.Get(ctx, db, newProjectID)
			if pErr != nil {
				return fmt.Errorf("resolve project: %w", pErr)
			}
			newSubDir = p.Slug
		}

		filename := filepath.Base(existing.FilePath)
		var newRelPath string
		if newSubDir != "" {
			newRelPath = newSubDir + "/" + filename
		} else {
			newRelPath = filename
		}

		// Validate the destination path stays within the notes directory.
		if vErr := validate.PathWithinDir(newRelPath, notesDir); vErr != nil {
			return fmt.Errorf("move: %w", vErr)
		}

		oldAbs := filepath.Join(notesDir, existing.FilePath)
		newAbs := filepath.Join(notesDir, newRelPath)

		// Move the file on disk.
		if newRelPath != existing.FilePath {
			if err := os.MkdirAll(filepath.Dir(newAbs), 0o755); err != nil {
				return fmt.Errorf("mkdir for move: %w", err)
			}
			if sup := s.getSuppressor(); sup != nil {
				sup.IgnoreNext(oldAbs)
				sup.IgnoreNext(newAbs)
			}
			if err := os.Rename(oldAbs, newAbs); err != nil {
				return fmt.Errorf("rename file: %w", err)
			}
		}

		existing.ProjectID = newProjectID
		existing.FilePath = newRelPath
		existing.UpdatedAt = time.Now().UTC()
		return s.store.Update(ctx, tx, existing)

	case "delete":
		existing, err := s.store.Get(ctx, tx, noteID)
		if err != nil {
			return err
		}
		absPath := filepath.Join(notesDir, existing.FilePath)
		*filesToDelete = append(*filesToDelete, absPath)
		return s.store.Delete(ctx, tx, noteID)
	}

	return nil
}

// ListVersions returns version history for a note.
func (s *Service) ListVersions(ctx context.Context, userID, noteID string, limit, offset int) ([]*NoteVersion, int, error) {
	if s.versionStore == nil {
		return nil, 0, fmt.Errorf("note.Service.ListVersions: version history not configured")
	}

	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("note.Service.ListVersions: open db: %w", err)
	}

	// Verify note exists.
	if _, err := s.store.Get(ctx, db, noteID); err != nil {
		return nil, 0, fmt.Errorf("note.Service.ListVersions: %w", err)
	}

	versions, total, err := s.versionStore.List(ctx, db, noteID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("note.Service.ListVersions: %w", err)
	}
	return versions, total, nil
}

// GetVersion returns a specific version of a note.
func (s *Service) GetVersion(ctx context.Context, userID, noteID string, version int) (*NoteVersion, error) {
	if s.versionStore == nil {
		return nil, fmt.Errorf("note.Service.GetVersion: version history not configured")
	}

	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("note.Service.GetVersion: open db: %w", err)
	}

	// Verify note exists.
	if _, err := s.store.Get(ctx, db, noteID); err != nil {
		return nil, fmt.Errorf("note.Service.GetVersion: %w", err)
	}

	v, err := s.versionStore.Get(ctx, db, noteID, version)
	if err != nil {
		return nil, fmt.Errorf("note.Service.GetVersion: %w", err)
	}
	return v, nil
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
		idVal, idErr := ulid.New(ulid.Now(), rand.Reader)
		if idErr != nil {
			return fmt.Errorf("note.Service.Reindex: generate id: %w", idErr)
		}
		id = idVal.String()
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
			if writeErr := atomicWriteFile(absPath, []byte(newContent), 0o644); writeErr != nil {
				s.logger.Warn("note.Service.Reindex: failed to write ID back to file",
					"file_path", filePath, "error", writeErr)
			}
		}
	}

	return nil
}

// AppendToNote appends a timestamped line to an existing note's body.
func (s *Service) AppendToNote(ctx context.Context, userID, noteID, text string) (*Note, error) {
	existing, err := s.Get(ctx, userID, noteID)
	if err != nil {
		return nil, fmt.Errorf("note.Service.AppendToNote: %w", err)
	}

	timestamp := time.Now().Format("15:04")
	appendLine := "\n- " + timestamp + " -- " + text
	newBody := existing.Body + appendLine

	updated, err := s.Update(ctx, userID, noteID, UpdateNoteReq{
		Body: &newBody,
	})
	if err != nil {
		return nil, fmt.Errorf("note.Service.AppendToNote: %w", err)
	}

	return updated, nil
}

// GetOrCreateDaily returns the daily note for the given date, creating one
// if it does not yet exist.
func (s *Service) GetOrCreateDaily(ctx context.Context, userID string, date time.Time) (*Note, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("note.Service.GetOrCreateDaily: open db: %w", err)
	}

	// Search for existing daily note by date prefix and tag.
	datePrefix := date.Format("2006-01-02")
	existing, err := s.store.FindByTitlePrefix(ctx, db, datePrefix, "daily")
	if err == nil {
		// Read body from disk (source of truth).
		notesDir := s.userDBManager.UserNotesDir(userID)
		if vErr := validate.PathWithinDir(existing.FilePath, notesDir); vErr != nil {
			return nil, fmt.Errorf("note.Service.GetOrCreateDaily: %w", vErr)
		}
		absPath := filepath.Join(notesDir, existing.FilePath)
		content, readErr := os.ReadFile(absPath)
		if readErr == nil {
			_, body, parseErr := ParseFrontmatter(string(content))
			if parseErr == nil {
				existing.Body = body
			}
		}
		return existing, nil
	}

	// Not found -- create a new daily note.
	title := date.Format("2006-01-02 Monday")
	dateStr := date.Format("2006-01-02")
	weekdayStr := date.Format("Monday")

	body := fmt.Sprintf("# %s %s\n\n## Notes\n\n## Tasks\n\n- [ ] \n", dateStr, weekdayStr)

	// Try the daily-log template if the handler has a template applier.
	// The service does not have direct access to the template applier, so we
	// use the hardcoded default above. The handler can override this if needed.

	n, err := s.Create(ctx, userID, CreateNoteReq{
		Title: title,
		Body:  body,
		Tags:  []string{"daily"},
	})
	if err != nil {
		return nil, fmt.Errorf("note.Service.GetOrCreateDaily: %w", err)
	}

	return n, nil
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
		fallback := s.generateFallbackFilename()
		s.logger.Warn("note.Service.uniqueFilename: stat error, using fallback",
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
			fallback := s.generateFallbackFilename()
			s.logger.Warn("note.Service.uniqueFilename: stat error, using fallback",
				"path", path, "error", err)
			return fallback
		}
	}

	// Exhausted all attempts; use a fallback name.
	fallback := s.generateFallbackFilename()
	s.logger.Warn("note.Service.uniqueFilename: exhausted attempts, using fallback",
		"slug", slug, "max_attempts", maxAttempts)
	return fallback
}

// generateFallbackFilename creates a unique filename using ULID with graceful
// fallback to a timestamp-based name if entropy source fails.
func (s *Service) generateFallbackFilename() string {
	id, err := ulid.New(ulid.Now(), rand.Reader)
	if err != nil {
		s.logger.Warn("note.Service.generateFallbackFilename: ulid generation failed, using timestamp",
			"error", err)
		return fmt.Sprintf("note-%d.md", time.Now().UnixNano())
	}
	return id.String() + ".md"
}

// computeHash returns the hex-encoded SHA-256 hash of content.
func computeHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h)
}

// atomicWriteFile writes data to a file atomically by writing to a temp file
// in the same directory and then renaming. This prevents partial writes from
// corrupting the source-of-truth .md files on crash.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".seam-tmp-*")
	if err != nil {
		return fmt.Errorf("atomicWriteFile: create temp: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("atomicWriteFile: write: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("atomicWriteFile: chmod: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomicWriteFile: close: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomicWriteFile: rename: %w", err)
	}
	return nil
}
