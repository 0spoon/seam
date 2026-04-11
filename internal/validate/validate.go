// Package validate provides shared input validation helpers for security.
// These enforce the AGENTS.md security invariants:
// - Reject any file path containing "..", absolute paths, or null bytes.
// - Sanitize note titles, project names, tags for filesystem safety.
package validate

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// ErrPathTraversal is returned when a path contains "..", is absolute,
	// or contains null bytes.
	ErrPathTraversal = fmt.Errorf("path contains traversal sequence, absolute path, or null bytes")

	// ErrUnsafeName is returned when a name contains filesystem-unsafe
	// characters or patterns.
	ErrUnsafeName = fmt.Errorf("name contains unsafe characters")

	// ErrInvalidUserID is returned when a user ID contains characters outside
	// the allowed set.
	ErrInvalidUserID = fmt.Errorf("invalid user ID format")

	// userIDPattern allows alphanumeric, hyphens, and underscores only.
	userIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// Path rejects file paths that could escape a base directory.
// It checks for ".." components, absolute paths, and null bytes.
func Path(path string) error {
	if path == "" {
		return fmt.Errorf("validate.Path: empty path")
	}
	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("validate.Path: %w: null byte", ErrPathTraversal)
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("validate.Path: %w: absolute path", ErrPathTraversal)
	}

	// Check each component for "..".
	cleaned := filepath.Clean(path)
	for _, part := range strings.Split(cleaned, string(filepath.Separator)) {
		if part == ".." {
			return fmt.Errorf("validate.Path: %w: dot-dot component", ErrPathTraversal)
		}
	}
	return nil
}

// PathWithinDir validates a path and then verifies the resolved absolute path
// stays within the given base directory.
func PathWithinDir(relPath, baseDir string) error {
	if err := Path(relPath); err != nil {
		return err
	}
	absPath := filepath.Join(baseDir, relPath)
	absPath = filepath.Clean(absPath)
	baseDir = filepath.Clean(baseDir)

	if !strings.HasPrefix(absPath, baseDir+string(filepath.Separator)) && absPath != baseDir {
		return fmt.Errorf("validate.PathWithinDir: %w: resolved path escapes base directory", ErrPathTraversal)
	}
	return nil
}

// Title validates note titles. More permissive than Name: allows forward
// slashes (common in titles like "TCP/IP", "A/B Testing") since titles are
// never used directly as filenames — they are slugified first.
// Rejects null bytes, backslashes, ".." sequences, and length > 255.
func Title(title string) error {
	if title == "" {
		return fmt.Errorf("validate.Title: name is empty")
	}
	if len(title) > 255 {
		return fmt.Errorf("validate.Title: %w: exceeds 255 characters", ErrUnsafeName)
	}
	if strings.ContainsRune(title, 0) {
		return fmt.Errorf("validate.Title: %w: null byte", ErrUnsafeName)
	}
	if strings.Contains(title, "\\") {
		return fmt.Errorf("validate.Title: %w: backslash", ErrUnsafeName)
	}
	if strings.Contains(title, "..") {
		return fmt.Errorf("validate.Title: %w: dot-dot sequence", ErrUnsafeName)
	}
	return nil
}

// Name rejects names (titles, project names) that contain filesystem-unsafe
// characters: null bytes, forward/back slashes, or ".." sequences.
// Also enforces a maximum length of 255 characters.
func Name(name string) error {
	if name == "" {
		return fmt.Errorf("validate.Name: name is empty")
	}
	if len(name) > 255 {
		return fmt.Errorf("validate.Name: %w: exceeds 255 characters", ErrUnsafeName)
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("validate.Name: %w: null byte", ErrUnsafeName)
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf("validate.Name: %w: forward slash", ErrUnsafeName)
	}
	if strings.Contains(name, "\\") {
		return fmt.Errorf("validate.Name: %w: backslash", ErrUnsafeName)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("validate.Name: %w: dot-dot sequence", ErrUnsafeName)
	}
	return nil
}

// UserID rejects user IDs that contain anything other than alphanumeric
// characters, hyphens, or underscores. Max length 128.
func UserID(id string) error {
	if id == "" {
		return fmt.Errorf("validate.UserID: %w: empty", ErrInvalidUserID)
	}
	if len(id) > 128 {
		return fmt.Errorf("validate.UserID: %w: exceeds 128 characters", ErrInvalidUserID)
	}
	if !userIDPattern.MatchString(id) {
		return fmt.Errorf("validate.UserID: %w: contains invalid characters", ErrInvalidUserID)
	}
	return nil
}
