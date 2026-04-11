package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComputeHookURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		listen string
		want   string
	}{
		{"", "http://127.0.0.1:8080/api/hooks/session-start"},
		{":8080", "http://127.0.0.1:8080/api/hooks/session-start"},
		{"0.0.0.0:8080", "http://127.0.0.1:8080/api/hooks/session-start"},
		{"127.0.0.1:8080", "http://127.0.0.1:8080/api/hooks/session-start"},
		{"192.168.1.5:9090", "http://192.168.1.5:9090/api/hooks/session-start"},
		{"[::]:8080", "http://127.0.0.1:8080/api/hooks/session-start"},
		{"[::1]:8080", "http://[::1]:8080/api/hooks/session-start"},
	}

	for _, tc := range cases {
		t.Run(tc.listen, func(t *testing.T) {
			got, err := computeHookURL(tc.listen)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestComputeHookURL_Error(t *testing.T) {
	t.Parallel()
	_, err := computeHookURL("not-a-listen")
	require.Error(t, err)
}

// --- mergeSeamHook ---

func parseSettings(t *testing.T, body string) settingsFile {
	t.Helper()
	settings := settingsFile{}
	if body == "" {
		return settings
	}
	require.NoError(t, json.Unmarshal([]byte(body), &settings))
	return settings
}

func TestMergeSeamHook_AppendsToEmptySettings(t *testing.T) {
	t.Parallel()

	updated, action, err := mergeSeamHook(parseSettings(t, "{}"), "http://127.0.0.1:8080/api/hooks/session-start", "k1")
	require.NoError(t, err)
	require.Equal(t, mergeActionAppended, action)

	got := decodeSessionStart(t, updated)
	require.Len(t, got, 1)
	require.True(t, got[0]["seam_managed"].(bool))
	hooks := got[0]["hooks"].([]any)
	require.Len(t, hooks, 1)
	first := hooks[0].(map[string]any)
	require.Equal(t, "http", first["type"])
	require.Equal(t, "http://127.0.0.1:8080/api/hooks/session-start", first["url"])
	headers := first["headers"].(map[string]any)
	require.Equal(t, "Bearer k1", headers["Authorization"])
}

func TestMergeSeamHook_PreservesUserHooks(t *testing.T) {
	t.Parallel()

	body := `{
        "hooks": {
            "SessionStart": [
                {"matcher": "startup", "hooks": [{"type": "command", "command": "echo user"}]}
            ],
            "PreToolUse": [
                {"matcher": "Bash", "hooks": [{"type": "command", "command": "echo bash"}]}
            ]
        }
    }`
	settings := parseSettings(t, body)
	updated, action, err := mergeSeamHook(settings, "http://127.0.0.1:8080/api/hooks/session-start", "k1")
	require.NoError(t, err)
	require.Equal(t, mergeActionAppended, action)

	sessionStart := decodeSessionStart(t, updated)
	require.Len(t, sessionStart, 2, "user-authored entry must survive")
	require.Nil(t, sessionStart[0]["seam_managed"], "user entry must not be marked")
	require.Equal(t, true, sessionStart[1]["seam_managed"], "seam entry appended last")

	// PreToolUse must remain untouched.
	hooksRaw := updated["hooks"]
	var hooksMap map[string]any
	require.NoError(t, json.Unmarshal(hooksRaw, &hooksMap))
	require.Contains(t, hooksMap, "PreToolUse")
}

func TestMergeSeamHook_UpdatesExistingInPlace(t *testing.T) {
	t.Parallel()

	first, _, err := mergeSeamHook(parseSettings(t, "{}"), "http://127.0.0.1:8080/api/hooks/session-start", "old-key")
	require.NoError(t, err)

	// Re-run with a different URL and key — should update in place, not append.
	updated, action, err := mergeSeamHook(first, "http://192.168.1.5:9000/api/hooks/session-start", "new-key")
	require.NoError(t, err)
	require.Equal(t, mergeActionUpdated, action)

	sessionStart := decodeSessionStart(t, updated)
	require.Len(t, sessionStart, 1, "no duplicate entry")

	first0 := sessionStart[0]["hooks"].([]any)[0].(map[string]any)
	require.Equal(t, "http://192.168.1.5:9000/api/hooks/session-start", first0["url"])
	require.Equal(t, "Bearer new-key", first0["headers"].(map[string]any)["Authorization"])
}

func TestMergeSeamHook_IdempotentWithSameInputs(t *testing.T) {
	t.Parallel()

	first, _, err := mergeSeamHook(parseSettings(t, "{}"), "http://127.0.0.1:8080/api/hooks/session-start", "k1")
	require.NoError(t, err)

	updated, action, err := mergeSeamHook(first, "http://127.0.0.1:8080/api/hooks/session-start", "k1")
	require.NoError(t, err)
	require.Equal(t, mergeActionUnchanged, action)
	require.Len(t, decodeSessionStart(t, updated), 1)
}

// --- removeSeamHook ---

func TestRemoveSeamHook_RemovesOnlySeamEntries(t *testing.T) {
	t.Parallel()

	body := `{
        "hooks": {
            "SessionStart": [
                {"matcher": "startup", "hooks": [{"type": "command", "command": "echo user"}]},
                {"matcher": "startup|resume|clear|compact", "seam_managed": true, "hooks": [{"type": "http", "url": "http://localhost:8080/api/hooks/session-start", "headers": {"Authorization": "Bearer k1"}}]}
            ]
        }
    }`
	updated, removed := removeSeamHook(parseSettings(t, body))
	require.Equal(t, 1, removed)

	sessionStart := decodeSessionStart(t, updated)
	require.Len(t, sessionStart, 1)
	require.Nil(t, sessionStart[0]["seam_managed"])
}

func TestRemoveSeamHook_NoEntriesIsNoop(t *testing.T) {
	t.Parallel()
	body := `{"hooks": {"SessionStart": [{"matcher": "startup", "hooks": []}]}}`
	updated, removed := removeSeamHook(parseSettings(t, body))
	require.Equal(t, 0, removed)
	require.Equal(t, parseSettings(t, body), updated)
}

func TestRemoveSeamHook_DropsSessionStartArrayWhenLast(t *testing.T) {
	t.Parallel()

	body := `{
        "hooks": {
            "SessionStart": [
                {"matcher": "startup|resume|clear|compact", "seam_managed": true, "hooks": [{"type": "http", "url": "http://localhost:8080/api/hooks/session-start"}]}
            ]
        }
    }`
	updated, removed := removeSeamHook(parseSettings(t, body))
	require.Equal(t, 1, removed)
	// Both SessionStart and the empty hooks object should be cleaned up.
	require.NotContains(t, updated, "hooks")
}

// --- end-to-end install round-trip ---

func TestInstallHooks_EndToEnd(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	require.NoError(t, os.WriteFile(settingsPath, []byte(`{"existingKey": "preserved"}`), 0o600))

	cfgPath := writeTempConfig(t, dir)

	require.NoError(t, runInstallHooks([]string{
		"-config", cfgPath,
		"-settings", settingsPath,
	}))

	got, err := os.ReadFile(settingsPath)
	require.NoError(t, err)
	require.Contains(t, string(got), `"seam_managed"`)
	require.Contains(t, string(got), `"existingKey"`)
	require.Contains(t, string(got), "Bearer test-mcp-key")

	// Backup must exist.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	hasBackup := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "settings.json.seam-bak-") {
			hasBackup = true
		}
	}
	require.True(t, hasBackup, "backup file must be created on first install")

	// File mode must be preserved.
	info, err := os.Stat(settingsPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestInstallHooks_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	cfgPath := writeTempConfig(t, dir)

	for i := 0; i < 3; i++ {
		require.NoError(t, runInstallHooks([]string{"-config", cfgPath, "-settings", settingsPath}))
	}

	body, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	settings := parseSettings(t, string(body))
	sessionStart := decodeSessionStart(t, settings)
	require.Len(t, sessionStart, 1, "must not duplicate on repeated install")

	// Only one backup file should exist (single backup).
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	backupCount := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "settings.json.seam-bak-") {
			backupCount++
		}
	}
	require.Equal(t, 0, backupCount, "no backup is needed when starting from a non-existent settings.json")
}

func TestInstallHooks_RefusesEmptyAPIKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "seam-server.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`listen: ":8080"
data_dir: ./data
jwt_secret: short
mcp:
  api_key: ""
`), 0o600))

	err := runInstallHooks([]string{"-config", cfgPath, "-settings", filepath.Join(dir, "settings.json")})
	require.Error(t, err)
	require.Contains(t, err.Error(), "mcp.api_key")
}

func TestInstallHooks_MalformedSettingsErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	require.NoError(t, os.WriteFile(settingsPath, []byte(`{not json`), 0o600))
	cfgPath := writeTempConfig(t, dir)

	err := runInstallHooks([]string{"-config", cfgPath, "-settings", settingsPath})
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse")

	// File should be unchanged on error.
	got, err := os.ReadFile(settingsPath)
	require.NoError(t, err)
	require.Equal(t, "{not json", string(got))
}

func TestInstallHooks_FollowsSymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "real_settings.json")
	require.NoError(t, os.WriteFile(target, []byte(`{}`), 0o600))

	link := filepath.Join(dir, "settings_link.json")
	require.NoError(t, os.Symlink(target, link))

	cfgPath := writeTempConfig(t, dir)
	require.NoError(t, runInstallHooks([]string{"-config", cfgPath, "-settings", link}))

	// Real file must contain the seam entry; symlink must still point at it.
	body, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Contains(t, string(body), `"seam_managed"`)

	info, err := os.Lstat(link)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "symlink must remain a symlink")
}

func TestUninstallHooks_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	require.NoError(t, os.WriteFile(settingsPath, []byte(`{"otherKey":"keep"}`), 0o600))
	cfgPath := writeTempConfig(t, dir)

	require.NoError(t, runInstallHooks([]string{"-config", cfgPath, "-settings", settingsPath}))
	require.NoError(t, runUninstallHooks([]string{"-settings", settingsPath}))

	body, err := os.ReadFile(settingsPath)
	require.NoError(t, err)
	require.Contains(t, string(body), `"otherKey"`)
	require.NotContains(t, string(body), `seam_managed`)
}

func TestUninstallHooks_NoFileWarnsButSucceeds(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, runUninstallHooks([]string{"-settings", filepath.Join(dir, "missing.json")}))
}

// --- helpers ---

func decodeSessionStart(t *testing.T, settings settingsFile) []map[string]any {
	t.Helper()
	hooksRaw, ok := settings["hooks"]
	require.True(t, ok, "hooks key missing")
	var hooksMap map[string]any
	require.NoError(t, json.Unmarshal(hooksRaw, &hooksMap))
	raw, ok := hooksMap["SessionStart"]
	require.True(t, ok, "SessionStart key missing")
	arr, ok := raw.([]any)
	require.True(t, ok, "SessionStart must be an array")
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		out = append(out, e.(map[string]any))
	}
	return out
}

func writeTempConfig(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "seam-server.yaml")
	require.NoError(t, os.WriteFile(p, []byte(`listen: ":8080"
data_dir: ./data
jwt_secret: this-is-thirty-two-characters!!!
mcp:
  api_key: test-mcp-key
`), 0o600))
	return p
}
