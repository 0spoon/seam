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
	store       *Store
	dbManager   userdb.Manager
	noteSvc     NoteService
	logger      *slog.Logger
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
func (s *Service) SetNoteService(noteSvc NoteService) {
	s.noteSvc = noteSvc
}

// SyncNote parses the note body for checkboxes and syncs them to the task index.
// Deletes all existing tasks for the note and re-inserts (simpler than diffing,
// ensures line numbers are always accurate).
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

	// Delete all existing tasks for this note.
	if err := s.store.DeleteByNote(ctx, tx, noteID); err != nil {
		return fmt.Errorf("task.Service.SyncNote: %w", err)
	}

	// Parse and insert new tasks.
	parsed := parseTasks(body)
	now := time.Now().UTC()
	for _, p := range parsed {
		t := &Task{
			ID:         ulid.MustNew(ulid.Now(), rand.Reader).String(),
			NoteID:     noteID,
			LineNumber: p.LineNumber,
			Content:    p.Content,
			Done:       p.Done,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := s.store.Upsert(ctx, tx, t); err != nil {
			return fmt.Errorf("task.Service.SyncNote: %w", err)
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
// the note file on disk.
func (s *Service) ToggleDone(ctx context.Context, userID, taskID string, done bool) error {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("task.Service.ToggleDone: open db: %w", err)
	}

	// Get the task to find the note and line number.
	t, err := s.store.Get(ctx, db, taskID)
	if err != nil {
		return fmt.Errorf("task.Service.ToggleDone: %w", err)
	}

	// Update DB status.
	if err := s.store.UpdateDone(ctx, db, taskID, done); err != nil {
		return fmt.Errorf("task.Service.ToggleDone: %w", err)
	}

	// Update the note file on disk.
	if s.noteSvc != nil {
		if err := s.toggleCheckboxInFile(ctx, userID, t.NoteID, t.LineNumber, done); err != nil {
			s.logger.Warn("task.Service.ToggleDone: failed to update file",
				"task_id", taskID, "note_id", t.NoteID, "error", err)
		}
	}

	return nil
}

// toggleCheckboxInFile reads the note file, toggles the checkbox on the
// specified line, and writes the file back.
func (s *Service) toggleCheckboxInFile(ctx context.Context, userID, noteID string, lineNumber int, done bool) error {
	n, err := s.noteSvc.Get(ctx, userID, noteID)
	if err != nil {
		return fmt.Errorf("get note: %w", err)
	}

	// Read the actual file from disk.
	notesDir := s.dbManager.UserNotesDir(userID)
	absPath := filepath.Join(notesDir, n.FilePath)

	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	lines := strings.Split(string(content), "\n")

	// Find the target line (1-indexed). The line number refers to the body,
	// but the file includes frontmatter. We need to find the checkbox line
	// in the full file content.
	// Parse frontmatter to find where body starts.
	fileStr := string(content)
	bodyStart := 0
	if strings.HasPrefix(fileStr, "---") {
		endIdx := strings.Index(fileStr[3:], "---")
		if endIdx >= 0 {
			// Body starts after the closing "---\n"
			bodyStart = endIdx + 3 + 3 // skip opening "---" + offset + closing "---"
			// Count lines in frontmatter.
			fmLines := strings.Count(fileStr[:bodyStart], "\n")
			// Adjust line number to be relative to the full file.
			lineNumber = lineNumber + fmLines
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
	newContent := strings.Join(lines, "\n")

	if err := os.WriteFile(absPath, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}
