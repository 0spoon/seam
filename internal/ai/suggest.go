package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// TagSuggestion represents a suggested tag with a confidence score.
type TagSuggestion struct {
	Name       string  `json:"name"`
	Confidence float64 `json:"confidence"`
}

// ProjectSuggestion represents a suggested project with a confidence score.
type ProjectSuggestion struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Confidence float64 `json:"confidence"`
}

// Suggester uses an LLM to suggest tags and projects for notes.
type Suggester struct {
	chat   ChatCompleter
	model  string
	logger *slog.Logger
}

// NewSuggester creates a new Suggester.
func NewSuggester(chat ChatCompleter, model string, logger *slog.Logger) *Suggester {
	if logger == nil {
		logger = slog.Default()
	}
	return &Suggester{
		chat:   chat,
		model:  model,
		logger: logger,
	}
}

// SuggestTags asks the LLM which of the existing tags apply to the given note.
// Returns empty suggestions (not an error) if the LLM is unavailable.
func (s *Suggester) SuggestTags(ctx context.Context, noteTitle, noteBody string, existingTags []string) ([]TagSuggestion, error) {
	if s.chat == nil {
		return []TagSuggestion{}, nil
	}

	if len(existingTags) == 0 {
		return []TagSuggestion{}, nil
	}

	// Truncate body to avoid overloading the prompt.
	body := noteBody
	if runes := []rune(body); len(runes) > 3000 {
		body = string(runes[:3000])
	}

	prompt := fmt.Sprintf(`You are a tag classifier for a personal knowledge base.

Given the note below, determine which of the EXISTING tags apply. Only suggest tags from the provided list.
Return a JSON array of objects with "name" (the tag) and "confidence" (0.0 to 1.0).
Only include tags with confidence >= 0.5. Return at most 5 tags.
Return ONLY the JSON array, no other text.

EXISTING TAGS: %s

NOTE TITLE: %s

NOTE BODY:
%s

JSON:`, strings.Join(existingTags, ", "), noteTitle, body)

	messages := []ChatMessage{
		{Role: "system", Content: "You are a precise JSON-only classifier. Return only valid JSON arrays."},
		{Role: "user", Content: prompt},
	}

	resp, err := s.chat.ChatCompletion(ctx, s.model, messages)
	if err != nil {
		s.logger.Warn("ai.Suggester.SuggestTags: LLM call failed, returning empty", "error", err)
		return []TagSuggestion{}, nil
	}

	suggestions, err := parseTagSuggestions(resp.Content, existingTags)
	if err != nil {
		s.logger.Warn("ai.Suggester.SuggestTags: parse failed, returning empty", "error", err)
		return []TagSuggestion{}, nil
	}

	return suggestions, nil
}

// SuggestProject asks the LLM which project the note fits best.
// Returns empty suggestions (not an error) if the LLM is unavailable.
func (s *Suggester) SuggestProject(ctx context.Context, noteTitle, noteBody string, projects []ProjectInfo) ([]ProjectSuggestion, error) {
	if s.chat == nil {
		return []ProjectSuggestion{}, nil
	}

	if len(projects) == 0 {
		return []ProjectSuggestion{}, nil
	}

	// Truncate body to avoid overloading the prompt.
	body := noteBody
	if runes := []rune(body); len(runes) > 3000 {
		body = string(runes[:3000])
	}

	// Build project list for the prompt.
	var projectList strings.Builder
	for _, p := range projects {
		desc := p.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&projectList, "- ID: %s, Name: %s, Description: %s\n", p.ID, p.Name, desc)
	}

	prompt := fmt.Sprintf(`You are a classifier for a personal knowledge base.

Given the note below, determine which project it belongs to. Only suggest projects from the provided list.
Return a JSON array of objects with "id" (project ID), "name" (project name), and "confidence" (0.0 to 1.0).
Only include projects with confidence >= 0.5. Return at most 3 projects.
Return ONLY the JSON array, no other text.

PROJECTS:
%s

NOTE TITLE: %s

NOTE BODY:
%s

JSON:`, projectList.String(), noteTitle, body)

	messages := []ChatMessage{
		{Role: "system", Content: "You are a precise JSON-only classifier. Return only valid JSON arrays."},
		{Role: "user", Content: prompt},
	}

	resp, err := s.chat.ChatCompletion(ctx, s.model, messages)
	if err != nil {
		s.logger.Warn("ai.Suggester.SuggestProject: LLM call failed, returning empty", "error", err)
		return []ProjectSuggestion{}, nil
	}

	suggestions, err := parseProjectSuggestions(resp.Content, projects)
	if err != nil {
		s.logger.Warn("ai.Suggester.SuggestProject: parse failed, returning empty", "error", err)
		return []ProjectSuggestion{}, nil
	}

	return suggestions, nil
}

// ProjectInfo holds the project metadata needed for suggestions.
type ProjectInfo struct {
	ID          string
	Name        string
	Description string
}

// parseTagSuggestions extracts tag suggestions from LLM output, filtering to
// only tags in the existing vocabulary.
func parseTagSuggestions(raw string, existingTags []string) ([]TagSuggestion, error) {
	// Build a lookup of valid tags (case-insensitive match).
	valid := make(map[string]string, len(existingTags))
	for _, t := range existingTags {
		valid[strings.ToLower(t)] = t
	}

	// Try to find JSON array in the response.
	content := extractJSON(raw)

	var parsed []TagSuggestion
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("parseTagSuggestions: %w", err)
	}

	// Filter to only valid tags with reasonable confidence.
	var result []TagSuggestion
	for _, s := range parsed {
		canonical, ok := valid[strings.ToLower(s.Name)]
		if !ok {
			continue
		}
		if s.Confidence < 0.5 {
			continue
		}
		if s.Confidence > 1.0 {
			s.Confidence = 1.0
		}
		result = append(result, TagSuggestion{
			Name:       canonical,
			Confidence: s.Confidence,
		})
	}

	if result == nil {
		result = []TagSuggestion{}
	}

	return result, nil
}

// parseProjectSuggestions extracts project suggestions from LLM output,
// filtering to only known projects.
func parseProjectSuggestions(raw string, projects []ProjectInfo) ([]ProjectSuggestion, error) {
	// Build a lookup of valid project IDs.
	valid := make(map[string]ProjectInfo, len(projects))
	for _, p := range projects {
		valid[p.ID] = p
	}

	content := extractJSON(raw)

	var parsed []ProjectSuggestion
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("parseProjectSuggestions: %w", err)
	}

	var result []ProjectSuggestion
	for _, s := range parsed {
		p, ok := valid[s.ID]
		if !ok {
			continue
		}
		if s.Confidence < 0.5 {
			continue
		}
		if s.Confidence > 1.0 {
			s.Confidence = 1.0
		}
		result = append(result, ProjectSuggestion{
			ID:         p.ID,
			Name:       p.Name,
			Confidence: s.Confidence,
		})
	}

	if result == nil {
		result = []ProjectSuggestion{}
	}

	return result, nil
}

// extractJSON tries to find a JSON array in LLM output that may contain
// markdown code fences or other wrapping text.
func extractJSON(raw string) string {
	s := strings.TrimSpace(raw)

	// Strip markdown code fence.
	if strings.HasPrefix(s, "```") {
		// Remove opening fence (possibly with language tag).
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		// Remove closing fence.
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	}

	// If it starts with '[', assume it's already JSON.
	if strings.HasPrefix(s, "[") {
		return s
	}

	// Try to find array brackets.
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start >= 0 && end > start {
		return s[start : end+1]
	}

	return s
}
