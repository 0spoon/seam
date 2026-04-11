package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/katata/seam/internal/config"
)

// runDoctor implements `seamd doctor`. It runs an end-to-end self-check
// against the local seamd, the Claude Code MCP registry (~/.claude.json),
// and the SessionStart hook entry in ~/.claude/settings.json.
//
// Doctor never mutates anything. Each check is independent and reports its
// own status; the exit code is non-zero if any check is at "error" level.
// Warnings are informational and never fail the run.
func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	configPath := fs.String("config", "seam-server.yaml", "path to seam-server.yaml")
	settingsPath := fs.String("settings", "", "override the path to Claude Code's settings.json (default: ~/.claude/settings.json)")
	mcpRegistryPath := fs.String("mcp-registry", "", "override the path to Claude Code's MCP registry (default: ~/.claude.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	color := newColorPrinter()
	results := []doctorResult{}

	cfg, configRes := checkConfigParses(*configPath)
	results = append(results, configRes)

	results = append(results, checkSeamdReachable(cfg))
	results = append(results, checkClaudeMCPRegistration(cfg, *mcpRegistryPath))
	results = append(results, checkSessionStartHookInstalled(cfg, *settingsPath))
	results = append(results, checkHookEndpointReachable(cfg))

	for _, r := range results {
		switch r.level {
		case doctorOK:
			color.ok(r.message)
		case doctorWarn:
			color.warn(r.message)
		case doctorError:
			color.err(r.message)
		}
	}

	warns, errs := 0, 0
	for _, r := range results {
		switch r.level {
		case doctorWarn:
			warns++
		case doctorError:
			errs++
		}
	}
	fmt.Println()
	fmt.Printf("%d warnings, %d errors.\n", warns, errs)

	if errs > 0 {
		return errors.New("doctor: one or more checks failed")
	}
	return nil
}

// doctorLevel is the severity of a single check result.
type doctorLevel int

const (
	doctorOK doctorLevel = iota
	doctorWarn
	doctorError
)

type doctorResult struct {
	level   doctorLevel
	message string
}

// checkConfigParses runs config.LoadForTools and asserts mcp.api_key is set.
// Returns the cfg pointer for downstream checks (or nil on error).
func checkConfigParses(path string) (*config.Config, doctorResult) {
	cfg, err := config.LoadForTools(path)
	if err != nil {
		return nil, doctorResult{
			level:   doctorError,
			message: fmt.Sprintf("seam-server.yaml: %v", err),
		}
	}
	if cfg.MCP.APIKey == "" {
		return cfg, doctorResult{
			level:   doctorError,
			message: "seam-server.yaml: mcp.api_key is empty (run `openssl rand -hex 32` and set it under mcp.api_key)",
		}
	}
	return cfg, doctorResult{
		level:   doctorOK,
		message: fmt.Sprintf("%s parses, mcp.api_key set", path),
	}
}

// checkSeamdReachable hits /api/health on the configured listen address.
// "Not running" is a warning rather than an error: the user may run doctor
// from a context where seamd lives elsewhere (laptop config, remote box).
func checkSeamdReachable(cfg *config.Config) doctorResult {
	if cfg == nil {
		return doctorResult{level: doctorWarn, message: "skipping seamd reachability: config not loaded"}
	}
	url, err := computeHealthURL(cfg.Listen)
	if err != nil {
		return doctorResult{level: doctorError, message: fmt.Sprintf("invalid listen %q: %v", cfg.Listen, err)}
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return doctorResult{
			level:   doctorWarn,
			message: fmt.Sprintf("seamd not reachable at %s (%v) — start it with `make run` or `make service-start`", url, err),
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return doctorResult{
			level:   doctorWarn,
			message: fmt.Sprintf("seamd at %s returned status %d", url, resp.StatusCode),
		}
	}
	return doctorResult{level: doctorOK, message: fmt.Sprintf("seamd reachable at %s", strings.TrimSuffix(url, "/api/health"))}
}

// checkClaudeMCPRegistration reads ~/.claude.json directly. We do NOT shell
// to `claude mcp get seam` because (a) the CLI may not be on PATH, (b) the
// output format is unstable, and (c) it is slow.
func checkClaudeMCPRegistration(cfg *config.Config, override string) doctorResult {
	path := override
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return doctorResult{level: doctorError, message: fmt.Sprintf("resolve home: %v", err)}
		}
		path = filepath.Join(home, ".claude.json")
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return doctorResult{
			level:   doctorWarn,
			message: fmt.Sprintf("%s does not exist — Claude Code has never been launched on this machine", path),
		}
	}
	if err != nil {
		return doctorResult{level: doctorError, message: fmt.Sprintf("read %s: %v", path, err)}
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return doctorResult{level: doctorError, message: fmt.Sprintf("parse %s: %v", path, err)}
	}

	servers, _ := raw["mcpServers"].(map[string]any)
	if servers == nil {
		return doctorResult{
			level:   doctorWarn,
			message: fmt.Sprintf("%s has no mcpServers section — register Seam with `claude mcp add --scope user ...` (or run `/seam-onboard`)", path),
		}
	}
	seam, ok := servers["seam"].(map[string]any)
	if !ok {
		return doctorResult{
			level:   doctorWarn,
			message: "Claude Code has no `seam` MCP entry — run `/seam-onboard` to register it at user scope",
		}
	}
	url, _ := seam["url"].(string)
	if url == "" {
		return doctorResult{level: doctorError, message: "Claude Code's `seam` MCP entry has no URL"}
	}

	expectedURL := ""
	if cfg != nil {
		if u, err := computeMCPURL(cfg.Listen); err == nil {
			expectedURL = u
		}
	}
	if expectedURL != "" && url != expectedURL {
		return doctorResult{
			level:   doctorWarn,
			message: fmt.Sprintf("Claude Code's `seam` MCP URL is %q but seamd is configured for %q", url, expectedURL),
		}
	}
	return doctorResult{level: doctorOK, message: fmt.Sprintf("Seam MCP registered with Claude Code at %s", url)}
}

// checkSessionStartHookInstalled walks ~/.claude/settings.json and looks for
// a SessionStart entry marked seam_managed: true.
func checkSessionStartHookInstalled(cfg *config.Config, override string) doctorResult {
	target, err := resolveSettingsPath(override)
	if err != nil {
		return doctorResult{level: doctorError, message: fmt.Sprintf("resolve settings path: %v", err)}
	}
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		return doctorResult{
			level:   doctorWarn,
			message: fmt.Sprintf("%s does not exist — run `make install-claude-hooks` to install the SessionStart hook", target),
		}
	}
	settings, _, err := loadSettings(target)
	if err != nil {
		return doctorResult{level: doctorError, message: fmt.Sprintf("load settings: %v", err)}
	}
	hooksRaw, ok := settings["hooks"]
	if !ok {
		return doctorResult{
			level:   doctorWarn,
			message: fmt.Sprintf("%s has no hooks section — run `make install-claude-hooks`", target),
		}
	}
	var hooksMap map[string]json.RawMessage
	if err := json.Unmarshal(hooksRaw, &hooksMap); err != nil {
		return doctorResult{level: doctorError, message: fmt.Sprintf("parse hooks: %v", err)}
	}
	raw, ok := hooksMap["SessionStart"]
	if !ok {
		return doctorResult{
			level:   doctorWarn,
			message: fmt.Sprintf("%s has no hooks.SessionStart entries — run `make install-claude-hooks`", target),
		}
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return doctorResult{level: doctorError, message: fmt.Sprintf("parse SessionStart: %v", err)}
	}

	for _, entry := range entries {
		if !isSeamManaged(entry) {
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal(entry, &parsed); err != nil {
			continue
		}
		hookList, _ := parsed["hooks"].([]any)
		if len(hookList) == 0 {
			return doctorResult{level: doctorError, message: "seam-managed SessionStart entry has no hooks list"}
		}
		first, _ := hookList[0].(map[string]any)
		if first == nil {
			return doctorResult{level: doctorError, message: "seam-managed SessionStart entry has malformed hook"}
		}
		url, _ := first["url"].(string)
		expected := ""
		if cfg != nil {
			if u, err := computeHookURL(cfg.Listen); err == nil {
				expected = u
			}
		}
		if expected != "" && url != expected {
			return doctorResult{
				level:   doctorWarn,
				message: fmt.Sprintf("SessionStart hook URL is %q but seamd is configured for %q (re-run `make install-claude-hooks`)", url, expected),
			}
		}
		headers, _ := first["headers"].(map[string]any)
		auth, _ := headers["Authorization"].(string)
		if cfg != nil && auth != "Bearer "+cfg.MCP.APIKey {
			return doctorResult{
				level:   doctorWarn,
				message: "SessionStart hook Authorization header does not match mcp.api_key (re-run `make install-claude-hooks`)",
			}
		}
		return doctorResult{level: doctorOK, message: fmt.Sprintf("SessionStart hook installed in %s", target)}
	}
	return doctorResult{
		level:   doctorWarn,
		message: fmt.Sprintf("no seam-managed SessionStart entry found in %s — run `make install-claude-hooks`", target),
	}
}

// checkHookEndpointReachable POSTs a synthetic payload at the hook endpoint
// to verify the auth chain and the response shape end-to-end. Skips silently
// when the config is missing or seamd isn't running.
func checkHookEndpointReachable(cfg *config.Config) doctorResult {
	if cfg == nil {
		return doctorResult{level: doctorWarn, message: "skipping hook endpoint check: config not loaded"}
	}
	if cfg.MCP.APIKey == "" {
		return doctorResult{level: doctorWarn, message: "skipping hook endpoint check: mcp.api_key empty"}
	}
	hookURL, err := computeHookURL(cfg.Listen)
	if err != nil {
		return doctorResult{level: doctorError, message: fmt.Sprintf("invalid listen %q: %v", cfg.Listen, err)}
	}
	body, _ := json.Marshal(map[string]string{"source": "startup"})
	req, err := http.NewRequest(http.MethodPost, hookURL, bytes.NewReader(body))
	if err != nil {
		return doctorResult{level: doctorError, message: fmt.Sprintf("build request: %v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+cfg.MCP.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return doctorResult{level: doctorWarn, message: fmt.Sprintf("hook endpoint not reachable (%v) — is seamd running?", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return doctorResult{level: doctorError, message: fmt.Sprintf("hook endpoint returned status %d", resp.StatusCode)}
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return doctorResult{level: doctorError, message: fmt.Sprintf("read hook response: %v", err)}
	}
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return doctorResult{level: doctorError, message: fmt.Sprintf("hook response is not JSON: %v", err)}
	}
	hso, _ := parsed["hookSpecificOutput"].(map[string]any)
	if hso == nil {
		return doctorResult{level: doctorError, message: "hook response missing hookSpecificOutput"}
	}
	if hso["hookEventName"] != "SessionStart" {
		return doctorResult{level: doctorError, message: fmt.Sprintf("hook response has wrong hookEventName %q", hso["hookEventName"])}
	}
	additional, _ := hso["additionalContext"].(string)
	return doctorResult{
		level:   doctorOK,
		message: fmt.Sprintf("%s responds with valid briefing (%d chars)", hookURL, len(additional)),
	}
}

// computeHealthURL is the doctor-side analog of computeHookURL: turns a
// Listen address into a URL Claude Code clients can hit. Reuses the same
// localhost-collapse rules.
func computeHealthURL(listen string) (string, error) {
	url, err := computeHookURL(listen)
	if err != nil {
		return "", err
	}
	return strings.Replace(url, "/api/hooks/session-start", "/api/health", 1), nil
}

// computeMCPURL builds the URL Claude Code's MCP client should target.
func computeMCPURL(listen string) (string, error) {
	url, err := computeHookURL(listen)
	if err != nil {
		return "", err
	}
	return strings.Replace(url, "/api/hooks/session-start", "/api/mcp", 1), nil
}
