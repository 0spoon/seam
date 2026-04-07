package task

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/userdb"
)

// checkboxRe matches markdown checkbox lines: "- [ ] text" or "- [x] text".
var checkboxRe = regexp.MustCompile(`(?m)^[\t ]*- \[([ xX])\] (.+)$`)

// parsedTask holds a checkbox extracted from a note body.
type parsedTask struct {
	LineNumber int
	Content    string
	Done       bool
}

// parseTasks extracts checkbox items from a note body.
func parseTasks(body string) []parsedTask {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	matches := checkboxRe.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return nil
	}

	// Pre-compute line number offsets.
	lines := strings.Split(body, "\n")
	lineOffsets := make([]int, len(lines))
	offset := 0
	for i, line := range lines {
		lineOffsets[i] = offset
		offset += len(line) + 1 // +1 for newline
	}

	var tasks []parsedTask
	for _, m := range matches {
		// m[0],m[1] = full match
		// m[2],m[3] = checkbox state (space or x/X)
		// m[4],m[5] = content text
		matchStart := m[0]
		state := body[m[2]:m[3]]
		content := body[m[4]:m[5]]

		// Find line number (1-indexed).
		lineNum := 1
		for i, off := range lineOffsets {
			if off > matchStart {
				break
			}
			lineNum = i + 1
		}

		tasks = append(tasks, parsedTask{
			LineNumber: lineNum,
			Content:    strings.TrimSpace(content),
			Done:       state == "x" || state == "X",
		})
	}

	return tasks
}

// NoteService defines the note operations needed by the task service.
type NoteService interface {
	Get(ctx context.Context, userID, noteID string) (*note.Note, error)
}

// Service implements task business logic.
type Service struct {
	store     *Store
	dbManager userdb.Manager
	noteSvc   NoteService
	mu        sync.RWMutex // protects noteSvc
	logger    *slog.Logger
}

// NewService creates a new task Service.
func NewService(store *Store, dbManager userdb.Manager, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:     store,
		dbManager: dbManager,
		logger:    logger,
	}
}

// SetNoteService sets the note service dependency after construction
// to break circular dependency during server startup.
// Must only be called during single-threaded startup, before any HTTP requests.
func (s *Service) SetNoteService(noteSvc NoteService) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noteSvc = noteSvc
}

// getNoteService returns the note service safely for concurrent access.
func (s *Service) getNoteService() NoteService {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.noteSvc
}

// SyncNote parses the note body for checkboxes and reconciles them with existing
// tasks, preserving stable IDs and created_at for unchanged tasks.
func (s *Service) SyncNote(ctx context.Context, userID, noteID, body string) error {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("task.Service.SyncNote: open db: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("task.Service.SyncNote: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Fetch existing tasks for reconciliation.
	existing, _, err := s.store.List(ctx, tx, TaskFilter{NoteID: noteID, Limit: 10000})
	if err != nil {
		return fmt.Errorf("task.Service.SyncNote: list existing: %w", err)
	}

	// Build lookup: content -> []*existingEntry (multiple tasks can have same content).
	type existingEntry struct {
		task    *Task
		matched bool
	}
	existingByContent := make(map[string][]*existingEntry)
	for _, t := range existing {
		existingByContent[t.Content] = append(existingByContent[t.Content], &existingEntry{task: t})
	}

	parsed := parseTasks(body)
	now := time.Now().UTC()
	matched := make(map[string]bool) // track matched existing task IDs

	for _, p := range parsed {
		// Try to match an existing unmatched task with the same content.
		var found *existingEntry
		if entries, ok := existingByContent[p.Content]; ok {
			for _, e := range entries {
				if !e.matched {
					found = e
					e.matched = true
					break
				}
			}
		}

		if found != nil {
			matched[found.task.ID] = true
			// Update line number and done status if changed.
			if found.task.LineNumber != p.LineNumber || found.task.Done != p.Done {
				found.task.LineNumber = p.LineNumber
				found.task.Done = p.Done
				found.task.UpdatedAt = now
				if err := s.store.Upsert(ctx, tx, found.task); err != nil {
					return fmt.Errorf("task.Service.SyncNote: update: %w", err)
				}
			}
		} else {
			// Insert new task.
			id, idErr := ulid.New(ulid.Now(), rand.Reader)
			if idErr != nil {
				return fmt.Errorf("task.Service.SyncNote: generate id: %w", idErr)
			}
			t := &Task{
				ID:         id.String(),
				NoteID:     noteID,
				LineNumber: p.LineNumber,
				Content:    p.Content,
				Done:       p.Done,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			if err := s.store.Upsert(ctx, tx, t); err != nil {
				return fmt.Errorf("task.Service.SyncNote: insert: %w", err)
			}
		}
	}

	// Delete tasks that no longer exist in the note.
	for _, t := range existing {
		if !matched[t.ID] {
			if err := s.store.Delete(ctx, tx, t.ID); err != nil {
				return fmt.Errorf("task.Service.SyncNote: delete: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("task.Service.SyncNote: commit: %w", err)
	}

	return nil
}

// List returns tasks matching the filter.
func (s *Service) List(ctx context.Context, userID string, filter TaskFilter) ([]*Task, int, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("task.Service.List: open db: %w", err)
	}

	tasks, total, err := s.store.List(ctx, db, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("task.Service.List: %w", err)
	}
	return tasks, total, nil
}

// Summary returns aggregate task counts.
func (s *Service) Summary(ctx context.Context, userID string, filter TaskFilter) (*TaskSummary, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("task.Service.Summary: open db: %w", err)
	}

	summary, err := s.store.Summary(ctx, db, filter)
	if err != nil {
		return nil, fmt.Errorf("task.Service.Summary: %w", err)
	}
	return summary, nil
}

// Get returns a single task by ID.
func (s *Service) Get(ctx context.Context, userID, taskID string) (*Task, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("task.Service.Get: open db: %w", err)
	}

	t, err := s.store.Get(ctx, db, taskID)
	if err != nil {
		return nil, fmt.Errorf("task.Service.Get: %w", err)
	}
	return t, nil
}

// ToggleDone updates a task's done status and also toggles the checkbox in
// the note file on disk. DB operations are wrapped in a transaction, and
// the file write is performed before commit so failures are not silently lost.
func (s *Service) ToggleDone(ctx context.Context, userID, taskID string, done bool) error {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("task.Service.ToggleDone: open db: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("task.Service.ToggleDone: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	t, err := s.store.Get(ctx, tx, taskID)
	if err != nil {
		return fmt.Errorf("task.Service.ToggleDone: %w", err)
	}

	if err := s.store.UpdateDone(ctx, tx, taskID, done); err != nil {
		return fmt.Errorf("task.Service.ToggleDone: %w", err)
	}

	// Update the file BEFORE committing DB transaction.
	if s.getNoteService() != nil {
		if err := s.toggleCheckboxInFile(ctx, userID, t.NoteID, t.LineNumber, done); err != nil {
			return fmt.Errorf("task.Service.ToggleDone: file update: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("task.Service.ToggleDone: commit: %w", err)
	}
	return nil
}

// toggleCheckboxInFile reads the note file, toggles the checkbox on the
// specified line, and writes the file back.
func (s *Service) toggleCheckboxInFile(ctx context.Context, userID, noteID string, lineNumber int, done bool) error {
	noteSvc := s.getNoteService()
	if noteSvc == nil {
		return fmt.Errorf("note service not configured")
	}
	n, err := noteSvc.Get(ctx, userID, noteID)
	if err != nil {
		return fmt.Errorf("get note: %w", err)
	}

	// Read the actual file from disk.
	notesDir := s.dbManager.UserNotesDir(userID)
	absPath := filepath.Join(notesDir, n.FilePath)

	// Defense-in-depth: reject paths that escape the notes directory.
	if !strings.HasPrefix(absPath, filepath.Clean(notesDir)+string(filepath.Separator)) && absPath != filepath.Clean(notesDir) {
		return fmt.Errorf("task.Service.toggleCheckboxInFile: path traversal detected")
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	// Detect original line ending style before normalization.
	rawStr := string(content)
	useCRLF := strings.Contains(rawStr, "\r\n")

	fileStr := strings.ReplaceAll(rawStr, "\r\n", "\n")
	fileStr = strings.ReplaceAll(fileStr, "\r", "\n")

	lines := strings.Split(fileStr, "\n")

	// Find the target line (1-indexed). The line number refers to the body,
	// but the file includes frontmatter. We need to find the checkbox line
	// in the full file content.
	// Parse frontmatter to find where body starts.
	bodyStart := 0
	if strings.HasPrefix(fileStr, "---\n") {
		// Find closing "---" that starts on its own line.
		rest := fileStr[3:] // skip opening "---"
		nlIdx := strings.Index(rest, "\n")
		if nlIdx >= 0 {
			rest = rest[nlIdx+1:]
			endIdx := strings.Index(rest, "\n---\n")
			closingLen := 5 // length of "\n---\n"
			if endIdx < 0 && strings.HasSuffix(rest, "\n---") {
				endIdx = len(rest) - 4
				closingLen = 4 // length of "\n---" (no trailing newline)
			}
			if endIdx >= 0 {
				// bodyStart = opening "---" (3) + newline after opening (nlIdx+1) +
				//             content before closing (endIdx) + closing delimiter length
				bodyStart = 3 + nlIdx + 1 + endIdx + closingLen
				if bodyStart > len(fileStr) {
					bodyStart = len(fileStr)
				}
				fmLines := strings.Count(fileStr[:bodyStart], "\n")
				lineNumber = lineNumber + fmLines
			}
		}
	}

	if lineNumber < 1 || lineNumber > len(lines) {
		return fmt.Errorf("line number %d out of range", lineNumber)
	}

	line := lines[lineNumber-1]
	var newLine string
	if done {
		newLine = strings.Replace(line, "[ ]", "[x]", 1)
	} else {
		newLine = strings.Replace(line, "[x]", "[ ]", 1)
		newLine = strings.Replace(newLine, "[X]", "[ ]", 1)
	}

	if newLine == line {
		return nil // no change needed
	}

	lines[lineNumber-1] = newLine
	sep := "\n"
	if useCRLF {
		sep = "\r\n"
	}
	newContent := strings.Join(lines, sep)

	// Preserve original file permissions.
	info, statErr := os.Stat(absPath)
	perm := os.FileMode(0o644)
	if statErr == nil {
		perm = info.Mode().Perm()
	}
	if err := note.AtomicWriteFile(absPath, []byte(newContent), perm); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}
