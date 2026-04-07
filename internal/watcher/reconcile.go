package watcher

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Reconcile walks a user's notes directory and calls the handler for every
// .md file that has changed since the server was last running. It uses a
// two-pass approach:
//  1. Fast pass: compare file mtime against DB updated_at. Only files whose
//     mtime is newer than the DB timestamp are passed to the handler (which
//     does the more expensive content_hash comparison).
//  2. Detect deleted files: query all file_paths from the DB, check if each
//     exists on disk, and call the handler for missing files (which will
//     delete the orphaned DB row).
//
// If db is nil, falls back to calling the handler for every file (no mtime
// optimization, no delete detection).
func Reconcile(ctx context.Context, userID string, notesDir string, handler FileEventHandler, db *sql.DB) error {
	absDir, err := filepath.Abs(notesDir)
	if err != nil {
		return fmt.Errorf("watcher.Reconcile: abs path: %w", err)
	}

	info, err := os.Stat(absDir)
	if err != nil {
		return fmt.Errorf("watcher.Reconcile: stat %s: %w", absDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("watcher.Reconcile: %s is not a directory", absDir)
	}

	// Build a map of file_path -> updated_at from the database for fast
	// mtime comparison. If db is nil, skip this optimization.
	var dbTimes map[string]time.Time
	if db != nil {
		dbTimes, err = loadDBFileTimes(ctx, db)
		if err != nil {
			slog.Warn("watcher.Reconcile: failed to load DB file times, falling back to full scan",
				"error", err)
			dbTimes = nil
		}
	}

	// Track which files we see on disk so we can detect DB-only entries.
	diskFiles := make(map[string]bool)

	err = filepath.WalkDir(absDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			// Skip symlinked directories to prevent indexing outside notes dir.
			if d.Type()&os.ModeSymlink != 0 {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip symlinked files.
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}

		// Check for cancellation between files.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relPath, relErr := filepath.Rel(absDir, path)
		if relErr != nil {
			return fmt.Errorf("watcher.Reconcile: rel path: %w", relErr)
		}

		diskFiles[relPath] = true

		// Fast pass: skip files whose mtime has not changed.
		// C-21: RFC3339 stores only second precision, so we add 1 second
		// to the DB time to compensate for sub-second mtime. Any file
		// whose mtime is strictly after (dbTime + 1s) is considered changed.
		if dbTimes != nil {
			if dbTime, found := dbTimes[relPath]; found {
				fInfo, statErr := d.Info()
				if statErr == nil && !fInfo.ModTime().After(dbTime.Add(time.Second)) {
					return nil
				}
			}
		}

		if handlerErr := handler(ctx, userID, relPath); handlerErr != nil {
			return fmt.Errorf("watcher.Reconcile: handler for %s: %w", relPath, handlerErr)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("watcher.Reconcile: %w", err)
	}

	// Detect deleted files: DB rows with no corresponding file on disk.
	// C-22: Check for context cancellation in each iteration so the loop
	// does not block shutdown when there are many orphaned DB entries.
	for relPath := range dbTimes {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if diskFiles[relPath] {
			continue
		}
		// File is in DB but not on disk. Call the handler which will
		// detect the missing file and delete the DB row.
		if handlerErr := handler(ctx, userID, relPath); handlerErr != nil {
			slog.Warn("watcher.Reconcile: handler error for deleted file",
				"file_path", relPath, "error", handlerErr)
		}
	}

	return nil
}

// loadDBFileTimes queries all note file_paths and their updated_at timestamps.
func loadDBFileTimes(ctx context.Context, db *sql.DB) (map[string]time.Time, error) {
	rows, err := db.QueryContext(ctx, `SELECT file_path, updated_at FROM notes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]time.Time)
	for rows.Next() {
		var fp, updatedAt string
		if scanErr := rows.Scan(&fp, &updatedAt); scanErr != nil {
			slog.Warn("watcher.loadDBFileTimes: scan error", "error", scanErr)
			continue
		}
		if t, parseErr := time.Parse(time.RFC3339, updatedAt); parseErr == nil {
			result[fp] = t
		}
	}
	return result, rows.Err()
}
