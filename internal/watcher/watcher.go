// Package watcher watches users' notes directories for file changes and
// triggers reindexing via a callback.
package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileEventHandler is called when a file change is detected. The filePath
// argument is relative to the user's notes directory. Defined here so the
// watcher package does NOT import the note package.
type FileEventHandler func(ctx context.Context, userID, filePath string) error

// Watcher watches users' notes directories for file changes.
type Watcher struct {
	fsWatcher *fsnotify.Watcher
	handler   FileEventHandler
	debounce  time.Duration
	logger    *slog.Logger

	mu         sync.Mutex
	userDirs   map[string]string    // userID -> notesDir (absolute)
	suppressed map[string]time.Time // absolute file path -> expiry time

	// pending tracks debounce timers keyed by absolute file path.
	pending map[string]*time.Timer

	// debounceGen tracks the generation counter per file path. Each new
	// debounce event increments the generation so stale timer callbacks
	// (that fired after Stop returned false) can detect they are outdated.
	debounceGen map[string]uint64

	ctx    context.Context
	cancel context.CancelFunc
}

// NewWatcher creates a new Watcher backed by fsnotify. The handler is called
// for every detected .md file change after debouncing. The debounce duration
// controls how long the watcher waits after the last event for a file before
// calling the handler.
func NewWatcher(handler FileEventHandler, debounce time.Duration, logger *slog.Logger) (*Watcher, error) {
	if handler == nil {
		return nil, fmt.Errorf("watcher.NewWatcher: handler must not be nil")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watcher.NewWatcher: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Watcher{
		fsWatcher:   fsw,
		handler:     handler,
		debounce:    debounce,
		logger:      logger,
		userDirs:    make(map[string]string),
		suppressed:  make(map[string]time.Time),
		pending:     make(map[string]*time.Timer),
		debounceGen: make(map[string]uint64),
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// Watch adds a user's notes directory to the watch list. The directory and
// all subdirectories are watched recursively.
// C-23: The mutex is held during the entire operation (including WalkDir
// and fsWatcher.Add calls) to prevent concurrent Watch/Unwatch from
// creating inconsistent state.
func (w *Watcher) Watch(userID, notesDir string) error {
	absDir, err := filepath.Abs(notesDir)
	if err != nil {
		return fmt.Errorf("watcher.Watch: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.userDirs[userID] = absDir

	// Walk the directory tree and add each directory to fsnotify.
	err = filepath.WalkDir(absDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if addErr := w.fsWatcher.Add(path); addErr != nil {
				return fmt.Errorf("watcher.Watch: add %s: %w", path, addErr)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("watcher.Watch: walk %s: %w", absDir, err)
	}

	w.logger.Info("watching notes directory", "user_id", userID, "dir", absDir)
	return nil
}

// Unwatch removes a user's notes directory from the watch list.
// C-23: The mutex is held during the entire operation to prevent concurrent
// Watch/Unwatch from creating inconsistent state.
func (w *Watcher) Unwatch(userID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	absDir, ok := w.userDirs[userID]
	if !ok {
		return fmt.Errorf("watcher.Unwatch: user %s not watched", userID)
	}
	delete(w.userDirs, userID)

	// Remove all subdirectories under the user's notes dir from fsnotify.
	err := filepath.WalkDir(absDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Directory may already be gone; skip gracefully.
			return nil
		}
		if d.IsDir() {
			_ = w.fsWatcher.Remove(path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("watcher.Unwatch: walk %s: %w", absDir, err)
	}

	w.logger.Info("unwatched notes directory", "user_id", userID, "dir", absDir)
	return nil
}

// IgnoreNext suppresses the next event for the given absolute file path.
// This is used for self-write suppression so that writes performed by the
// application do not trigger a re-index loop. The suppression expires after
// 2 seconds.
func (w *Watcher) IgnoreNext(filePath string) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		w.logger.Warn("watcher.IgnoreNext: failed to resolve path", "path", filePath, "error", err)
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.suppressed[absPath] = time.Now().Add(2 * time.Second)
}

// Run processes filesystem events in a blocking loop. It debounces rapid
// changes to the same file and calls the handler when the debounce period
// elapses without further changes. Run returns when the context is cancelled
// or the watcher is closed.
func (w *Watcher) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.ctx.Done():
			return nil
		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return nil
			}
			w.handleEvent(ctx, event)
		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return nil
			}
			w.logger.Error("fsnotify error", "error", err)
		}
	}
}

// Close stops the watcher and releases resources.
func (w *Watcher) Close() error {
	w.cancel()

	w.mu.Lock()
	for _, t := range w.pending {
		t.Stop()
	}
	w.pending = make(map[string]*time.Timer)
	w.mu.Unlock()

	return w.fsWatcher.Close()
}

// handleEvent processes a single fsnotify event.
func (w *Watcher) handleEvent(ctx context.Context, event fsnotify.Event) {
	absPath := event.Name

	// If a new directory was created, start watching it so we pick up
	// files created inside subdirectories.
	if event.Has(fsnotify.Create) {
		// Use Lstat so symlinks are not followed. With Lstat, IsDir() returns
		// false for symlinks (even those pointing to directories), so we only
		// watch real directories.
		if info, err := os.Lstat(absPath); err == nil && info.IsDir() {
			if addErr := w.fsWatcher.Add(absPath); addErr != nil {
				w.logger.Warn("watcher: failed to add new directory", "path", absPath, "error", addErr)
			}
			return
		}
	}

	// Only process .md files.
	if !strings.HasSuffix(absPath, ".md") {
		return
	}

	// Check suppression list.
	if w.checkAndClearSuppression(absPath) {
		w.logger.Debug("watcher: suppressed event", "path", absPath, "op", event.Op)
		return
	}

	// Resolve which user this file belongs to and compute the relative path.
	userID, relPath, ok := w.resolveUser(absPath)
	if !ok {
		w.logger.Warn("watcher: event for unknown directory", "path", absPath)
		return
	}

	// Remove events fire immediately (no debounce).
	if event.Has(fsnotify.Remove) {
		w.fireHandler(ctx, userID, relPath, absPath)
		return
	}

	// Create, Write, and Rename events are debounced.
	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) || event.Has(fsnotify.Rename) {
		w.debounceEvent(ctx, userID, relPath, absPath)
	}
}

// checkAndClearSuppression returns true if the path is suppressed (within the
// time window). Unlike the previous one-shot approach, the entry is kept
// until the expiry time passes so that all fsnotify events generated by a
// single write (typically 2+) are suppressed.
//
// Also performs periodic cleanup of expired entries to prevent map growth.
func (w *Watcher) checkAndClearSuppression(absPath string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()

	// Clean up all expired suppression entries to prevent unbounded growth.
	for path, expiry := range w.suppressed {
		if now.After(expiry) {
			delete(w.suppressed, path)
		}
	}

	expiry, ok := w.suppressed[absPath]
	if !ok {
		return false
	}

	if now.Before(expiry) {
		// Still within the suppression window; keep the entry.
		return true
	}

	// Suppression has expired (cleaned above, but handle race).
	return false
}

// resolveUser finds the user ID and relative path for a given absolute path.
func (w *Watcher) resolveUser(absPath string) (userID, relPath string, ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for uid, dir := range w.userDirs {
		if strings.HasPrefix(absPath, dir+string(filepath.Separator)) {
			rel, err := filepath.Rel(dir, absPath)
			if err != nil {
				continue
			}
			return uid, rel, true
		}
	}
	return "", "", false
}

// debounceEvent resets or creates a timer for the given file path. When the
// timer fires (after the debounce duration with no new events), the handler
// is called. A generation counter prevents stale timer callbacks from firing
// when Stop() returns false because the timer already expired.
func (w *Watcher) debounceEvent(ctx context.Context, userID, relPath, absPath string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if t, exists := w.pending[absPath]; exists {
		t.Stop()
	}

	w.debounceGen[absPath]++
	gen := w.debounceGen[absPath]

	w.pending[absPath] = time.AfterFunc(w.debounce, func() {
		w.mu.Lock()
		if w.debounceGen[absPath] != gen {
			// A newer debounce event superseded this one; skip.
			w.mu.Unlock()
			return
		}
		delete(w.pending, absPath)
		delete(w.debounceGen, absPath)
		w.mu.Unlock()

		// Check context before calling the handler. If shutdown occurred
		// during the debounce interval, skip the handler call.
		if ctx.Err() != nil {
			return
		}

		w.fireHandler(ctx, userID, relPath, absPath)
	})
}

// fireHandler calls the handler and logs any error.
func (w *Watcher) fireHandler(ctx context.Context, userID, relPath, absPath string) {
	w.logger.Debug("watcher: firing handler", "user_id", userID, "rel_path", relPath)
	if err := w.handler(ctx, userID, relPath); err != nil {
		w.logger.Error("watcher: handler error",
			"user_id", userID,
			"rel_path", relPath,
			"abs_path", absPath,
			"error", err,
		)
	}
}
