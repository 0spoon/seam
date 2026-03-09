package watcher

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIgnoreNext_SuppressesPath(t *testing.T) {
	handler := func(_ context.Context, _, _ string) error { return nil }
	w, err := NewWatcher(handler, 50*time.Millisecond, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	require.NoError(t, err)
	defer w.Close()

	absPath := "/tmp/fake/notes/test.md"
	w.IgnoreNext(absPath)

	w.mu.Lock()
	_, exists := w.suppressed[absPath]
	w.mu.Unlock()
	require.True(t, exists, "path should be in suppressed map after IgnoreNext")

	// First check should consume the suppression.
	suppressed := w.checkAndClearSuppression(absPath)
	require.True(t, suppressed, "first check should return true (suppressed)")

	// Second check should return false (one-shot).
	suppressed = w.checkAndClearSuppression(absPath)
	require.False(t, suppressed, "second check should return false (consumed)")
}

func TestIgnoreNext_ExpiresAfterTTL(t *testing.T) {
	handler := func(_ context.Context, _, _ string) error { return nil }
	w, err := NewWatcher(handler, 50*time.Millisecond, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	require.NoError(t, err)
	defer w.Close()

	absPath := "/tmp/fake/notes/expired.md"

	// Manually insert an already-expired suppression.
	w.mu.Lock()
	w.suppressed[absPath] = time.Now().Add(-1 * time.Second)
	w.mu.Unlock()

	suppressed := w.checkAndClearSuppression(absPath)
	require.False(t, suppressed, "expired suppression should not suppress")
}

func TestWatcher_FileCreate_TriggersHandler(t *testing.T) {
	dir := t.TempDir()

	called := make(chan struct {
		userID  string
		relPath string
	}, 1)

	handler := func(_ context.Context, userID, relPath string) error {
		called <- struct {
			userID  string
			relPath string
		}{userID, relPath}
		return nil
	}

	w, err := NewWatcher(handler, 50*time.Millisecond, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	require.NoError(t, err)
	defer w.Close()

	err = w.Watch("user1", dir)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Run(ctx)
	}()

	// Create a .md file in the watched directory.
	filePath := filepath.Join(dir, "hello.md")
	err = os.WriteFile(filePath, []byte("# Hello"), 0644)
	require.NoError(t, err)

	select {
	case result := <-called:
		require.Equal(t, "user1", result.userID)
		require.Equal(t, "hello.md", result.relPath)
	case <-time.After(3 * time.Second):
		t.Fatal("handler was not called within timeout")
	}
}

func TestWatcher_NonMdFile_Ignored(t *testing.T) {
	dir := t.TempDir()

	called := make(chan string, 1)

	handler := func(_ context.Context, _, relPath string) error {
		called <- relPath
		return nil
	}

	w, err := NewWatcher(handler, 50*time.Millisecond, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	require.NoError(t, err)
	defer w.Close()

	err = w.Watch("user1", dir)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Run(ctx)
	}()

	// Create a non-.md file; should be ignored.
	err = os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("text"), 0644)
	require.NoError(t, err)

	// Then create a .md file to prove the watcher is working.
	err = os.WriteFile(filepath.Join(dir, "note.md"), []byte("# Note"), 0644)
	require.NoError(t, err)

	select {
	case relPath := <-called:
		require.Equal(t, "note.md", relPath, "only .md files should trigger the handler")
	case <-time.After(3 * time.Second):
		t.Fatal("handler was not called within timeout")
	}
}

func TestWatcher_Subdirectory_TriggersHandler(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	called := make(chan string, 1)

	handler := func(_ context.Context, _, relPath string) error {
		called <- relPath
		return nil
	}

	w, err := NewWatcher(handler, 50*time.Millisecond, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	require.NoError(t, err)
	defer w.Close()

	err = w.Watch("user1", dir)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Run(ctx)
	}()

	// Create a .md file in a subdirectory.
	filePath := filepath.Join(subDir, "deep.md")
	err = os.WriteFile(filePath, []byte("# Deep"), 0644)
	require.NoError(t, err)

	select {
	case relPath := <-called:
		require.Equal(t, filepath.Join("subdir", "deep.md"), relPath)
	case <-time.After(3 * time.Second):
		t.Fatal("handler was not called within timeout")
	}
}

func TestWatcher_IgnoreNext_SuppressesFileEvent(t *testing.T) {
	dir := t.TempDir()

	callCount := make(chan string, 10)

	handler := func(_ context.Context, _, relPath string) error {
		callCount <- relPath
		return nil
	}

	w, err := NewWatcher(handler, 50*time.Millisecond, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	require.NoError(t, err)
	defer w.Close()

	err = w.Watch("user1", dir)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Run(ctx)
	}()

	// Suppress the next event for this path, then create the file.
	filePath := filepath.Join(dir, "suppressed.md")
	w.IgnoreNext(filePath)

	err = os.WriteFile(filePath, []byte("# Suppressed"), 0644)
	require.NoError(t, err)

	// Write a second file to verify the watcher is still running.
	secondPath := filepath.Join(dir, "second.md")
	err = os.WriteFile(secondPath, []byte("# Second"), 0644)
	require.NoError(t, err)

	select {
	case relPath := <-callCount:
		require.Equal(t, "second.md", relPath, "suppressed file should not trigger; second file should")
	case <-time.After(3 * time.Second):
		t.Fatal("handler was not called within timeout")
	}
}

func TestNewWatcher_NilHandler_ReturnsError(t *testing.T) {
	_, err := NewWatcher(nil, 50*time.Millisecond, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "handler must not be nil")
}
