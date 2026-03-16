// Package template provides a note template system with shared defaults
// and per-user overrides. Templates are .md files with {{var}} placeholders.
package template

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/katata/seam/internal/userdb"
)

// Domain errors.
var (
	ErrTemplateNotFound = errors.New("template not found")
	ErrInvalidName      = errors.New("invalid template name")
)

// TemplateMeta holds metadata about a template (for listing).
type TemplateMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Template holds the full template including its body.
type Template struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"` // raw content with {{var}} placeholders
}

// Service manages note templates: loading from disk, applying variable substitution.
type Service struct {
	dataDir   string
	dbManager userdb.Manager
	logger    *slog.Logger
}

// NewService creates a new template Service.
func NewService(dataDir string, dbManager userdb.Manager, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		dataDir:   dataDir,
		dbManager: dbManager,
		logger:    logger,
	}
}

// EnsureDefaults creates the shared templates directory and writes default
// templates if they do not already exist. Called at startup.
func (s *Service) EnsureDefaults() error {
	dir := s.sharedTemplatesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("template.Service.EnsureDefaults: mkdir: %w", err)
	}

	for name, content := range defaultTemplates {
		path := filepath.Join(dir, name+".md")
		if _, err := os.Stat(path); err == nil {
			continue // already exists, do not overwrite
		}
		if err := atomicWriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("template.Service.EnsureDefaults: write %s: %w", name, err)
		}
	}

	return nil
}

// List returns metadata for all available templates (per-user overrides + shared).
func (s *Service) List(ctx context.Context, userID string) ([]TemplateMeta, error) {
	seen := make(map[string]bool)
	var result []TemplateMeta

	// Per-user templates take priority.
	if userID != "" {
		userDir := s.userTemplatesDir(userID)
		metas, err := s.scanDir(userDir)
		if err == nil {
			for _, m := range metas {
				seen[m.Name] = true
				result = append(result, m)
			}
		}
	}

	// Shared templates.
	metas, err := s.scanDir(s.sharedTemplatesDir())
	if err == nil {
		for _, m := range metas {
			if !seen[m.Name] {
				result = append(result, m)
			}
		}
	}

	if result == nil {
		result = []TemplateMeta{}
	}

	return result, nil
}

// Get returns a template by name, checking per-user first then shared.
func (s *Service) Get(ctx context.Context, userID, name string) (*Template, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}

	// Check per-user templates first.
	if userID != "" {
		t, err := s.loadTemplate(s.userTemplatesDir(userID), name)
		if err == nil {
			return t, nil
		}
	}

	// Fall back to shared templates.
	t, err := s.loadTemplate(s.sharedTemplatesDir(), name)
	if err != nil {
		return nil, fmt.Errorf("template.Service.Get: %w", ErrTemplateNotFound)
	}

	return t, nil
}

// Apply loads a template and substitutes variables, returning the rendered body.
func (s *Service) Apply(ctx context.Context, userID, name string, vars map[string]string) (string, error) {
	tmpl, err := s.Get(ctx, userID, name)
	if err != nil {
		return "", fmt.Errorf("template.Service.Apply: %w", err)
	}

	body := substituteVars(tmpl.Body, vars)
	return body, nil
}

// sharedTemplatesDir returns the path to shared templates.
func (s *Service) sharedTemplatesDir() string {
	return filepath.Join(s.dataDir, "templates")
}

// userTemplatesDir returns the path to a user's templates.
func (s *Service) userTemplatesDir(userID string) string {
	return filepath.Join(s.dbManager.UserDataDir(userID), "templates")
}

// scanDir lists .md files in a directory and returns template metadata.
func (s *Service) scanDir(dir string) ([]TemplateMeta, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var result []TemplateMeta
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		desc := templateDescriptions[name]
		result = append(result, TemplateMeta{Name: name, Description: desc})
	}

	return result, nil
}

// loadTemplate reads a template .md file from the given directory.
func (s *Service) loadTemplate(dir, name string) (*Template, error) {
	path := filepath.Join(dir, name+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return &Template{
		Name:        name,
		Description: templateDescriptions[name],
		Body:        string(data),
	}, nil
}

// substituteVars replaces {{var}} placeholders in the template body.
// Built-in variables: {{date}}, {{datetime}}, {{year}}, {{month}}, {{day}}.
// User-provided vars override built-in ones.
func substituteVars(body string, vars map[string]string) string {
	now := time.Now()
	builtins := map[string]string{
		"date":     now.Format("2006-01-02"),
		"datetime": now.Format("2006-01-02 15:04"),
		"year":     now.Format("2006"),
		"month":    now.Format("01"),
		"day":      now.Format("02"),
		"time":     now.Format("15:04"),
	}

	// Merge user vars (overriding builtins).
	merged := make(map[string]string, len(builtins)+len(vars))
	for k, v := range builtins {
		merged[k] = v
	}
	for k, v := range vars {
		merged[k] = v
	}

	result := body
	for k, v := range merged {
		result = strings.ReplaceAll(result, "{{"+k+"}}", v)
	}

	return result
}

// validateName ensures a template name does not contain path traversal.
func validateName(name string) error {
	if name == "" {
		return ErrInvalidName
	}
	if strings.Contains(name, "..") || strings.Contains(name, "/") ||
		strings.Contains(name, "\\") || strings.ContainsRune(name, 0) {
		return fmt.Errorf("%w: unsafe characters", ErrInvalidName)
	}
	return nil
}

// templateDescriptions maps template names to human-readable descriptions.
var templateDescriptions = map[string]string{
	"meeting-notes":    "Structured meeting notes with attendees, agenda, and action items",
	"project-kickoff":  "Project kickoff document with goals, scope, and timeline",
	"research-summary": "Research summary with key findings and references",
	"daily-log":        "Daily log for tracking progress and thoughts",
}

// defaultTemplates maps template names to their default content.
var defaultTemplates = map[string]string{
	"meeting-notes": `# Meeting Notes - {{date}}

## Attendees
- 

## Agenda
1. 

## Discussion

## Action Items
- [ ] 

## Next Meeting
`,

	"project-kickoff": `# {{project}} - Project Kickoff

**Date:** {{date}}

## Goals
- 

## Scope
### In Scope
- 

### Out of Scope
- 

## Timeline
| Phase | Start | End | Description |
|-------|-------|-----|-------------|
| 1     |       |     |             |

## Team
| Role | Person |
|------|--------|
|      |        |

## Open Questions
- 

## Notes
`,

	"research-summary": `# Research: {{title}}

**Date:** {{date}}

## Key Question


## Key Findings
1. 

## Sources
- 

## Analysis


## Next Steps
- [ ] 
`,

	"daily-log": `# Daily Log - {{date}}

## Plan for Today
- [ ] 

## Progress


## Blockers


## Notes


## Tomorrow
- [ ] 
`,
}

// atomicWriteFile writes data to a file atomically by writing to a temp file
// in the same directory and then renaming.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".seam-tmp-*")
	if err != nil {
		return fmt.Errorf("atomicWriteFile: create temp: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("atomicWriteFile: write: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("atomicWriteFile: chmod: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomicWriteFile: close: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomicWriteFile: rename: %w", err)
	}
	return nil
}
