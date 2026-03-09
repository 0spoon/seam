package template

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type mockManager struct {
	dataDir string
}

func (m *mockManager) Open(ctx context.Context, userID string) (*sql.DB, error) { return nil, nil }
func (m *mockManager) Close(userID string) error                                { return nil }
func (m *mockManager) CloseAll() error                                          { return nil }
func (m *mockManager) UserNotesDir(userID string) string {
	return filepath.Join(m.dataDir, "users", userID, "notes")
}
func (m *mockManager) UserDataDir(userID string) string {
	return filepath.Join(m.dataDir, "users", userID)
}
func (m *mockManager) ListUsers(ctx context.Context) ([]string, error) { return nil, nil }
func (m *mockManager) EnsureUserDirs(userID string) error              { return nil }

func TestEnsureDefaults(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{dataDir: dir}
	svc := NewService(dir, mgr, nil)

	err := svc.EnsureDefaults()
	require.NoError(t, err)

	// Check that default templates were created.
	for name := range defaultTemplates {
		path := filepath.Join(dir, "templates", name+".md")
		_, err := os.Stat(path)
		require.NoError(t, err, "expected default template %s to exist", name)
	}
}

func TestEnsureDefaults_DoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{dataDir: dir}
	svc := NewService(dir, mgr, nil)

	// Create shared templates dir and write a custom version.
	tmplDir := filepath.Join(dir, "templates")
	require.NoError(t, os.MkdirAll(tmplDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmplDir, "meeting-notes.md"), []byte("custom content"), 0o644))

	err := svc.EnsureDefaults()
	require.NoError(t, err)

	// Verify custom content was not overwritten.
	data, err := os.ReadFile(filepath.Join(tmplDir, "meeting-notes.md"))
	require.NoError(t, err)
	require.Equal(t, "custom content", string(data))
}

func TestList_SharedTemplates(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{dataDir: dir}
	svc := NewService(dir, mgr, nil)

	require.NoError(t, svc.EnsureDefaults())

	templates, err := svc.List(context.Background(), "user1")
	require.NoError(t, err)
	require.Len(t, templates, len(defaultTemplates))

	// Verify all default templates are listed.
	names := make(map[string]bool)
	for _, t := range templates {
		names[t.Name] = true
	}
	for name := range defaultTemplates {
		require.True(t, names[name], "expected template %s in list", name)
	}
}

func TestList_UserOverrides(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{dataDir: dir}
	svc := NewService(dir, mgr, nil)

	require.NoError(t, svc.EnsureDefaults())

	// Create a per-user template that overrides a shared one.
	userTmplDir := filepath.Join(dir, "users", "user1", "templates")
	require.NoError(t, os.MkdirAll(userTmplDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(userTmplDir, "meeting-notes.md"), []byte("user's version"), 0o644))

	// Also create a unique per-user template.
	require.NoError(t, os.WriteFile(filepath.Join(userTmplDir, "custom-template.md"), []byte("custom body"), 0o644))

	templates, err := svc.List(context.Background(), "user1")
	require.NoError(t, err)
	// Should have shared defaults + 1 new custom template (override replaces, not adds).
	require.Equal(t, len(defaultTemplates)+1, len(templates))
}

func TestGet_SharedTemplate(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{dataDir: dir}
	svc := NewService(dir, mgr, nil)

	require.NoError(t, svc.EnsureDefaults())

	tmpl, err := svc.Get(context.Background(), "user1", "daily-log")
	require.NoError(t, err)
	require.Equal(t, "daily-log", tmpl.Name)
	require.Contains(t, tmpl.Body, "Daily Log")
}

func TestGet_UserOverride(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{dataDir: dir}
	svc := NewService(dir, mgr, nil)

	require.NoError(t, svc.EnsureDefaults())

	// Create per-user override.
	userTmplDir := filepath.Join(dir, "users", "user1", "templates")
	require.NoError(t, os.MkdirAll(userTmplDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(userTmplDir, "daily-log.md"), []byte("My custom daily log"), 0o644))

	tmpl, err := svc.Get(context.Background(), "user1", "daily-log")
	require.NoError(t, err)
	require.Equal(t, "My custom daily log", tmpl.Body)
}

func TestGet_NotFound(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{dataDir: dir}
	svc := NewService(dir, mgr, nil)

	_, err := svc.Get(context.Background(), "user1", "nonexistent")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTemplateNotFound)
}

func TestGet_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{dataDir: dir}
	svc := NewService(dir, mgr, nil)

	_, err := svc.Get(context.Background(), "user1", "../../../etc/passwd")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidName)
}

func TestApply_SubstitutesVars(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{dataDir: dir}
	svc := NewService(dir, mgr, nil)

	require.NoError(t, svc.EnsureDefaults())

	body, err := svc.Apply(context.Background(), "user1", "project-kickoff", map[string]string{
		"project": "Seam",
	})
	require.NoError(t, err)
	require.Contains(t, body, "Seam - Project Kickoff")
	// Should also substitute built-in {{date}}.
	require.NotContains(t, body, "{{date}}")
}

func TestApply_BuiltinVars(t *testing.T) {
	dir := t.TempDir()
	mgr := &mockManager{dataDir: dir}
	svc := NewService(dir, mgr, nil)

	require.NoError(t, svc.EnsureDefaults())

	body, err := svc.Apply(context.Background(), "user1", "daily-log", nil)
	require.NoError(t, err)
	require.NotContains(t, body, "{{date}}")
}

func TestSubstituteVars(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		vars     map[string]string
		expected string
	}{
		{
			name:     "simple var",
			body:     "Hello {{name}}!",
			vars:     map[string]string{"name": "World"},
			expected: "Hello World!",
		},
		{
			name:     "multiple vars",
			body:     "{{project}} - {{date}}",
			vars:     map[string]string{"project": "Seam"},
			expected: "Seam - " + substituteVars("{{date}}", nil), // date is auto-substituted
		},
		{
			name:     "no vars",
			body:     "No vars here",
			vars:     nil,
			expected: "No vars here",
		},
		{
			name:     "user var overrides builtin",
			body:     "Date: {{date}}",
			vars:     map[string]string{"date": "2025-01-01"},
			expected: "Date: 2025-01-01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := substituteVars(tt.body, tt.vars)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		{"valid name", "meeting-notes", true},
		{"valid with underscore", "my_template", true},
		{"empty", "", false},
		{"path traversal", "../secret", false},
		{"slash", "a/b", false},
		{"backslash", "a\\b", false},
		{"null byte", "a\x00b", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateName(tt.input)
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
