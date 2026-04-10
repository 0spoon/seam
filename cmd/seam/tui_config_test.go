package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// withTempHome redirects HOME (and XDG_CONFIG_HOME, where applicable)
// to a temp directory so the test cannot stomp on the developer's real
// ~/.config/seam/tui.yaml. The temp dir is automatically removed.
func withTempHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// authConfigDir builds the path from os.UserHomeDir() which uses
	// HOME on Unix; the temp HOME is enough.
	return tmp
}

func TestLoadTUIConfig_MissingReturnsDefaults(t *testing.T) {
	withTempHome(t)
	cfg, err := LoadTUIConfig()
	require.NoError(t, err)
	require.Equal(t, DefaultTUIConfig(), cfg)
}

func TestSaveLoadTUIConfig_RoundTrip(t *testing.T) {
	tmp := withTempHome(t)

	want := TUIConfig{
		Theme:          "catppuccin-latte",
		AssistantTheme: "follow_global",
	}
	require.NoError(t, SaveTUIConfig(want))

	// Confirm the file landed in the expected location with the right
	// permissions.
	p := filepath.Join(tmp, ".config", "seam", "tui.yaml")
	info, err := os.Stat(p)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "file mode should be 0o600")

	dir := filepath.Join(tmp, ".config", "seam")
	dirInfo, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm(), "dir mode should be 0o700")

	got, err := LoadTUIConfig()
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestLoadTUIConfig_UnknownThemeFallsBack(t *testing.T) {
	tmp := withTempHome(t)
	dir := filepath.Join(tmp, ".config", "seam")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	yamlBytes := []byte("theme: catppuccin-bogus\nassistant_theme: \"\"\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tui.yaml"), yamlBytes, 0o600))

	cfg, err := LoadTUIConfig()
	require.NoError(t, err)
	require.Equal(t, DefaultTUIConfig().Theme, cfg.Theme,
		"unknown theme should fall back to default")
}

func TestLoadTUIConfig_MalformedDoesNotFail(t *testing.T) {
	tmp := withTempHome(t)
	dir := filepath.Join(tmp, ".config", "seam")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tui.yaml"), []byte(":::not yaml::"), 0o600))

	cfg, err := LoadTUIConfig()
	require.NoError(t, err, "malformed YAML should not fail TUI startup")
	require.Equal(t, DefaultTUIConfig(), cfg)
}

func TestResolveTUIConfig_Precedence(t *testing.T) {
	tmp := withTempHome(t)
	dir := filepath.Join(tmp, ".config", "seam")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	// File says Frappe.
	yamlBytes := []byte("theme: catppuccin-frappe\nassistant_theme: mario\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tui.yaml"), yamlBytes, 0o600))

	t.Run("file only", func(t *testing.T) {
		t.Setenv("SEAM_THEME", "")
		t.Setenv("SEAM_ASSISTANT_THEME", "")
		cfg := ResolveTUIConfig("", "")
		require.Equal(t, "catppuccin-frappe", cfg.Theme)
		require.Equal(t, "mario", cfg.AssistantTheme)
	})

	t.Run("env overrides file", func(t *testing.T) {
		t.Setenv("SEAM_THEME", "catppuccin-mocha")
		t.Setenv("SEAM_ASSISTANT_THEME", "follow_global")
		cfg := ResolveTUIConfig("", "")
		require.Equal(t, "catppuccin-mocha", cfg.Theme)
		require.Equal(t, "follow_global", cfg.AssistantTheme)
	})

	t.Run("flag overrides env", func(t *testing.T) {
		t.Setenv("SEAM_THEME", "catppuccin-mocha")
		t.Setenv("SEAM_ASSISTANT_THEME", "follow_global")
		cfg := ResolveTUIConfig("catppuccin-latte", "mario")
		require.Equal(t, "catppuccin-latte", cfg.Theme)
		require.Equal(t, "mario", cfg.AssistantTheme)
	})

	t.Run("invalid env value ignored", func(t *testing.T) {
		t.Setenv("SEAM_THEME", "garbage")
		t.Setenv("SEAM_ASSISTANT_THEME", "")
		cfg := ResolveTUIConfig("", "")
		// File still wins because env was invalid.
		require.Equal(t, "catppuccin-frappe", cfg.Theme)
	})

	t.Run("invalid flag value ignored", func(t *testing.T) {
		t.Setenv("SEAM_THEME", "")
		t.Setenv("SEAM_ASSISTANT_THEME", "")
		cfg := ResolveTUIConfig("garbage", "")
		require.Equal(t, "catppuccin-frappe", cfg.Theme)
	})
}

func TestSaveTUIConfig_AtomicReplacesExisting(t *testing.T) {
	withTempHome(t)
	require.NoError(t, SaveTUIConfig(TUIConfig{Theme: "seam", AssistantTheme: "mario"}))
	require.NoError(t, SaveTUIConfig(TUIConfig{Theme: "catppuccin-latte", AssistantTheme: "follow_global"}))
	got, err := LoadTUIConfig()
	require.NoError(t, err)
	require.Equal(t, "catppuccin-latte", got.Theme)
	require.Equal(t, "follow_global", got.AssistantTheme)
}
