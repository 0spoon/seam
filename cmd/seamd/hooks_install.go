package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/katata/seam/internal/config"
)

// Defaults for the SessionStart hook installer. The marker key lives inside
// the matcher entry (which is a JSON object Claude Code only inspects shape
// keys on) so we can find our entry on subsequent installs without false
// matches against user-authored hooks.
const (
	hookMatcher       = "startup|resume|clear|compact"
	hookSeamMarkerKey = "seam_managed"
	hookEndpointPath  = "/api/hooks/session-start"
	hookHTTPTimeoutMs = 3000
)

// runInstallHooks is the entry point for `seamd install-hooks`. It writes a
// SessionStart entry into ~/.claude/settings.json that points at this
// machine's seamd, authenticated with the configured MCP API key.
func runInstallHooks(args []string) error {
	fs := flag.NewFlagSet("install-hooks", flag.ContinueOnError)
	configPath := fs.String("config", "seam-server.yaml", "path to seam-server.yaml")
	urlOverride := fs.String("url", "", "override the hook URL (use when seamd lives behind a reverse proxy)")
	settingsPath := fs.String("settings", "", "override the path to Claude Code's settings.json (default: ~/.claude/settings.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.LoadForTools(*configPath)
	if err != nil {
		return fmt.Errorf("install-hooks: %w", err)
	}

	if cfg.MCP.APIKey == "" {
		return errors.New(
			"install-hooks: mcp.api_key is not set\n" +
				"\n" +
				"  Generate one with:\n" +
				"      openssl rand -hex 32\n" +
				"\n" +
				"  Then put it under `mcp.api_key:` in seam-server.yaml (or export\n" +
				"  SEAM_MCP_API_KEY), restart seamd, and re-run install-hooks.",
		)
	}

	hookURL := *urlOverride
	if hookURL == "" {
		computed, err := computeHookURL(cfg.Listen)
		if err != nil {
			return fmt.Errorf("install-hooks: %w", err)
		}
		hookURL = computed
	}

	target, err := resolveSettingsPath(*settingsPath)
	if err != nil {
		return fmt.Errorf("install-hooks: %w", err)
	}

	settings, mode, err := loadSettings(target)
	if err != nil {
		return fmt.Errorf("install-hooks: %w", err)
	}

	updated, action, err := mergeSeamHook(settings, hookURL, cfg.MCP.APIKey)
	if err != nil {
		return fmt.Errorf("install-hooks: %w", err)
	}

	// Only touch disk when something actually changed; this keeps repeat
	// runs from creating spurious backups and writing identical bytes.
	if action != mergeActionUnchanged {
		if err := backupSettingsOnce(target, mode); err != nil {
			return fmt.Errorf("install-hooks: %w", err)
		}
		if err := saveSettingsAtomic(target, updated, mode); err != nil {
			return fmt.Errorf("install-hooks: %w", err)
		}
	}

	color := newColorPrinter()
	color.ok(fmt.Sprintf("Installed seam SessionStart hook at %s", target))
	color.ok(fmt.Sprintf("URL:    %s", hookURL))
	switch action {
	case mergeActionAppended:
		color.info("Action: appended new entry")
	case mergeActionUpdated:
		color.info("Action: updated existing entry in place")
	case mergeActionUnchanged:
		color.info("Action: no change (entry already up to date)")
	}
	color.info("'make install-service' does NOT install this hook; uninstall with 'make uninstall-claude-hooks'.")
	return nil
}

// runUninstallHooks removes any seam-managed entries from the SessionStart
// hooks list, leaving the rest of settings.json untouched.
func runUninstallHooks(args []string) error {
	fs := flag.NewFlagSet("uninstall-hooks", flag.ContinueOnError)
	settingsPath := fs.String("settings", "", "override the path to Claude Code's settings.json (default: ~/.claude/settings.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	target, err := resolveSettingsPath(*settingsPath)
	if err != nil {
		return fmt.Errorf("uninstall-hooks: %w", err)
	}

	color := newColorPrinter()
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		color.warn(fmt.Sprintf("%s does not exist; nothing to uninstall", target))
		return nil
	}

	settings, mode, err := loadSettings(target)
	if err != nil {
		return fmt.Errorf("uninstall-hooks: %w", err)
	}

	updated, removed := removeSeamHook(settings)
	if removed == 0 {
		color.warn(fmt.Sprintf("no seam-managed SessionStart entries found in %s", target))
		return nil
	}

	if err := saveSettingsAtomic(target, updated, mode); err != nil {
		return fmt.Errorf("uninstall-hooks: %w", err)
	}
	color.ok(fmt.Sprintf("Removed %d seam-managed SessionStart entr%s from %s", removed, plural(removed, "y", "ies"), target))
	return nil
}

func plural(n int, single, many string) string {
	if n == 1 {
		return single
	}
	return many
}

// resolveSettingsPath returns either the explicit override or the canonical
// ~/.claude/settings.json. Symlinks are resolved here so we always write to
// the underlying file rather than rewriting the link.
func resolveSettingsPath(override string) (string, error) {
	path := override
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home: %w", err)
		}
		path = filepath.Join(home, ".claude", "settings.json")
	}
	// Expand ~ for explicit overrides like "--settings ~/foo/settings.json".
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("absolute path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	// EvalSymlinks fails when the file does not yet exist; that is fine.
	// Make sure the parent directory exists so we can resolve it.
	if _, err := filepath.EvalSymlinks(filepath.Dir(abs)); err != nil {
		// Parent doesn't exist either; create it (mkdir -p).
		if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
			return "", fmt.Errorf("create settings dir: %w", mkErr)
		}
	}
	return abs, nil
}

// settingsFile is a permissive top-level shape: every key Claude Code might
// add stays as raw JSON so we can round-trip the file without dropping
// fields we don't recognize.
type settingsFile map[string]json.RawMessage

// loadSettings reads settings.json. Missing file returns an empty map (so a
// fresh install creates one). Malformed JSON returns an error referencing
// the file path so the user knows where to look.
func loadSettings(path string) (settingsFile, os.FileMode, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settingsFile{}, 0o600, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("read %s: %w", path, err)
	}
	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if len(data) == 0 {
		return settingsFile{}, mode, nil
	}
	settings := settingsFile{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, 0, fmt.Errorf("parse %s: %w", path, err)
	}
	return settings, mode, nil
}

// mergeAction reports what mergeSeamHook did to the file.
type mergeAction int

const (
	mergeActionAppended mergeAction = iota
	mergeActionUpdated
	mergeActionUnchanged
)

// mergeSeamHook idempotently merges the seam SessionStart entry into
// settings.hooks.SessionStart, preserving any user-authored entries. The
// match is on the seam_managed marker, NOT the URL — users may legitimately
// run multiple seamd setups (laptop + remote) with different URLs and
// expect both hooks to coexist when they re-run install-hooks.
//
// However, an existing seam_managed entry IS updated in place rather than
// duplicated, because the URL or API key may have rotated.
func mergeSeamHook(settings settingsFile, hookURL, apiKey string) (settingsFile, mergeAction, error) {
	hooksRaw, hasHooks := settings["hooks"]
	hooksMap := map[string]json.RawMessage{}
	if hasHooks {
		if err := json.Unmarshal(hooksRaw, &hooksMap); err != nil {
			return nil, 0, fmt.Errorf("hooks must be an object: %w", err)
		}
	}

	var sessionStart []json.RawMessage
	if raw, ok := hooksMap["SessionStart"]; ok {
		if err := json.Unmarshal(raw, &sessionStart); err != nil {
			return nil, 0, fmt.Errorf("hooks.SessionStart must be an array: %w", err)
		}
	}

	desired := newSeamHookEntryRaw(hookURL, apiKey)

	action := mergeActionAppended
	replaced := false
	for i, entry := range sessionStart {
		if !isSeamManaged(entry) {
			continue
		}
		// Replace in place. Compare against the desired entry to know
		// whether anything actually changed.
		if rawEqualSemantically(entry, desired) {
			action = mergeActionUnchanged
		} else {
			action = mergeActionUpdated
		}
		sessionStart[i] = desired
		replaced = true
		break
	}
	if !replaced {
		sessionStart = append(sessionStart, desired)
	}

	newSessionStart, err := json.Marshal(sessionStart)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal SessionStart: %w", err)
	}
	hooksMap["SessionStart"] = newSessionStart
	newHooks, err := json.Marshal(hooksMap)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal hooks: %w", err)
	}
	settings["hooks"] = newHooks
	return settings, action, nil
}

// removeSeamHook drops every seam-managed entry from
// hooks.SessionStart and returns the new settings + the count removed.
func removeSeamHook(settings settingsFile) (settingsFile, int) {
	hooksRaw, hasHooks := settings["hooks"]
	if !hasHooks {
		return settings, 0
	}
	hooksMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(hooksRaw, &hooksMap); err != nil {
		return settings, 0
	}
	raw, ok := hooksMap["SessionStart"]
	if !ok {
		return settings, 0
	}
	var sessionStart []json.RawMessage
	if err := json.Unmarshal(raw, &sessionStart); err != nil {
		return settings, 0
	}
	kept := sessionStart[:0]
	removed := 0
	for _, entry := range sessionStart {
		if isSeamManaged(entry) {
			removed++
			continue
		}
		kept = append(kept, entry)
	}
	if removed == 0 {
		return settings, 0
	}
	if len(kept) == 0 {
		delete(hooksMap, "SessionStart")
	} else {
		newSessionStart, err := json.Marshal(kept)
		if err != nil {
			return settings, 0
		}
		hooksMap["SessionStart"] = newSessionStart
	}
	if len(hooksMap) == 0 {
		delete(settings, "hooks")
	} else {
		newHooks, err := json.Marshal(hooksMap)
		if err != nil {
			return settings, 0
		}
		settings["hooks"] = newHooks
	}
	return settings, removed
}

// isSeamManaged reports whether a single SessionStart entry was created by
// seam (i.e. carries the seam_managed: true marker at the top level).
func isSeamManaged(raw json.RawMessage) bool {
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		return false
	}
	v, ok := entry[hookSeamMarkerKey]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// newSeamHookEntryRaw builds the canonical seam SessionStart entry as raw
// JSON so it can sit alongside any other shape in the SessionStart array
// without being normalized through a strict struct.
func newSeamHookEntryRaw(hookURL, apiKey string) json.RawMessage {
	entry := map[string]any{
		"matcher":         hookMatcher,
		hookSeamMarkerKey: true,
		"hooks": []map[string]any{
			{
				"type":    "http",
				"url":     hookURL,
				"timeout": hookHTTPTimeoutMs,
				"headers": map[string]string{
					"Authorization": "Bearer " + apiKey,
				},
			},
		},
	}
	data, _ := json.Marshal(entry)
	return data
}

// rawEqualSemantically compares two JSON messages by re-marshaling with
// sorted keys, so {"a":1,"b":2} == {"b":2,"a":1}.
func rawEqualSemantically(a, b json.RawMessage) bool {
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	ac, err := canonicalize(av)
	if err != nil {
		return false
	}
	bc, err := canonicalize(bv)
	if err != nil {
		return false
	}
	return string(ac) == string(bc)
}

// canonicalize re-encodes a parsed JSON value with sorted object keys so
// two semantically-equivalent payloads compare byte-for-byte.
func canonicalize(v any) ([]byte, error) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			b.Write(kb)
			b.WriteByte(':')
			cb, err := canonicalize(t[k])
			if err != nil {
				return nil, err
			}
			b.Write(cb)
		}
		b.WriteByte('}')
		return []byte(b.String()), nil
	case []any:
		var b strings.Builder
		b.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			cb, err := canonicalize(item)
			if err != nil {
				return nil, err
			}
			b.Write(cb)
		}
		b.WriteByte(']')
		return []byte(b.String()), nil
	default:
		return json.Marshal(v)
	}
}

// computeHookURL turns a Listen address into a URL Claude Code can hit.
// Bind-all addresses (empty, ":N", "0.0.0.0:N", "[::]:N") collapse to
// 127.0.0.1 because 0.0.0.0 is not a valid destination on macOS.
func computeHookURL(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		// Treat the whole string as a port if it starts with ":" (Go's
		// SplitHostPort handles ":8080", but a literal "8080" is invalid
		// — fall through to defaults).
		if listen == "" {
			host, port = "127.0.0.1", "8080"
		} else {
			return "", fmt.Errorf("parse listen %q: %w", listen, err)
		}
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s%s", net.JoinHostPort(host, port), hookEndpointPath), nil
}

// backupSettingsOnce writes a settings.json.seam-bak-<ts> sibling the first
// time install-hooks runs against this file. Subsequent runs see the existing
// backup and skip — we don't want to lose the original by overwriting our
// own backup with a copy of our own write.
func backupSettingsOnce(target string, mode os.FileMode) error {
	dir := filepath.Dir(target)
	base := filepath.Base(target)
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Parent missing: install-hooks will create the file fresh, so
		// no original to back up.
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	prefix := base + ".seam-bak-"
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			return nil // backup already exists
		}
	}
	// Nothing to back up if the file itself doesn't exist yet.
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	src, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("read for backup: %w", err)
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	bakPath := filepath.Join(dir, prefix+stamp)
	if err := os.WriteFile(bakPath, src, mode); err != nil {
		return fmt.Errorf("write backup %s: %w", bakPath, err)
	}
	return nil
}

// saveSettingsAtomic writes settings to disk via tmp-file + fsync + rename.
// The mode is restored from the original (or 0600 for new files), so we
// don't accidentally widen permissions.
func saveSettingsAtomic(path string, settings settingsFile, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o600
	}

	data, err := marshalSettingsIndented(settings)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	// Trailing newline matches what Claude Code itself writes; nice for
	// diff-friendly editors.
	if !strings.HasSuffix(string(data), "\n") {
		data = append(data, '\n')
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op on success because rename consumes the file

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("fsync tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("chmod tmp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// marshalSettingsIndented round-trips through a generic any so the top-level
// keys come out in stable sorted order (json.RawMessage at the map values
// preserves nested ordering as-is).
func marshalSettingsIndented(settings settingsFile) ([]byte, error) {
	// Convert RawMessages to any so json.MarshalIndent can sort top-level
	// keys deterministically.
	out := map[string]any{}
	for k, v := range settings {
		var parsed any
		if err := json.Unmarshal(v, &parsed); err != nil {
			return nil, fmt.Errorf("unmarshal %q: %w", k, err)
		}
		out[k] = parsed
	}
	return json.MarshalIndent(out, "", "  ")
}

// --- color printer (mirrors scripts/install-onboard-skill.sh style) ---

type colorPrinter struct{ enabled bool }

func newColorPrinter() *colorPrinter {
	return &colorPrinter{enabled: termSupportsColor()}
}
func termSupportsColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
func (c *colorPrinter) info(msg string) { c.print("1;34", msg) }
func (c *colorPrinter) ok(msg string)   { c.print("1;32", msg) }
func (c *colorPrinter) warn(msg string) { c.print("1;33", msg) }
func (c *colorPrinter) err(msg string)  { c.print("1;31", msg) }
func (c *colorPrinter) print(code, msg string) {
	if c.enabled {
		fmt.Printf("\033[%sm==>\033[0m %s\n", code, msg)
	} else {
		fmt.Printf("==> %s\n", msg)
	}
}
