package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
)

// RepoProjectMapSetting is the settings key holding the JSON object that maps
// absolute repo paths to Seam project slugs.
const RepoProjectMapSetting = "repo_project_map"

// ResolveProjectForCWD maps an absolute working directory to a Seam project
// slug via the repo_project_map setting. Longest-prefix match wins; "" when
// unmapped. Paths are compared with separators normalized and a path-boundary
// check so "/a/b" does not match cwd "/a/bc".
//
// Failure-soft: any error (no settings service, unreadable setting, malformed
// JSON) yields "" and a Debug log, never an error.
func (s *Service) ResolveProjectForCWD(ctx context.Context, userID, cwd string) string {
	if cwd == "" || s.cfg.SettingsService == nil {
		return ""
	}
	all, err := s.cfg.SettingsService.GetAll(ctx, userID)
	if err != nil {
		s.cfg.Logger.Debug("agent.ResolveProjectForCWD: get settings", "error", err)
		return ""
	}
	raw := all[RepoProjectMapSetting]
	if raw == "" || raw == "{}" {
		return ""
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		s.cfg.Logger.Debug("agent.ResolveProjectForCWD: malformed repo_project_map", "error", err)
		return ""
	}

	cleanCWD := filepath.Clean(cwd)
	best := ""
	bestLen := -1
	for prefix, slug := range m {
		if slug == "" {
			continue
		}
		p := filepath.Clean(prefix)
		if !pathHasPrefix(cleanCWD, p) {
			continue
		}
		if len(p) > bestLen {
			bestLen = len(p)
			best = slug
		}
	}
	return best
}

// pathHasPrefix reports whether path equals prefix or lies under prefix with a
// separator boundary. "/a/b" is a prefix of "/a/b/c" but not "/a/bc".
func pathHasPrefix(path, prefix string) bool {
	if prefix == "" {
		return false
	}
	if path == prefix {
		return true
	}
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	// Boundary: either prefix already ends at a separator, or the next
	// character in path is a separator.
	if prefix[len(prefix)-1] == filepath.Separator {
		return true
	}
	return path[len(prefix)] == filepath.Separator
}
