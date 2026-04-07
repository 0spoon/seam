// Package briefing assembles a daily summary note that surfaces recent
// notes, open tasks, and suggestions. The briefing runs as a registered
// scheduler action and produces (or updates) a note in a configurable
// briefings project.
package briefing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/task"
	"github.com/katata/seam/internal/userdb"
	"github.com/katata/seam/internal/ws"
)

// Default values for briefing config.
const (
	DefaultProjectName = "briefings"
	DefaultProjectSlug = "briefings"
)

// Config bundles dependencies the briefing service needs from the rest of
// the application. All fields are required except Hub (optional).
type Config struct {
	NoteService    NoteService
	ProjectService ProjectService
	TaskService    TaskService
	DBManager      userdb.Manager
	Hub            *ws.Hub
	Logger         *slog.Logger
}

// NoteService captures the subset of note.Service methods we use.
type NoteService interface {
	List(ctx context.Context, userID string, filter note.NoteFilter) ([]*note.Note, int, error)
	Create(ctx context.Context, userID string, req note.CreateNoteReq) (*note.Note, error)
	Update(ctx context.Context, userID, noteID string, req note.UpdateNoteReq) (*note.Note, error)
}

// ProjectService captures the subset of project.Service methods we use.
type ProjectService interface {
	GetBySlug(ctx context.Context, userID, slug string) (*project.Project, error)
	Create(ctx context.Context, userID, name, description string) (*project.Project, error)
}

// TaskService captures the subset of task.Service methods we use.
type TaskService interface {
	List(ctx context.Context, userID string, filter task.TaskFilter) ([]*task.Task, int, error)
}

// Service produces daily briefings.
type Service struct {
	notes     NoteService
	projects  ProjectService
	tasks     TaskService
	dbManager userdb.Manager
	hub       *ws.Hub
	logger    *slog.Logger
}

// NewService creates a briefing service.
func NewService(cfg Config) *Service {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Service{
		notes:     cfg.NoteService,
		projects:  cfg.ProjectService,
		tasks:     cfg.TaskService,
		dbManager: cfg.DBManager,
		hub:       cfg.Hub,
		logger:    cfg.Logger,
	}
}

// ActionConfig is the JSON payload stored on a briefing schedule. All
// fields are optional and fall back to sensible defaults.
type ActionConfig struct {
	// ProjectSlug controls where the briefing note is created. Defaults
	// to "briefings". The project is auto-created on first run.
	ProjectSlug string `json:"project_slug,omitempty"`

	// LookbackHours bounds "recent activity" queries. Defaults to 24.
	LookbackHours int `json:"lookback_hours,omitempty"`

	// MaxNotes caps how many recent notes are listed. Defaults to 10.
	MaxNotes int `json:"max_notes,omitempty"`

	// MaxTasks caps how many open/overdue tasks are listed. Defaults to 20.
	MaxTasks int `json:"max_tasks,omitempty"`
}

func (c *ActionConfig) applyDefaults() {
	if strings.TrimSpace(c.ProjectSlug) == "" {
		c.ProjectSlug = DefaultProjectSlug
	}
	if c.LookbackHours <= 0 {
		c.LookbackHours = 24
	}
	if c.MaxNotes <= 0 {
		c.MaxNotes = 10
	}
	if c.MaxTasks <= 0 {
		c.MaxTasks = 20
	}
}

// Generate builds a briefing note for the given user using the supplied
// raw config. Returns the created note. Errors from sub-queries (e.g.
// listing tasks) are tolerated -- the briefing degrades gracefully and
// reports the failure inline rather than aborting the whole job.
func (s *Service) Generate(ctx context.Context, userID string, rawConfig json.RawMessage) (*note.Note, error) {
	var cfg ActionConfig
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("briefing.Service.Generate: parse config: %w", err)
		}
	}
	cfg.applyDefaults()

	now := time.Now().UTC()
	since := now.Add(-time.Duration(cfg.LookbackHours) * time.Hour)

	data := s.collect(ctx, userID, cfg, now, since)

	body := renderBriefing(data, now, cfg.LookbackHours)
	title := fmt.Sprintf("%s Daily Briefing", now.Format("2006-01-02"))

	proj, err := s.ensureProject(ctx, userID, cfg.ProjectSlug)
	if err != nil {
		return nil, fmt.Errorf("briefing.Service.Generate: ensure project: %w", err)
	}

	// Dedupe by title within today's project. If the cron and a manual
	// "run now" both fire on the same day, the second invocation
	// overwrites the existing note instead of producing a duplicate.
	if existing := s.findExistingForToday(ctx, userID, proj.ID, title); existing != nil {
		updated, updErr := s.notes.Update(ctx, userID, existing.ID, note.UpdateNoteReq{
			Body: &body,
		})
		if updErr != nil {
			return nil, fmt.Errorf("briefing.Service.Generate: update note: %w", updErr)
		}
		s.publish(userID, updated)
		s.logger.Info("briefing updated",
			"user_id", userID, "note_id", updated.ID, "title", updated.Title)
		return updated, nil
	}

	created, err := s.notes.Create(ctx, userID, note.CreateNoteReq{
		Title:     title,
		Body:      body,
		ProjectID: proj.ID,
		Tags:      []string{"briefing", "daily"},
	})
	if err != nil {
		return nil, fmt.Errorf("briefing.Service.Generate: create note: %w", err)
	}

	s.publish(userID, created)
	s.logger.Info("briefing generated",
		"user_id", userID, "note_id", created.ID, "title", created.Title)
	return created, nil
}

// findExistingForToday looks for a briefing note in the configured
// project that already has today's title. Returns nil on any error or
// when no match is found -- the caller falls back to creating a fresh
// note, so this lookup must never block briefing generation.
func (s *Service) findExistingForToday(ctx context.Context, userID, projectID, title string) *note.Note {
	notes, _, err := s.notes.List(ctx, userID, note.NoteFilter{
		ProjectID:   projectID,
		Limit:       50,
		ExcludeBody: true,
	})
	if err != nil {
		s.logger.Debug("briefing.Service.findExistingForToday: list failed",
			"user_id", userID, "error", err)
		return nil
	}
	for _, n := range notes {
		if n.Title == title {
			return n
		}
	}
	return nil
}

// Action returns a scheduler.ActionRunner adapter that calls Generate.
// Wired in seamd via scheduler.Service.RegisterRunner.
func (s *Service) Action() func(ctx context.Context, userID string, config json.RawMessage) error {
	return func(ctx context.Context, userID string, config json.RawMessage) error {
		_, err := s.Generate(ctx, userID, config)
		return err
	}
}

// briefingData collects everything needed to render a briefing.
type briefingData struct {
	RecentNotes  []*note.Note
	NoteCount    int
	OpenTasks    []*task.Task
	OpenCount    int
	RecentErrors []string
}

func (s *Service) collect(ctx context.Context, userID string, cfg ActionConfig, now, since time.Time) briefingData {
	var data briefingData

	notes, total, err := s.notes.List(ctx, userID, note.NoteFilter{
		Since:       since,
		Until:       now,
		Sort:        "modified",
		SortDir:     "desc",
		Limit:       cfg.MaxNotes,
		ExcludeBody: true,
	})
	if err != nil {
		s.logger.Warn("briefing.Service.collect: list notes failed",
			"user_id", userID, "error", err)
		data.RecentErrors = append(data.RecentErrors, "Recent notes unavailable.")
	} else {
		data.RecentNotes = notes
		data.NoteCount = total
	}

	openOnly := false // task filter Done is *bool: nil = all, false = open only.
	tasks, openTotal, err := s.tasks.List(ctx, userID, task.TaskFilter{
		Done:  &openOnly,
		Limit: cfg.MaxTasks,
	})
	if err != nil {
		s.logger.Warn("briefing.Service.collect: list tasks failed",
			"user_id", userID, "error", err)
		data.RecentErrors = append(data.RecentErrors, "Open tasks unavailable.")
	} else {
		data.OpenTasks = tasks
		data.OpenCount = openTotal
	}

	return data
}

// ensureProject returns the briefings project, creating it on first use.
func (s *Service) ensureProject(ctx context.Context, userID, slug string) (*project.Project, error) {
	if slug == "" {
		slug = DefaultProjectSlug
	}
	if s.projects == nil {
		return nil, errors.New("project service not configured")
	}

	existing, err := s.projects.GetBySlug(ctx, userID, slug)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, project.ErrNotFound) {
		// Some other lookup error -- still try to create as a recovery path.
		s.logger.Warn("briefing.Service.ensureProject: lookup error, attempting create",
			"slug", slug, "error", err)
	}

	name := DefaultProjectName
	if slug != DefaultProjectSlug {
		name = slug
	}
	created, createErr := s.projects.Create(ctx, userID, name, "Auto-generated daily briefings.")
	if createErr != nil {
		return nil, createErr
	}
	return created, nil
}

// publish pushes a websocket notification announcing the new briefing
// note. Best effort: a missing hub or marshal failure is logged and
// swallowed so a silent client never blocks the briefing pipeline.
func (s *Service) publish(userID string, n *note.Note) {
	if s.hub == nil || n == nil {
		return
	}
	payload, err := json.Marshal(ws.NoteChangedPayload{
		NoteID:     n.ID,
		ChangeType: "created",
	})
	if err != nil {
		s.logger.Debug("briefing.Service.publish: marshal payload",
			"note_id", n.ID, "error", err)
		return
	}
	_ = s.hub.Send(userID, ws.Message{
		Type:    ws.MsgTypeNoteChanged,
		Payload: payload,
	})
}

// renderBriefing produces the markdown body of the briefing note. Pure
// function so it's trivial to unit test.
//
// All timestamps are rendered in UTC for consistency. Mixing UTC for
// the header and server-local time for the per-note line caused notes
// from before/after midnight UTC to look like they belonged to
// different days. If timezone-aware rendering is needed later, both
// the header and per-note timestamps should convert to the same zone.
func renderBriefing(data briefingData, now time.Time, lookbackHours int) string {
	var b strings.Builder

	nowUTC := now.UTC()
	fmt.Fprintf(&b, "# Daily Briefing -- %s\n\n", nowUTC.Format("Monday, January 2 2006"))
	fmt.Fprintf(&b, "_Generated %s. Looking back %d hours._\n\n",
		nowUTC.Format("15:04 MST"), lookbackHours)

	if len(data.RecentErrors) > 0 {
		b.WriteString("> Note: ")
		b.WriteString(strings.Join(data.RecentErrors, " "))
		b.WriteString("\n\n")
	}

	// Recent notes section.
	b.WriteString("## Recent activity\n\n")
	if len(data.RecentNotes) == 0 {
		b.WriteString("_No notes created or modified in this window._\n\n")
	} else {
		fmt.Fprintf(&b, "%d note(s) updated since %s.\n\n",
			data.NoteCount, nowUTC.Add(-time.Duration(lookbackHours)*time.Hour).Format("15:04 MST"))
		for _, n := range data.RecentNotes {
			ts := n.UpdatedAt.UTC().Format("15:04")
			fmt.Fprintf(&b, "- [[%s]] _(updated %s)_\n", n.Title, ts)
		}
		b.WriteString("\n")
	}

	// Open tasks section, grouped by note.
	b.WriteString("## Open tasks\n\n")
	if len(data.OpenTasks) == 0 {
		b.WriteString("_No open tasks._\n\n")
	} else {
		fmt.Fprintf(&b, "%d open task(s).\n\n", data.OpenCount)
		grouped := groupTasksByNote(data.OpenTasks)
		// Stable order: notes with the most recent task update first.
		// Tie-break on noteID so the rendered output is deterministic
		// even when groupTasksByNote returns groups in a different
		// (map iteration) order across runs.
		sort.SliceStable(grouped, func(i, j int) bool {
			if !grouped[i].mostRecent.Equal(grouped[j].mostRecent) {
				return grouped[i].mostRecent.After(grouped[j].mostRecent)
			}
			return grouped[i].noteID < grouped[j].noteID
		})
		for _, g := range grouped {
			fmt.Fprintf(&b, "### Note %s\n", shortNoteRef(g.noteID))
			for _, t := range g.tasks {
				b.WriteString("- [ ] ")
				b.WriteString(t.Content)
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}

	// Suggested actions: a small static set tied to what's in the briefing.
	b.WriteString("## Suggested actions\n\n")
	suggestions := suggestActions(data)
	if len(suggestions) == 0 {
		b.WriteString("_Nothing flagged for today._\n")
	} else {
		for _, s := range suggestions {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}

	return b.String()
}

type taskGroup struct {
	noteID     string
	tasks      []*task.Task
	mostRecent time.Time
}

func groupTasksByNote(tasks []*task.Task) []taskGroup {
	byNote := make(map[string]*taskGroup)
	var order []string
	for _, t := range tasks {
		g, ok := byNote[t.NoteID]
		if !ok {
			g = &taskGroup{noteID: t.NoteID}
			byNote[t.NoteID] = g
			order = append(order, t.NoteID)
		}
		g.tasks = append(g.tasks, t)
		if t.UpdatedAt.After(g.mostRecent) {
			g.mostRecent = t.UpdatedAt
		}
	}
	// Iterate `order` (insertion order) instead of the map so the
	// output is deterministic regardless of map randomization. Final
	// ordering is still applied by the caller's sort.
	out := make([]taskGroup, 0, len(byNote))
	for _, id := range order {
		out = append(out, *byNote[id])
	}
	return out
}

// shortNoteRef returns a brief identifier for a note used in the briefing
// when the note title is not loaded. Falls back to the ULID prefix.
func shortNoteRef(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// suggestActions produces a handful of textual nudges based on what the
// collector found. Cheap heuristics; no LLM call.
func suggestActions(data briefingData) []string {
	var out []string
	if data.OpenCount > 5 {
		out = append(out,
			fmt.Sprintf("You have %d open tasks -- consider triaging the oldest before adding more.", data.OpenCount))
	}
	if len(data.RecentNotes) == 0 && data.NoteCount == 0 {
		out = append(out, "No notes touched recently. Capture a quick log entry to keep momentum.")
	}
	if data.NoteCount > 20 {
		out = append(out,
			fmt.Sprintf("%d notes were updated -- a synthesis note may help connect them.", data.NoteCount))
	}
	return out
}
