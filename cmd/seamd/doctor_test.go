package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/config"
)

func TestCheckConfigParses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeTempConfig(t, dir)

	cfg, res := checkConfigParses(cfgPath)
	require.NotNil(t, cfg)
	require.Equal(t, doctorOK, res.level)
	require.Contains(t, res.message, "mcp.api_key set")
}

func TestCheckConfigParses_MissingFile(t *testing.T) {
	t.Parallel()

	cfg, res := checkConfigParses(filepath.Join(t.TempDir(), "no-config.yaml"))
	// LoadForTools tolerates missing files; mcp.api_key will be empty so
	// the check should report the empty-key error.
	require.NotNil(t, cfg)
	require.Equal(t, doctorError, res.level)
	require.Contains(t, res.message, "api_key is empty")
}

func TestCheckSeamdReachable_Up(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	cfg := mustConfigForListen(t, srv.URL)
	res := checkSeamdReachable(cfg)
	require.Equal(t, doctorOK, res.level)
}

func TestCheckSeamdReachable_Down(t *testing.T) {
	t.Parallel()

	cfg := mustConfigForListen(t, "http://127.0.0.1:1") // unreachable port
	res := checkSeamdReachable(cfg)
	require.Equal(t, doctorWarn, res.level)
	require.Contains(t, res.message, "not reachable")
}

func TestCheckClaudeMCPRegistration_Missing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	res := checkClaudeMCPRegistration(nil, filepath.Join(dir, "missing.json"))
	require.Equal(t, doctorWarn, res.level)
	require.Contains(t, res.message, "Claude Code has never been launched")
}

func TestCheckClaudeMCPRegistration_NoMcpServers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o600))
	res := checkClaudeMCPRegistration(nil, path)
	require.Equal(t, doctorWarn, res.level)
	require.Contains(t, res.message, "no mcpServers")
}

func TestCheckClaudeMCPRegistration_NoSeamEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"mcpServers":{"other":{"url":"x"}}}`), 0o600))
	res := checkClaudeMCPRegistration(nil, path)
	require.Equal(t, doctorWarn, res.level)
	require.Contains(t, res.message, "no `seam` MCP entry")
}

func TestCheckClaudeMCPRegistration_OK(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	body, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"seam": map[string]any{"url": "http://127.0.0.1:8080/api/mcp"},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, body, 0o600))

	cfg := mustConfigForListen(t, "http://127.0.0.1:8080")
	res := checkClaudeMCPRegistration(cfg, path)
	require.Equal(t, doctorOK, res.level)
}

func TestCheckClaudeMCPRegistration_URLMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	body, _ := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"seam": map[string]any{"url": "http://127.0.0.1:9999/api/mcp"},
		},
	})
	require.NoError(t, os.WriteFile(path, body, 0o600))

	cfg := mustConfigForListen(t, "http://127.0.0.1:8080")
	res := checkClaudeMCPRegistration(cfg, path)
	require.Equal(t, doctorWarn, res.level)
	require.Contains(t, res.message, "9999")
}

func TestCheckSessionStartHookInstalled_Missing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	res := checkSessionStartHookInstalled(nil, filepath.Join(dir, "settings.json"))
	require.Equal(t, doctorWarn, res.level)
	require.Contains(t, res.message, "install-claude-hooks")
}

func TestCheckSessionStartHookInstalled_OK(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	require.NoError(t, os.WriteFile(settingsPath, []byte(`{}`), 0o600))
	cfgPath := writeTempConfig(t, dir)

	require.NoError(t, runInstallHooks([]string{"-config", cfgPath, "-settings", settingsPath}))

	cfg := mustConfigForListen(t, "http://127.0.0.1:8080")
	cfg.MCP.APIKey = "test-mcp-key"
	res := checkSessionStartHookInstalled(cfg, settingsPath)
	require.Equal(t, doctorOK, res.level, res.message)
}

func TestCheckSessionStartHookInstalled_URLMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	require.NoError(t, os.WriteFile(settingsPath, []byte(`{}`), 0o600))
	cfgPath := writeTempConfig(t, dir)
	require.NoError(t, runInstallHooks([]string{"-config", cfgPath, "-settings", settingsPath}))

	cfg := mustConfigForListen(t, "http://127.0.0.1:9999")
	cfg.MCP.APIKey = "test-mcp-key"
	res := checkSessionStartHookInstalled(cfg, settingsPath)
	require.Equal(t, doctorWarn, res.level)
	require.Contains(t, res.message, "9999")
}

func TestCheckHookEndpointReachable_OK(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"continue":true,"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"<seam-briefing>x</seam-briefing>"}}`))
	}))
	t.Cleanup(srv.Close)

	cfg := mustConfigForListen(t, srv.URL)
	cfg.MCP.APIKey = "test-key"
	res := checkHookEndpointReachable(cfg)
	require.Equal(t, doctorOK, res.level, res.message)
	require.Contains(t, res.message, "valid briefing")
}

func TestCheckHookEndpointReachable_WrongShape(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	cfg := mustConfigForListen(t, srv.URL)
	cfg.MCP.APIKey = "test-key"
	res := checkHookEndpointReachable(cfg)
	require.Equal(t, doctorError, res.level)
}

func TestCheckHookEndpointReachable_Unauthorized(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	cfg := mustConfigForListen(t, srv.URL)
	cfg.MCP.APIKey = "test-key"
	res := checkHookEndpointReachable(cfg)
	require.Equal(t, doctorError, res.level)
	require.Contains(t, res.message, "401")
}

// mustConfigForListen builds a Config whose Listen matches an httptest URL,
// stripping the http:// prefix.
func mustConfigForListen(t *testing.T, fullURL string) *config.Config {
	t.Helper()
	u, err := url.Parse(fullURL)
	require.NoError(t, err)
	return &config.Config{Listen: u.Host}
}
