package watcher

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReconcile_WalksAndCallsHandler(t *testing.T) {
	dir := t.TempDir()

	// Create a nested structure with .md files.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "b.md"), []byte("# B"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "c.md"), []byte("# C"), 0644))

	var collected []string
	handler := func(_ context.Context, userID, relPath string) error {
		require.Equal(t, "user1", userID)
		collected = append(collected, relPath)
		return nil
	}

	err := Reconcile(context.Background(), "user1", dir, handler, nil)
	require.NoError(t, err)

	sort.Strings(collected)
	expected := []string{
		"a.md",
		filepath.Join("sub", "b.md"),
		filepath.Join("sub", "c.md"),
	}
	sort.Strings(expected)
	require.Equal(t, expected, collected)
}

func TestReconcile_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	callCount := 0
	handler := func(_ context.Context, _, _ string) error {
		callCount++
		return nil
	}

	err := Reconcile(context.Background(), "user1", dir, handler, nil)
	require.NoError(t, err)
	require.Equal(t, 0, callCount, "handler should not be called for empty directory")
}

func TestReconcile_SkipsNonMdFiles(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Notes"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("text"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "image.png"), []byte("fake"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "data.json"), []byte("{}"), 0644))

	var collected []string
	handler := func(_ context.Context, _, relPath string) error {
		collected = append(collected, relPath)
		return nil
	}

	err := Reconcile(context.Background(), "user1", dir, handler, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"notes.md"}, collected, "only .md files should be processed")
}

func TestReconcile_RespectsContextCancellation(t *testing.T) {
	dir := t.TempDir()

	// Create several files so we have a chance to cancel mid-walk.
	for i := 0; i < 10; i++ {
		name := filepath.Join(dir, string(rune('a'+i))+".md")
		require.NoError(t, os.WriteFile(name, []byte("# Note"), 0644))
	}

	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0
	handler := func(_ context.Context, _, _ string) error {
		callCount++
		if callCount >= 2 {
			cancel()
		}
		return nil
	}

	err := Reconcile(ctx, "user1", dir, handler, nil)
	require.Error(t, err, "should return error on context cancellation")
}

func TestReconcile_NotADirectory_ReturnsError(t *testing.T) {
	f := filepath.Join(t.TempDir(), "not-a-dir.txt")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0644))

	handler := func(_ context.Context, _, _ string) error { return nil }
	err := Reconcile(context.Background(), "user1", f, handler, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a directory")
}
