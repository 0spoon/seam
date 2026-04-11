package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// TUIConfig holds TUI-only persistent preferences. It lives next to
// auth.json under ~/.config/seam/, separate from the server's
// seam-server.yaml. Only the TUI binary reads or writes this file.
type TUIConfig struct {
	// Theme is the slug of the active global theme. Must be a key in
	// themeRegistry; unknown values fall back to defaults at load time.
	Theme string `yaml:"theme"`
	// AssistantTheme controls the assistant screen palette. Either
	// "mario" (the historical default) or AssistantInheritName
	// ("follow_global") to use the active global theme.
	AssistantTheme string `yaml:"assistant_theme"`
	// Keybindings maps action IDs (e.g. "editor.save") to a list of key
	// strings that should trigger the action. Any action omitted keeps
	// its built-in default. An empty list unbinds the action entirely.
	// Validation happens in LoadKeymap: unknown actions and invalid
	// keys are warned to stderr but never block TUI startup.
	Keybindings map[string][]string `yaml:"keybindings,omitempty"`
}

// DefaultTUIConfig returns the built-in default config. Used when no
// file exists and no flags or env vars override anything.
func DefaultTUIConfig() TUIConfig {
	return TUIConfig{
		Theme:          themeCatppuccinMocha.Name,
		AssistantTheme: themeMario.Name,
	}
}

// tuiConfigPath returns the absolute path to ~/.config/seam/tui.yaml.
func tuiConfigPath() (string, error) {
	dir, err := authConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tui.yaml"), nil
}

// LoadTUIConfig reads the TUI config from disk. If the file does not
// exist, returns DefaultTUIConfig() with no error -- a missing file is
// the expected first-run state.
//
// Malformed YAML or unknown theme names produce a warning to stderr and
// fall back to defaults rather than failing TUI startup.
func LoadTUIConfig() (TUIConfig, error) {
	cfg := DefaultTUIConfig()
	p, err := tuiConfigPath()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read tui config: %w", err)
	}
	var loaded TUIConfig
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		fmt.Fprintf(os.Stderr, "seam: invalid tui.yaml, using defaults: %v\n", err)
		return cfg, nil
	}
	if loaded.Theme != "" {
		if _, ok := ResolveTheme(loaded.Theme); ok {
			cfg.Theme = loaded.Theme
		} else {
			fmt.Fprintf(os.Stderr, "seam: unknown theme %q in tui.yaml, using default\n", loaded.Theme)
		}
	}
	if loaded.AssistantTheme != "" {
		if validAssistantTheme(loaded.AssistantTheme) {
			cfg.AssistantTheme = loaded.AssistantTheme
		} else {
			fmt.Fprintf(os.Stderr, "seam: unknown assistant_theme %q in tui.yaml, using default\n", loaded.AssistantTheme)
		}
	}
	// Keybindings passes through unchanged; LoadKeymap does per-entry
	// validation and emits warnings there so messages reference action
	// IDs instead of top-level TUIConfig fields.
	cfg.Keybindings = loaded.Keybindings
	return cfg, nil
}

// SaveTUIConfig writes the TUI config to disk atomically (tmp + rename)
// so a crash mid-write cannot corrupt the file. The parent directory is
// created with 0o700 and the file with 0o600 to match auth.json.
func SaveTUIConfig(cfg TUIConfig) error {
	dir, err := authConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal tui config: %w", err)
	}

	finalPath := filepath.Join(dir, "tui.yaml")
	tmpFile, err := os.CreateTemp(dir, "tui.yaml.tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	// Best-effort cleanup if rename fails.
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		tmpFile.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// ResolveTUIConfig applies the precedence chain: CLI flag > env var >
// config file > default. Empty strings for the flag arguments mean
// "flag not set"; the caller is expected to pass through the result of
// flag.String dereference unchanged.
//
// Unknown values at any level are silently dropped (with a stderr warn)
// so the TUI never refuses to start over a typo.
func ResolveTUIConfig(flagTheme, flagAssistant string) TUIConfig {
	cfg, _ := LoadTUIConfig()

	// Env vars override the file.
	if v := strings.TrimSpace(os.Getenv("SEAM_THEME")); v != "" {
		if _, ok := ResolveTheme(v); ok {
			cfg.Theme = v
		} else {
			fmt.Fprintf(os.Stderr, "seam: unknown SEAM_THEME=%q, ignoring\n", v)
		}
	}
	if v := strings.TrimSpace(os.Getenv("SEAM_ASSISTANT_THEME")); v != "" {
		if validAssistantTheme(v) {
			cfg.AssistantTheme = v
		} else {
			fmt.Fprintf(os.Stderr, "seam: unknown SEAM_ASSISTANT_THEME=%q, ignoring\n", v)
		}
	}

	// CLI flags override env.
	if v := strings.TrimSpace(flagTheme); v != "" {
		if _, ok := ResolveTheme(v); ok {
			cfg.Theme = v
		} else {
			fmt.Fprintf(os.Stderr, "seam: unknown --theme=%q, ignoring\n", v)
		}
	}
	if v := strings.TrimSpace(flagAssistant); v != "" {
		if validAssistantTheme(v) {
			cfg.AssistantTheme = v
		} else {
			fmt.Fprintf(os.Stderr, "seam: unknown --assistant-theme=%q, ignoring\n", v)
		}
	}

	return cfg
}

// validAssistantTheme returns true if the value is a recognized
// assistant theme mode (either "mario" or AssistantInheritName).
func validAssistantTheme(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case themeMario.Name, AssistantInheritName:
		return true
	}
	return false
}
