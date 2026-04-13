// Package librarian is an autonomous background service that organizes
// notes in the knowledge base. It pulls candidates from the review queue
// (orphan, untagged, inbox notes), asks an LLM to classify each one,
// and applies project/tag assignments. It never modifies note content,
// titles, or creates new projects/tags -- only uses what already exists.
//
// Safety: only processes notes that have been quiet for a configurable
// cooldown period (default 15 min) and verifies the content hash has
// not changed between LLM call and update.
package librarian

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/review"
	"github.com/katata/seam/internal/ws"
)

// NoteService captures the subset of note.Service methods the librarian needs.
type NoteService interface {
	Get(ctx context.Context, userID, noteID string) (*note.Note, error)
	Update(ctx context.Context, userID, noteID string, req note.UpdateNoteReq) (*note.Note, error)
	ListTags(ctx context.Context, userID string) ([]note.TagCount, error)
}

// ProjectService captures the subset of project.Service methods the librarian needs.
type ProjectService interface {
	List(ctx context.Context, userID string) ([]*project.Project, error)
}

// ReviewService captures the subset of review.Service methods the librarian needs.
type ReviewService interface {
	GetQueue(ctx context.Context, userID string, limit int) ([]review.ReviewItem, error)
}

// SettingsService captures the subset of settings.Service methods the librarian needs.
type SettingsService interface {
	GetAll(ctx context.Context, userID string) (map[string]string, error)
}

// Config bundles dependencies the librarian service needs.
type Config struct {
	NoteService     NoteService
	ProjectService  ProjectService
	ReviewService   ReviewService
	SettingsService SettingsService
	Chat            ai.ChatCompleter
	ChatModel       string
	Hub             *ws.Hub
	Logger          *slog.Logger
}

// Service organizes notes autonomously via the scheduler.
type Service struct {
	notes    NoteService
	projects ProjectService
	reviews  ReviewService
	settings SettingsService
	chat     ai.ChatCompleter
	model    string
	hub      *ws.Hub
	logger   *slog.Logger
}

// NewService creates a librarian service.
func NewService(cfg Config) *Service {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Service{
		notes:    cfg.NoteService,
		projects: cfg.ProjectService,
		reviews:  cfg.ReviewService,
		settings: cfg.SettingsService,
		chat:     cfg.Chat,
		model:    cfg.ChatModel,
		hub:      cfg.Hub,
		logger:   cfg.Logger,
	}
}

// ActionConfig is the JSON payload stored on a librarian schedule.
type ActionConfig struct {
	CooldownMinutes int `json:"cooldown_minutes,omitempty"`
	MaxPerRun       int `json:"max_per_run,omitempty"`
}

func (c *ActionConfig) applyDefaults() {
	if c.CooldownMinutes <= 0 {
		c.CooldownMinutes = 15
	}
	if c.MaxPerRun <= 0 {
		c.MaxPerRun = 10
	}
}

// Action returns a scheduler-compatible ActionRunner.
func (s *Service) Action() func(ctx context.Context, userID string, config json.RawMessage) error {
	return func(ctx context.Context, userID string, config json.RawMessage) error {
		return s.Run(ctx, userID, config)
	}
}

// classification is the structured response from the LLM.
type classification struct {
	ProjectID string   `json:"project_id"`
	Tags      []string `json:"tags"`
	Rationale string   `json:"rationale"`
}

// Run executes one librarian sweep for the given user.
func (s *Service) Run(ctx context.Context, userID string, rawConfig json.RawMessage) error {
	// Check the librarian_enabled setting.
	settings, err := s.settings.GetAll(ctx, userID)
	if err != nil {
		return fmt.Errorf("librarian.Service.Run: get settings: %w", err)
	}
	if settings["librarian_enabled"] != "true" {
		s.logger.Debug("librarian disabled, skipping sweep", "user_id", userID)
		return nil
	}

	var cfg ActionConfig
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return fmt.Errorf("librarian.Service.Run: parse config: %w", err)
		}
	}
	cfg.applyDefaults()

	// Fetch the review queue (over-fetch to account for skips).
	candidates, err := s.reviews.GetQueue(ctx, userID, cfg.MaxPerRun*3)
	if err != nil {
		return fmt.Errorf("librarian.Service.Run: get queue: %w", err)
	}
	if len(candidates) == 0 {
		s.logger.Debug("librarian: no candidates", "user_id", userID)
		return nil
	}

	// Gather shared context: all projects + all tags.
	projects, err := s.projects.List(ctx, userID)
	if err != nil {
		return fmt.Errorf("librarian.Service.Run: list projects: %w", err)
	}
	tagCounts, err := s.notes.ListTags(ctx, userID)
	if err != nil {
		return fmt.Errorf("librarian.Service.Run: list tags: %w", err)
	}

	// Short-circuit if the user has no projects AND no tags -- nothing
	// to classify into.
	if len(projects) == 0 && len(tagCounts) == 0 {
		s.logger.Debug("librarian: no projects or tags to classify into", "user_id", userID)
		return nil
	}

	cooldown := time.Duration(cfg.CooldownMinutes) * time.Minute
	now := time.Now().UTC()
	processed := 0

	for _, candidate := range candidates {
		if processed >= cfg.MaxPerRun {
			break
		}

		n, err := s.notes.Get(ctx, userID, candidate.NoteID)
		if err != nil {
			s.logger.Warn("librarian: failed to get note",
				"note_id", candidate.NoteID, "error", err)
			continue
		}

		// Skip if updated within the cooldown window.
		if now.Sub(n.UpdatedAt) < cooldown {
			continue
		}

		// Skip if already reviewed by the librarian.
		if hasTag(n.Tags, "librarian:reviewed") {
			continue
		}

		if err := s.processNote(ctx, userID, n, projects, tagCounts); err != nil {
			s.logger.Warn("librarian: failed to process note",
				"note_id", n.ID, "title", n.Title, "error", err)
			continue
		}
		processed++
	}

	s.logger.Info("librarian sweep complete",
		"user_id", userID, "processed", processed, "candidates", len(candidates))
	return nil
}

// processNote classifies a single note and applies the update.
func (s *Service) processNote(
	ctx context.Context,
	userID string,
	n *note.Note,
	projects []*project.Project,
	tagCounts []note.TagCount,
) error {
	originalHash := n.ContentHash

	cls, err := s.classify(ctx, n, projects, tagCounts)
	if err != nil {
		return fmt.Errorf("classify: %w", err)
	}

	// Content hash guard: re-read the note and verify nothing changed
	// during the LLM call.
	fresh, err := s.notes.Get(ctx, userID, n.ID)
	if err != nil {
		return fmt.Errorf("re-read note: %w", err)
	}
	if fresh.ContentHash != originalHash {
		s.logger.Info("librarian: note changed during classification, skipping",
			"note_id", n.ID, "title", n.Title)
		return nil
	}

	req := s.buildUpdate(n, cls)
	if req == nil {
		// No classification changes, but still mark as reviewed.
		reviewed := mergeUnique(n.Tags, []string{"librarian:reviewed"})
		req = &note.UpdateNoteReq{Tags: &reviewed}
	}

	updated, err := s.notes.Update(ctx, userID, n.ID, *req)
	if err != nil {
		return fmt.Errorf("update note: %w", err)
	}

	s.publish(userID, updated, req)
	return nil
}

// classify sends note content to the LLM for project/tag classification.
func (s *Service) classify(
	ctx context.Context,
	n *note.Note,
	projects []*project.Project,
	tagCounts []note.TagCount,
) (*classification, error) {
	// Build project list for the prompt.
	var projectLines []string
	for _, p := range projects {
		desc := p.Description
		if desc == "" {
			desc = "(no description)"
		}
		projectLines = append(projectLines, fmt.Sprintf("- ID: %s | Name: %s | Description: %s", p.ID, p.Name, desc))
	}

	// Build tag list for the prompt.
	var tagNames []string
	for _, tc := range tagCounts {
		tagNames = append(tagNames, tc.Name)
	}

	// Truncate body to 3000 runes.
	body := n.Body
	if utf8.RuneCountInString(body) > 3000 {
		body = string([]rune(body)[:3000])
	}

	systemPrompt := `You are a librarian for a personal knowledge base. Your job is to classify notes into the correct project and suggest relevant tags. You must ONLY use projects and tags from the provided lists -- never invent new ones. If no project fits, respond with an empty project_id. If no tags fit, respond with an empty tags array. Be conservative: only assign when there is a clear match.

Respond with a single JSON object:
{"project_id": "...", "tags": ["..."], "rationale": "..."}`

	userPrompt := fmt.Sprintf(`Classify this note:

Title: %s
Body:
%s

Review type: %s

Available projects:
%s

Available tags: %s

Respond with JSON only.`,
		n.Title,
		body,
		inferReviewType(n),
		strings.Join(projectLines, "\n"),
		strings.Join(tagNames, ", "),
	)

	messages := []ai.ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	resp, err := s.chat.ChatCompletion(ctx, s.model, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM call: %w", err)
	}

	cls, err := parseClassification(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("parse LLM response: %w", err)
	}

	// Validate: filter to only known project IDs and tag names.
	cls = validateClassification(cls, projects, tagCounts)
	return cls, nil
}

// buildUpdate translates a classification into an UpdateNoteReq.
// Returns nil if the only change would be adding librarian:reviewed.
func (s *Service) buildUpdate(
	n *note.Note,
	cls *classification,
) *note.UpdateNoteReq {
	var req note.UpdateNoteReq
	hasChanges := false

	// Only assign a project if the note currently has none.
	if n.ProjectID == "" && cls.ProjectID != "" {
		req.ProjectID = &cls.ProjectID
		hasChanges = true
	}

	// Merge tags: existing + LLM-suggested + librarian:reviewed.
	newTags := mergeUnique(n.Tags, cls.Tags)
	newTags = mergeUnique(newTags, []string{"librarian:reviewed"})

	if !tagsEqual(n.Tags, newTags) {
		req.Tags = &newTags
		hasChanges = true
	}

	if !hasChanges {
		return nil
	}

	// Always ensure librarian:reviewed is in the final tag set.
	if req.Tags == nil {
		tags := mergeUnique(n.Tags, []string{"librarian:reviewed"})
		req.Tags = &tags
	}

	return &req
}

// publish sends a WebSocket notification about the librarian's action.
func (s *Service) publish(userID string, n *note.Note, req *note.UpdateNoteReq) {
	if s.hub == nil || n == nil {
		return
	}

	var actions []string
	if req.ProjectID != nil {
		actions = append(actions, "assigned project")
	}
	if req.Tags != nil {
		actions = append(actions, "updated tags")
	}

	payload, err := json.Marshal(ws.LibrarianActionPayload{
		NoteID:    n.ID,
		NoteTitle: n.Title,
		Actions:   actions,
	})
	if err != nil {
		s.logger.Debug("librarian: marshal ws payload", "error", err)
		return
	}
	_ = s.hub.Send(userID, ws.Message{
		Type:    ws.MsgTypeLibrarianAction,
		Payload: payload,
	})
}

// inferReviewType guesses the review category from note state.
func inferReviewType(n *note.Note) string {
	if n.ProjectID == "" && len(n.Tags) == 0 {
		return "inbox, untagged"
	}
	if n.ProjectID == "" {
		return "inbox"
	}
	if len(n.Tags) == 0 {
		return "untagged"
	}
	return "orphan"
}

// jsonObjectRe matches a JSON object in LLM output.
var jsonObjectRe = regexp.MustCompile(`\{[\s\S]*\}`)

// parseClassification extracts a classification from LLM output using
// a multi-strategy approach for robustness.
func parseClassification(content string) (*classification, error) {
	content = strings.TrimSpace(content)

	var cls classification

	// Strategy 1: direct unmarshal.
	if err := json.Unmarshal([]byte(content), &cls); err == nil {
		return &cls, nil
	}

	// Strategy 2: bracket-based extraction (handles markdown fences).
	if idx := strings.Index(content, "{"); idx >= 0 {
		if end := strings.LastIndex(content, "}"); end > idx {
			extracted := content[idx : end+1]
			if err := json.Unmarshal([]byte(extracted), &cls); err == nil {
				return &cls, nil
			}
		}
	}

	// Strategy 3: regex extraction.
	if match := jsonObjectRe.FindString(content); match != "" {
		if err := json.Unmarshal([]byte(match), &cls); err == nil {
			return &cls, nil
		}
	}

	return nil, fmt.Errorf("no valid JSON object found in LLM response")
}

// validateClassification filters the classification to only contain
// known project IDs and tag names.
func validateClassification(
	cls *classification,
	projects []*project.Project,
	tagCounts []note.TagCount,
) *classification {
	// Validate project ID.
	if cls.ProjectID != "" {
		found := false
		for _, p := range projects {
			if p.ID == cls.ProjectID {
				found = true
				break
			}
		}
		if !found {
			cls.ProjectID = ""
		}
	}

	// Validate tags (case-insensitive).
	knownTags := make(map[string]string, len(tagCounts))
	for _, tc := range tagCounts {
		knownTags[strings.ToLower(tc.Name)] = tc.Name
	}
	var validTags []string
	for _, t := range cls.Tags {
		if canonical, ok := knownTags[strings.ToLower(t)]; ok {
			validTags = append(validTags, canonical)
		}
	}
	cls.Tags = validTags

	return cls
}

// hasTag checks if a tag exists in a slice (case-insensitive).
func hasTag(tags []string, target string) bool {
	lower := strings.ToLower(target)
	for _, t := range tags {
		if strings.ToLower(t) == lower {
			return true
		}
	}
	return false
}

// mergeUnique returns the union of two string slices, preserving order.
// Deduplication is case-insensitive to match validateClassification.
func mergeUnique(existing, additions []string) []string {
	seen := make(map[string]bool, len(existing))
	result := make([]string, 0, len(existing)+len(additions))
	for _, s := range existing {
		lower := strings.ToLower(s)
		if !seen[lower] {
			seen[lower] = true
			result = append(result, s)
		}
	}
	for _, s := range additions {
		lower := strings.ToLower(s)
		if !seen[lower] {
			seen[lower] = true
			result = append(result, s)
		}
	}
	return result
}

// tagsEqual reports whether two tag slices contain the same elements
// regardless of order. Comparison is case-insensitive to match
// mergeUnique and validateClassification.
func tagsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]int, len(a))
	for _, s := range a {
		set[strings.ToLower(s)]++
	}
	for _, s := range b {
		set[strings.ToLower(s)]--
		if set[strings.ToLower(s)] < 0 {
			return false
		}
	}
	return true
}
