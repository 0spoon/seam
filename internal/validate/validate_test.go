package validate

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPath_Valid(t *testing.T) {
	tests := []string{
		"notes.md",
		"project/notes.md",
		"a/b/c.md",
		"my-project/my-note.md",
	}
	for _, path := range tests {
		require.NoError(t, Path(path), "path %q should be valid", path)
	}
}

func TestPath_Traversal(t *testing.T) {
	tests := []struct {
		path string
		desc string
	}{
		{"../../etc/passwd", "dot-dot traversal"},
		{"notes/../../../secret.md", "dot-dot in middle"},
		{"..", "bare dot-dot"},
		{"notes/../../etc", "partial traversal"},
	}
	for _, tt := range tests {
		err := Path(tt.path)
		require.Error(t, err, "path %q should be rejected (%s)", tt.path, tt.desc)
		require.True(t, errors.Is(err, ErrPathTraversal), "should be ErrPathTraversal")
	}
}

func TestPath_Absolute(t *testing.T) {
	err := Path("/etc/passwd")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrPathTraversal))
}

func TestPath_NullByte(t *testing.T) {
	err := Path("notes\x00.md")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrPathTraversal))
}

func TestPath_Empty(t *testing.T) {
	err := Path("")
	require.Error(t, err)
}

func TestPathWithinDir(t *testing.T) {
	require.NoError(t, PathWithinDir("notes.md", "/data/users/alice"))
	require.NoError(t, PathWithinDir("project/notes.md", "/data/users/alice"))

	// Traversal should fail.
	err := PathWithinDir("../../etc/passwd", "/data/users/alice")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrPathTraversal))
}

func TestName_Valid(t *testing.T) {
	tests := []string{
		"My Note Title",
		"project-name",
		"Hello World 123",
		"Notes & Ideas",
		"A",
	}
	for _, name := range tests {
		require.NoError(t, Name(name), "name %q should be valid", name)
	}
}

func TestName_Invalid(t *testing.T) {
	tests := []struct {
		name string
		desc string
	}{
		{"path/to/danger", "forward slash"},
		{"path\\to\\danger", "backslash"},
		{"sneaky..title", "dot-dot sequence"},
		{"null\x00byte", "null byte"},
		{"", "empty"},
		{strings.Repeat("a", 256), "too long"},
	}
	for _, tt := range tests {
		err := Name(tt.name)
		require.Error(t, err, "name %q should be rejected (%s)", tt.name, tt.desc)
	}
}

func TestUserID_Valid(t *testing.T) {
	tests := []string{
		"alice",
		"user-123",
		"User_Name",
		"01HZXYZ",
	}
	for _, id := range tests {
		require.NoError(t, UserID(id), "userID %q should be valid", id)
	}
}

func TestUserID_Invalid(t *testing.T) {
	tests := []struct {
		id   string
		desc string
	}{
		{"../../etc", "traversal"},
		{"user/admin", "slash"},
		{"user name", "space"},
		{"", "empty"},
		{strings.Repeat("a", 129), "too long"},
		{"user@domain", "at sign"},
	}
	for _, tt := range tests {
		err := UserID(tt.id)
		require.Error(t, err, "userID %q should be rejected (%s)", tt.id, tt.desc)
		require.True(t, errors.Is(err, ErrInvalidUserID))
	}
}
