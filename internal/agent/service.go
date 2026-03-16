package agent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"

	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/search"
	"github.com/katata/seam/internal/userdb"
)

// NoteCreator abstracts note.Service for agent use.
type NoteCreator interface {
	Create(ctx context.Context, userID string, req note.CreateNoteReq) (*note.Note, error)
	Update(ctx context.Context, userID, noteID string, req note.UpdateNoteReq) (*note.Note, error)
	Get(ctx context.Context, userID, noteID string) (*note.Note, error)
	List(ctx context.Context, userID string, filter note.NoteFilter) ([]*note.Note, int, error)
	Delete(ctx context.Context, userID, noteID string) error
	AppendToNote(ctx context.Context, userID, noteID, text string) (*note.Note, error)
}

// ProjectCreator abstracts project.Service for auto-creating the agent-memory project.
type ProjectCreator interface {
	Create(ctx context.Context, userID, name, description string) (*project.Project, error)
	List(ctx context.Context, userID string) ([]*project.Project, error)
	GetBySlug(ctx context.Context, userID, slug string) (*project.Project, error)
}

// Searcher abstracts search across notes.
type Searcher interface {
	SearchFTS(ctx context.Context, userID, query string, limit, offset int) ([]search.FTSResult, int, error)
	SearchSemantic(ctx context.Context, userID, query string, limit int) ([]search.SemanticResult, error)
	SearchFTSScoped(ctx context.Context, userID, query string, limit, offset int, includeProjectID, excludeProjectID string) ([]search.FTSResult, int, error)
	SearchSemanticScoped(ctx context.Context, userID, query string, limit int, where map[string]interface{}) ([]search.SemanticResult, error)
	SearchSemanticScopedWithRecency(ctx context.Context, userID, query string, limit int, where map[string]interface{}, recencyBias float64) ([]search.SemanticResult, error)
	SearchFTSScopedWithRecency(ctx context.Context, userID, query string, limit, offset int, includeProjectID, excludeProjectID string, recencyBias float64) ([]search.FTSResult, int, error)
	SearchFTSWithRecency(ctx context.Context, userID, query string, limit, offset int, recencyBias float64) ([]search.FTSResult, int, error)
}

// WSNotifier abstracts WebSocket event delivery.
type WSNotifier interface {
	SendAgentEvent(userID string, eventType string, payload interface{})
}

// ServiceConfig holds dependencies for the agent Service.
type ServiceConfig struct {
	Store          Store
	NoteService    NoteCreator
	ProjectService ProjectCreator
	SearchService  Searcher
	AIQueue        *ai.Queue  // may be nil if AI disabled; used to enqueue embed tasks
	WSNotifier     WSNotifier // may be nil; used to push agent events via WebSocket
	UserDBManager  userdb.Manager
	Logger         *slog.Logger
}

// Service implements agent business logic: session lifecycle, memory CRUD,
// and context gathering.
type Service struct {
	cfg ServiceConfig

	// projectCache caches the agent-memory project ID per user.
	projectCacheMu sync.RWMutex
	projectCache   map[string]string // userID -> projectID
}

// NewService creates a new agent Service.
func NewService(cfg ServiceConfig) *Service {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Service{
		cfg:          cfg,
		projectCache: make(map[string]string),
	}
}

// ensureAgentMemoryProject ensures the "agent-memory" project exists for the user.
// Returns the project ID, caching the result.
func (s *Service) ensureAgentMemoryProject(ctx context.Context, userID string) (string, error) {
	// Check cache first.
	s.projectCacheMu.RLock()
	if id, ok := s.projectCache[userID]; ok {
		s.projectCacheMu.RUnlock()
		return id, nil
	}
	s.projectCacheMu.RUnlock()

	// Try to find existing project.
	p, err := s.cfg.ProjectService.GetBySlug(ctx, userID, AgentMemoryProject)
	if err == nil {
		s.projectCacheMu.Lock()
		s.projectCache[userID] = p.ID
		s.projectCacheMu.Unlock()
		return p.ID, nil
	}

	if !errors.Is(err, project.ErrNotFound) {
		return "", fmt.Errorf("agent.Service.ensureAgentMemoryProject: %w", err)
	}

	// Create the project.
	p, err = s.cfg.ProjectService.Create(ctx, userID, "agent-memory", "Agent memory storage")
	if err != nil {
		// Handle race condition: another goroutine may have created it.
		if errors.Is(err, project.ErrSlugExists) {
			p, err = s.cfg.ProjectService.GetBySlug(ctx, userID, AgentMemoryProject)
			if err != nil {
				return "", fmt.Errorf("agent.Service.ensureAgentMemoryProject: get after race: %w", err)
			}
		} else {
			return "", fmt.Errorf("agent.Service.ensureAgentMemoryProject: create: %w", err)
		}
	}

	s.projectCacheMu.Lock()
	s.projectCache[userID] = p.ID
	s.projectCacheMu.Unlock()
	return p.ID, nil
}

// --- Session Lifecycle ---

// SessionStart creates a new session or resumes an existing one.
// Returns a Briefing with context assembled within the character budget.
func (s *Service) SessionStart(ctx context.Context, userID, name string, maxContextChars int) (*Briefing, error) {
	if err := ValidateSessionName(name); err != nil {
		return nil, err
	}

	if maxContextChars <= 0 {
		maxContextChars = DefaultMaxContextChars
	}

	// Ensure agent-memory project exists.
	if _, err := s.ensureAgentMemoryProject(ctx, userID); err != nil {
		return nil, fmt.Errorf("agent.Service.SessionStart: %w", err)
	}

	db, err := s.cfg.UserDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("agent.Service.SessionStart: open db: %w", err)
	}

	// Check if session already exists (resume case).
	existing, getErr := s.cfg.Store.GetSessionByName(ctx, db, name)
	if getErr == nil {
		// Resume existing session. If session is not active, the briefing still
		// returns the data (status field indicates current state). The agent can
		// decide whether to act on a completed/archived session.
		if existing.Status != StatusActive {
			s.cfg.Logger.Info("resuming non-active session",
				"session", name, "status", existing.Status)
		}
		return s.assembleBriefing(ctx, userID, db, existing, maxContextChars)
	}
	if !errors.Is(getErr, ErrNotFound) {
		return nil, fmt.Errorf("agent.Service.SessionStart: get by name: %w", getErr)
	}

	// Create new session.
	now := time.Now().UTC()
	idVal, idErr := ulid.New(ulid.Now(), rand.Reader)
	if idErr != nil {
		return nil, fmt.Errorf("agent.Service.SessionStart: generate id: %w", idErr)
	}
	sess := &Session{
		ID:        idVal.String(),
		Name:      name,
		Status:    StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Resolve parent session from "/" naming convention.
	if parentName, ok := ParentSessionName(name); ok {
		parent, parentErr := s.cfg.Store.GetSessionByName(ctx, db, parentName)
		if parentErr == nil {
			sess.ParentSessionID = parent.ID
		}
		// If parent not found, that is fine -- subagent started before parent.
	}

	if err := s.cfg.Store.CreateSession(ctx, db, sess); err != nil {
		return nil, fmt.Errorf("agent.Service.SessionStart: create: %w", err)
	}

	// Reconcile orphan children (sessions that started before this parent).
	if _, err := s.cfg.Store.ReconcileChildren(ctx, db, sess.ID, name); err != nil {
		s.cfg.Logger.Warn("agent.Service.SessionStart: reconcile children failed",
			"session", name, "error", err)
	}

	s.notifyWS(userID, "agent.session_started", map[string]string{
		"session_name": name,
		"session_id":   sess.ID,
	})

	return s.assembleBriefing(ctx, userID, db, sess, maxContextChars)
}

// SessionEnd completes a session with findings.
func (s *Service) SessionEnd(ctx context.Context, userID, sessionName, findings string) error {
	if findings == "" {
		return fmt.Errorf("agent.Service.SessionEnd: %w", ErrFindingsRequired)
	}
	if utf8.RuneCountInString(findings) > MaxFindingsChars {
		return fmt.Errorf("agent.Service.SessionEnd: %d chars exceeds %d: %w",
			utf8.RuneCountInString(findings), MaxFindingsChars, ErrFindingsTooLong)
	}

	db, err := s.cfg.UserDBManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("agent.Service.SessionEnd: open db: %w", err)
	}

	sess, err := s.cfg.Store.GetSessionByName(ctx, db, sessionName)
	if err != nil {
		return fmt.Errorf("agent.Service.SessionEnd: %w", err)
	}

	if sess.Status != StatusActive {
		return fmt.Errorf("agent.Service.SessionEnd: session %q is %s: %w",
			sessionName, sess.Status, ErrSessionNotActive)
	}

	sess.Status = StatusCompleted
	sess.Findings = findings
	sess.UpdatedAt = time.Now().UTC()

	if err := s.cfg.Store.UpdateSession(ctx, db, sess); err != nil {
		return fmt.Errorf("agent.Service.SessionEnd: %w", err)
	}

	// Update session note tags from status:active to status:completed.
	s.updateSessionNoteStatus(ctx, userID, sessionName, "completed")

	s.cfg.Logger.Info("session ended", "user_id", userID, "session", sessionName)

	s.notifyWS(userID, "agent.session_ended", map[string]string{
		"session_name": sessionName,
		"status":       StatusCompleted,
	})

	return nil
}

// SessionList returns sessions filtered by status.
func (s *Service) SessionList(ctx context.Context, userID, status string, limit int) ([]*Session, error) {
	db, err := s.cfg.UserDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("agent.Service.SessionList: open db: %w", err)
	}

	sessions, err := s.cfg.Store.ListSessions(ctx, db, status, limit, 0)
	if err != nil {
		return nil, fmt.Errorf("agent.Service.SessionList: %w", err)
	}
	return sessions, nil
}

// --- Session Notes (Plan, Progress, Context) ---

// SessionPlanSet creates or updates the plan note for a session.
func (s *Service) SessionPlanSet(ctx context.Context, userID, sessionName, content string) (string, error) {
	return s.upsertSessionNote(ctx, userID, sessionName, "plan", PlanNoteTitle(sessionName), content)
}

// SessionProgressUpdate appends a progress entry to the session progress note.
func (s *Service) SessionProgressUpdate(ctx context.Context, userID, sessionName, task, status, notes string) (string, error) {
	title := ProgressNoteTitle(sessionName)
	tags := SessionTags(sessionName, "progress", "active")

	// Find existing progress note.
	noteID, err := s.findSessionNote(ctx, userID, sessionName, "progress")
	if err != nil && !errors.Is(err, ErrNotFound) {
		return "", fmt.Errorf("agent.Service.SessionProgressUpdate: find: %w", err)
	}
	if err != nil {
		// Create the note (ErrNotFound).
		projectID, projErr := s.ensureAgentMemoryProject(ctx, userID)
		if projErr != nil {
			return "", fmt.Errorf("agent.Service.SessionProgressUpdate: %w", projErr)
		}

		entry := formatProgressEntry(task, status, notes)
		n, createErr := s.cfg.NoteService.Create(ctx, userID, note.CreateNoteReq{
			Title:     title,
			Body:      entry,
			ProjectID: projectID,
			Tags:      tags,
		})
		if createErr != nil {
			return "", fmt.Errorf("agent.Service.SessionProgressUpdate: create: %w", createErr)
		}
		s.enqueueEmbed(ctx, userID, n.ID)
		return n.ID, nil
	}

	// Append to existing progress note using AppendToNote for timestamped format.
	entry := fmt.Sprintf("[%s] %s", status, task)
	if notes != "" {
		entry += ": " + notes
	}
	_, appendErr := s.cfg.NoteService.AppendToNote(ctx, userID, noteID, entry)
	if appendErr != nil {
		return "", fmt.Errorf("agent.Service.SessionProgressUpdate: append: %w", appendErr)
	}
	return noteID, nil
}

// SessionContextSet creates or updates the context note for a session.
func (s *Service) SessionContextSet(ctx context.Context, userID, sessionName, content string) (string, error) {
	return s.upsertSessionNote(ctx, userID, sessionName, "context", ContextNoteTitle(sessionName), content)
}

// --- Knowledge (Memory) CRUD ---

// MemoryWrite creates or updates a knowledge note.
func (s *Service) MemoryWrite(ctx context.Context, userID, category, name, content string) (string, error) {
	title := KnowledgeNoteTitle(category, name)
	tags := KnowledgeTags(category)

	// Try to find existing.
	noteID, err := s.findKnowledgeNote(ctx, userID, category, name)
	if err == nil {
		// Update existing.
		body := content
		if _, updateErr := s.cfg.NoteService.Update(ctx, userID, noteID, note.UpdateNoteReq{
			Body: &body,
		}); updateErr != nil {
			return "", fmt.Errorf("agent.Service.MemoryWrite: update: %w", updateErr)
		}
		s.enqueueEmbed(ctx, userID, noteID)
		s.notifyWS(userID, "agent.memory_changed", map[string]string{
			"action":   "update",
			"category": category,
			"name":     name,
		})
		return noteID, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return "", fmt.Errorf("agent.Service.MemoryWrite: find: %w", err)
	}

	// Create new.
	projectID, projErr := s.ensureAgentMemoryProject(ctx, userID)
	if projErr != nil {
		return "", fmt.Errorf("agent.Service.MemoryWrite: %w", projErr)
	}

	n, createErr := s.cfg.NoteService.Create(ctx, userID, note.CreateNoteReq{
		Title:     title,
		Body:      content,
		ProjectID: projectID,
		Tags:      tags,
	})
	if createErr != nil {
		return "", fmt.Errorf("agent.Service.MemoryWrite: create: %w", createErr)
	}
	s.enqueueEmbed(ctx, userID, n.ID)

	s.notifyWS(userID, "agent.memory_changed", map[string]string{
		"action":   "write",
		"category": category,
		"name":     name,
	})

	return n.ID, nil
}

// MemoryRead reads a knowledge note by category and name.
func (s *Service) MemoryRead(ctx context.Context, userID, category, name string) (string, string, error) {
	noteID, err := s.findKnowledgeNote(ctx, userID, category, name)
	if err != nil {
		return "", "", err
	}

	n, err := s.cfg.NoteService.Get(ctx, userID, noteID)
	if err != nil {
		return "", "", fmt.Errorf("agent.Service.MemoryRead: %w", err)
	}
	return n.Title, n.Body, nil
}

// MemoryAppend appends content to an existing knowledge note.
// Unlike session progress, this does NOT use AppendToNote (no timestamp).
func (s *Service) MemoryAppend(ctx context.Context, userID, category, name, content string) error {
	noteID, err := s.findKnowledgeNote(ctx, userID, category, name)
	if err != nil {
		return err
	}

	n, err := s.cfg.NoteService.Get(ctx, userID, noteID)
	if err != nil {
		return fmt.Errorf("agent.Service.MemoryAppend: get: %w", err)
	}

	// Ensure separator between existing content and appended text.
	newBody := n.Body
	if newBody != "" && !strings.HasSuffix(newBody, "\n") {
		newBody += "\n"
	}
	newBody += content
	if _, err := s.cfg.NoteService.Update(ctx, userID, noteID, note.UpdateNoteReq{
		Body: &newBody,
	}); err != nil {
		return fmt.Errorf("agent.Service.MemoryAppend: update: %w", err)
	}
	s.enqueueEmbed(ctx, userID, noteID)
	return nil
}

// MemoryList lists knowledge notes, optionally filtered by category.
func (s *Service) MemoryList(ctx context.Context, userID, category string) ([]MemoryItem, error) {
	projectID, err := s.ensureAgentMemoryProject(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("agent.Service.MemoryList: %w", err)
	}

	// When category is provided, filter by domain tag. The type:knowledge tag
	// is also present on all knowledge notes but NoteFilter.Tag accepts only
	// a single tag. Using domain:{category} is more specific and sufficient
	// since only knowledge notes have domain: tags.
	tag := "type:knowledge"
	if category != "" {
		tag = "domain:" + category
	}

	notes, _, err := s.cfg.NoteService.List(ctx, userID, note.NoteFilter{
		ProjectID: projectID,
		Tag:       tag,
		Limit:     100,
	})
	if err != nil {
		return nil, fmt.Errorf("agent.Service.MemoryList: %w", err)
	}

	items := make([]MemoryItem, 0, len(notes))
	for _, n := range notes {
		cat, name := parseKnowledgeTitle(n.Title)
		items = append(items, MemoryItem{
			Category:  cat,
			Name:      name,
			Title:     n.Title,
			UpdatedAt: n.UpdatedAt,
		})
	}
	return items, nil
}

// MemoryDelete deletes a knowledge note.
func (s *Service) MemoryDelete(ctx context.Context, userID, category, name string) error {
	noteID, err := s.findKnowledgeNote(ctx, userID, category, name)
	if err != nil {
		return err
	}

	if err := s.cfg.NoteService.Delete(ctx, userID, noteID); err != nil {
		return fmt.Errorf("agent.Service.MemoryDelete: %w", err)
	}
	s.enqueueDeleteEmbed(ctx, userID, noteID)

	s.notifyWS(userID, "agent.memory_changed", map[string]string{
		"action":   "delete",
		"category": category,
		"name":     name,
	})

	return nil
}

// --- User Note Access ---

// NotesSearch performs full-text search across user notes.
// When recencyBias > 0, results are re-ranked with recency weighting.
func (s *Service) NotesSearch(ctx context.Context, userID, query string, limit int, recencyBias float64) ([]search.FTSResult, error) {
	if s.cfg.SearchService == nil {
		return nil, fmt.Errorf("agent.Service.NotesSearch: search not configured")
	}
	var results []search.FTSResult
	var err error
	if recencyBias > 0 {
		results, _, err = s.cfg.SearchService.SearchFTSWithRecency(ctx, userID, query, limit, 0, recencyBias)
	} else {
		results, _, err = s.cfg.SearchService.SearchFTS(ctx, userID, query, limit, 0)
	}
	if err != nil {
		return nil, fmt.Errorf("agent.Service.NotesSearch: %w", err)
	}
	return results, nil
}

// NotesRead reads a user note by ID, returning the full note.
func (s *Service) NotesRead(ctx context.Context, userID, noteID string) (*note.Note, error) {
	n, err := s.cfg.NoteService.Get(ctx, userID, noteID)
	if err != nil {
		return nil, fmt.Errorf("agent.Service.NotesRead: %w", err)
	}
	return n, nil
}

// NotesList lists user notes with optional project and tag filtering.
func (s *Service) NotesList(ctx context.Context, userID string, projectSlug, tag string, limit int) ([]*note.Note, int, error) {
	filter := note.NoteFilter{
		Tag:   tag,
		Limit: limit,
	}

	// Resolve project slug to ID if provided.
	if projectSlug != "" {
		p, err := s.cfg.ProjectService.GetBySlug(ctx, userID, projectSlug)
		if err != nil {
			return nil, 0, fmt.Errorf("agent.Service.NotesList: resolve project: %w", err)
		}
		filter.ProjectID = p.ID
	}

	notes, total, err := s.cfg.NoteService.List(ctx, userID, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("agent.Service.NotesList: %w", err)
	}
	return notes, total, nil
}

// NotesCreate creates a user note with the "created-by:agent" tag auto-appended.
func (s *Service) NotesCreate(ctx context.Context, userID, title, body, projectSlug string, tags []string) (*note.Note, error) {
	var projectID string
	if projectSlug != "" {
		p, err := s.cfg.ProjectService.GetBySlug(ctx, userID, projectSlug)
		if err != nil {
			return nil, fmt.Errorf("agent.Service.NotesCreate: resolve project: %w", err)
		}
		projectID = p.ID
	}

	// Always append created-by:agent tag. Copy to avoid mutating caller's slice.
	allTags := make([]string, len(tags)+1)
	copy(allTags, tags)
	allTags[len(tags)] = TagCreatedByAgent

	n, err := s.cfg.NoteService.Create(ctx, userID, note.CreateNoteReq{
		Title:     title,
		Body:      body,
		ProjectID: projectID,
		Tags:      allTags,
	})
	if err != nil {
		return nil, fmt.Errorf("agent.Service.NotesCreate: %w", err)
	}

	// Enqueue embed task with "user" scope (this is a user-facing note).
	s.enqueueEmbedWithScope(ctx, userID, n.ID, "user")

	// Notify via WebSocket.
	s.notifyWS(userID, "agent.note_created", map[string]string{
		"note_id": n.ID,
		"title":   title,
	})

	return n, nil
}

// --- Tool Call Audit ---

// LogToolCall persists a tool call audit record to the user's database.
func (s *Service) LogToolCall(ctx context.Context, userID string, tc *ToolCallRecord) error {
	db, err := s.cfg.UserDBManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("agent.Service.LogToolCall: open db: %w", err)
	}
	return s.cfg.Store.LogToolCall(ctx, db, tc)
}

// --- Context Gathering ---

// ContextGather searches for relevant context across notes, returning results
// truncated to the character budget. Scope filters: "agent", "user", or "all".
// When recencyBias > 0, results are re-ranked with recency weighting.
func (s *Service) ContextGather(ctx context.Context, userID, query, scope string, maxChars int, recencyBias float64) ([]KnowledgeHit, error) {
	if maxChars <= 0 {
		maxChars = 3000
	}

	hits := s.searchKnowledgeScoped(ctx, userID, query, scope, 10, recencyBias)
	if len(hits) == 0 {
		return []KnowledgeHit{}, nil
	}

	return truncateKnowledge(hits, maxChars), nil
}

// --- Memory Search ---

// MemorySearch searches agent knowledge notes using FTS and semantic search.
// Returns results scoped to the agent-memory project.
func (s *Service) MemorySearch(ctx context.Context, userID, query string, limit int) ([]KnowledgeHit, error) {
	if limit <= 0 {
		limit = 10
	}
	hits := s.searchKnowledgeScoped(ctx, userID, query, "agent", limit, 0.0)
	if hits == nil {
		hits = []KnowledgeHit{}
	}
	return hits, nil
}

// --- Session Metrics ---

// SessionMetrics returns aggregate tool call statistics for a session.
func (s *Service) SessionMetrics(ctx context.Context, userID, sessionName string) (*SessionMetrics, error) {
	db, err := s.cfg.UserDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("agent.Service.SessionMetrics: open db: %w", err)
	}

	sess, err := s.cfg.Store.GetSessionByName(ctx, db, sessionName)
	if err != nil {
		return nil, fmt.Errorf("agent.Service.SessionMetrics: %w", err)
	}

	totalCalls, breakdown, errorCount, avgDuration, err := s.cfg.Store.GetSessionMetrics(ctx, db, sess.ID)
	if err != nil {
		return nil, fmt.Errorf("agent.Service.SessionMetrics: %w", err)
	}

	// Count notes created/modified by looking at session-tagged notes.
	notesCreated := 0
	notesModified := 0
	projectID, projErr := s.ensureAgentMemoryProject(ctx, userID)
	if projErr == nil {
		notes, _, listErr := s.cfg.NoteService.List(ctx, userID, note.NoteFilter{
			ProjectID: projectID,
			Tag:       "session:" + sessionName,
			Limit:     100,
		})
		if listErr == nil {
			for _, n := range notes {
				if n.CreatedAt.Equal(n.UpdatedAt) || n.UpdatedAt.Sub(n.CreatedAt) < time.Second {
					notesCreated++
				} else {
					notesModified++
				}
			}
		}
	}

	durationSec := int64(0)
	if sess.Status == StatusCompleted {
		durationSec = int64(sess.UpdatedAt.Sub(sess.CreatedAt).Seconds())
	} else {
		durationSec = int64(time.Since(sess.CreatedAt).Seconds())
	}

	return &SessionMetrics{
		SessionName:   sessionName,
		Status:        sess.Status,
		DurationSec:   durationSec,
		ToolCallCount: totalCalls,
		ToolBreakdown: breakdown,
		NotesCreated:  notesCreated,
		NotesModified: notesModified,
		ErrorCount:    errorCount,
		AvgDurationMs: avgDuration,
	}, nil
}

// --- Internal Helpers ---

// upsertSessionNote creates or updates a session note (plan, context).
func (s *Service) upsertSessionNote(ctx context.Context, userID, sessionName, noteType, title, content string) (string, error) {
	tags := SessionTags(sessionName, noteType, "active")

	// Try to find existing note.
	noteID, err := s.findSessionNote(ctx, userID, sessionName, noteType)
	if err == nil {
		// Update existing.
		body := content
		if _, updateErr := s.cfg.NoteService.Update(ctx, userID, noteID, note.UpdateNoteReq{
			Body: &body,
		}); updateErr != nil {
			return "", fmt.Errorf("agent.Service.upsertSessionNote: update: %w", updateErr)
		}
		s.enqueueEmbed(ctx, userID, noteID)
		return noteID, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return "", fmt.Errorf("agent.Service.upsertSessionNote: find: %w", err)
	}

	// Create new note.
	projectID, projErr := s.ensureAgentMemoryProject(ctx, userID)
	if projErr != nil {
		return "", fmt.Errorf("agent.Service.upsertSessionNote: %w", projErr)
	}

	n, createErr := s.cfg.NoteService.Create(ctx, userID, note.CreateNoteReq{
		Title:     title,
		Body:      content,
		ProjectID: projectID,
		Tags:      tags,
	})
	if createErr != nil {
		return "", fmt.Errorf("agent.Service.upsertSessionNote: create: %w", createErr)
	}
	s.enqueueEmbed(ctx, userID, n.ID)
	return n.ID, nil
}

// findSessionNote finds a session note by session name and type.
// Uses tag filter + title prefix matching.
func (s *Service) findSessionNote(ctx context.Context, userID, sessionName, noteType string) (string, error) {
	projectID, err := s.ensureAgentMemoryProject(ctx, userID)
	if err != nil {
		return "", err
	}

	notes, _, err := s.cfg.NoteService.List(ctx, userID, note.NoteFilter{
		ProjectID: projectID,
		Tag:       "session:" + sessionName,
		Limit:     10,
	})
	if err != nil {
		return "", fmt.Errorf("agent.Service.findSessionNote: %w", err)
	}

	// Match by title prefix.
	var prefix string
	switch noteType {
	case "plan":
		prefix = "Session Plan:"
	case "progress":
		prefix = "Session Progress:"
	case "context":
		prefix = "Session Context:"
	}

	for _, n := range notes {
		if strings.HasPrefix(n.Title, prefix) {
			return n.ID, nil
		}
	}

	return "", fmt.Errorf("agent.Service.findSessionNote: %w", ErrNotFound)
}

// findKnowledgeNote finds a knowledge note by category and name.
func (s *Service) findKnowledgeNote(ctx context.Context, userID, category, name string) (string, error) {
	projectID, err := s.ensureAgentMemoryProject(ctx, userID)
	if err != nil {
		return "", err
	}

	title := KnowledgeNoteTitle(category, name)
	notes, _, err := s.cfg.NoteService.List(ctx, userID, note.NoteFilter{
		ProjectID: projectID,
		Tag:       "domain:" + category,
		Limit:     50,
	})
	if err != nil {
		return "", fmt.Errorf("agent.Service.findKnowledgeNote: %w", err)
	}

	for _, n := range notes {
		if n.Title == title {
			return n.ID, nil
		}
	}

	return "", fmt.Errorf("agent.Service.findKnowledgeNote: %w", ErrNotFound)
}

// assembleBriefing builds a budgeted context package for a session.
func (s *Service) assembleBriefing(ctx context.Context, userID string, db DBTX, sess *Session, maxChars int) (*Briefing, error) {
	briefing := &Briefing{Session: sess}

	// Determine what sections we have data for.
	var plan, lastProgress, parentPlan string
	var siblings []SiblingFinding

	// Try to load session state (plan + progress).
	planNoteID, planErr := s.findSessionNote(ctx, userID, sess.Name, "plan")
	if planErr == nil {
		planNote, err := s.cfg.NoteService.Get(ctx, userID, planNoteID)
		if err == nil {
			plan = planNote.Body
		}
	}

	progressNoteID, progErr := s.findSessionNote(ctx, userID, sess.Name, "progress")
	if progErr == nil {
		progNote, err := s.cfg.NoteService.Get(ctx, userID, progressNoteID)
		if err == nil {
			lastProgress = progNote.Body
		}
	}

	// Load parent plan if this is a child session.
	hasParent := false
	if sess.ParentSessionID != "" {
		parent, err := s.cfg.Store.GetSession(ctx, db, sess.ParentSessionID)
		if err == nil {
			parentPlanID, ppErr := s.findSessionNote(ctx, userID, parent.Name, "plan")
			if ppErr == nil {
				ppNote, noteErr := s.cfg.NoteService.Get(ctx, userID, parentPlanID)
				if noteErr == nil {
					parentPlan = ppNote.Body
					hasParent = true
				}
			}
		}
	}

	// Load sibling findings (completed children of the same parent).
	if sess.ParentSessionID != "" {
		children, err := s.cfg.Store.ListChildSessions(ctx, db, sess.ParentSessionID)
		if err == nil {
			for _, child := range children {
				if child.ID == sess.ID {
					continue // skip self
				}
				if child.Status == StatusCompleted && child.Findings != "" {
					siblings = append(siblings, SiblingFinding{
						SessionName: child.Name,
						Findings:    child.Findings,
					})
				}
			}
		}
	}

	hasSession := plan != "" || lastProgress != ""
	hasSiblings := len(siblings) > 0

	// Search for relevant knowledge.
	var knowledge []KnowledgeHit
	searchQuery := sess.Name
	if parentPlan != "" {
		searchQuery += " " + parentPlan
	}
	knowledge = s.searchKnowledge(ctx, userID, searchQuery, 5)
	hasKnowledge := len(knowledge) > 0

	// Allocate budget.
	budget := allocateBudget(maxChars, hasSession, hasParent, hasSiblings, hasKnowledge)

	// Apply budgets.
	if hasSession {
		briefing.Plan = truncateToChars(plan, budget.sessionChars*2/3)
		briefing.LastProgress = truncateToChars(lastProgress, budget.sessionChars/3)
	}
	if hasParent {
		briefing.ParentPlan = truncateToChars(parentPlan, budget.parentChars)
	}
	if hasSiblings {
		briefing.SiblingFindings = truncateSiblings(siblings, budget.siblingChars)
	}
	if hasKnowledge {
		briefing.Knowledge = truncateKnowledge(knowledge, budget.knowledgeChars)
	}

	return briefing, nil
}

// searchKnowledge performs FTS search, falling back gracefully. Searches all notes.
func (s *Service) searchKnowledge(ctx context.Context, userID, query string, limit int) []KnowledgeHit {
	return s.searchKnowledgeScoped(ctx, userID, query, "all", limit, 0.0)
}

// searchKnowledgeScoped performs scoped search (semantic then FTS fallback).
// Scope: "agent" (agent-memory only), "user" (exclude agent-memory), "all" (everything).
// When recencyBias > 0, the WithRecency variants are used for recency-weighted scoring.
func (s *Service) searchKnowledgeScoped(ctx context.Context, userID, query, scope string, limit int, recencyBias float64) []KnowledgeHit {
	if s.cfg.SearchService == nil {
		return nil
	}

	// Build scope filters.
	var chromaWhere map[string]interface{}
	var includeProjectID, excludeProjectID string

	switch scope {
	case "agent":
		chromaWhere = map[string]interface{}{"scope": "agent"}
		// For FTS, scope to agent-memory project.
		if pid, err := s.ensureAgentMemoryProject(ctx, userID); err == nil {
			includeProjectID = pid
		}
	case "user":
		chromaWhere = map[string]interface{}{"scope": "user"}
		// For FTS, exclude agent-memory project.
		if pid, err := s.ensureAgentMemoryProject(ctx, userID); err == nil {
			excludeProjectID = pid
		}
	default:
		// "all" - no filters.
	}

	// Try semantic search first.
	var semResults []search.SemanticResult
	var err error
	if recencyBias > 0 {
		semResults, err = s.cfg.SearchService.SearchSemanticScopedWithRecency(ctx, userID, query, limit, chromaWhere, recencyBias)
	} else {
		semResults, err = s.cfg.SearchService.SearchSemanticScoped(ctx, userID, query, limit, chromaWhere)
	}
	if err == nil && len(semResults) > 0 {
		hits := make([]KnowledgeHit, 0, len(semResults))
		for _, r := range semResults {
			hits = append(hits, KnowledgeHit{
				Title:   r.Title,
				Snippet: r.Snippet,
				Source:  "semantic",
				Score:   r.Score,
			})
		}
		return hits
	}

	// Fall back to FTS.
	var ftsResults []search.FTSResult
	var ftsErr error
	if recencyBias > 0 {
		ftsResults, _, ftsErr = s.cfg.SearchService.SearchFTSScopedWithRecency(ctx, userID, query, limit, 0, includeProjectID, excludeProjectID, recencyBias)
	} else {
		ftsResults, _, ftsErr = s.cfg.SearchService.SearchFTSScoped(ctx, userID, query, limit, 0, includeProjectID, excludeProjectID)
	}
	if ftsErr != nil || len(ftsResults) == 0 {
		return nil
	}

	hits := make([]KnowledgeHit, 0, len(ftsResults))
	for _, r := range ftsResults {
		hits = append(hits, KnowledgeHit{
			Title:   r.Title,
			Snippet: r.Snippet,
			Source:  "fts",
			Score:   float64(r.Rank),
		})
	}
	return hits
}

// updateSessionNoteStatus updates the status tag on session notes.
func (s *Service) updateSessionNoteStatus(ctx context.Context, userID, sessionName, newStatus string) {
	// Best-effort: update tags on session notes. Non-critical if it fails.
	for _, noteType := range []string{"plan", "progress", "context"} {
		noteID, err := s.findSessionNote(ctx, userID, sessionName, noteType)
		if err != nil {
			continue
		}
		n, err := s.cfg.NoteService.Get(ctx, userID, noteID)
		if err != nil {
			continue
		}

		// Replace status:active with status:{newStatus} in tags.
		newTags := make([]string, 0, len(n.Tags))
		for _, tag := range n.Tags {
			if strings.HasPrefix(tag, "status:") {
				newTags = append(newTags, "status:"+newStatus)
			} else {
				newTags = append(newTags, tag)
			}
		}
		_, _ = s.cfg.NoteService.Update(ctx, userID, noteID, note.UpdateNoteReq{
			Tags: &newTags,
		})
	}
}

// formatProgressEntry formats a progress entry for the progress note.
func formatProgressEntry(task, status, notes string) string {
	entry := fmt.Sprintf("[%s] %s", status, task)
	if notes != "" {
		entry += ": " + notes
	}
	return entry
}

// truncateSiblings truncates sibling findings to fit within the budget.
func truncateSiblings(siblings []SiblingFinding, maxChars int) []SiblingFinding {
	if maxChars <= 0 || len(siblings) == 0 {
		return nil
	}

	result := make([]SiblingFinding, 0, len(siblings))
	remaining := maxChars
	for _, sib := range siblings {
		header := sib.SessionName + ": "
		headerLen := utf8.RuneCountInString(header)
		if remaining <= headerLen {
			break
		}
		maxFindings := remaining - headerLen
		findings := truncateToChars(sib.Findings, maxFindings)
		result = append(result, SiblingFinding{
			SessionName: sib.SessionName,
			Findings:    findings,
		})
		remaining -= headerLen + utf8.RuneCountInString(findings)
		if remaining <= 0 {
			break
		}
	}
	return result
}

// truncateKnowledge truncates knowledge hits to fit within the budget.
func truncateKnowledge(hits []KnowledgeHit, maxChars int) []KnowledgeHit {
	if maxChars <= 0 || len(hits) == 0 {
		return nil
	}

	result := make([]KnowledgeHit, 0, len(hits))
	remaining := maxChars
	for _, hit := range hits {
		header := hit.Title + ": "
		headerLen := utf8.RuneCountInString(header)
		if remaining <= headerLen {
			break
		}
		maxSnippet := remaining - headerLen
		snippet := truncateToChars(hit.Snippet, maxSnippet)
		result = append(result, KnowledgeHit{
			Title:   hit.Title,
			Snippet: snippet,
			Source:  hit.Source,
			Score:   hit.Score,
		})
		remaining -= headerLen + utf8.RuneCountInString(snippet)
		if remaining <= 0 {
			break
		}
	}
	return result
}

// enqueueEmbed enqueues an embed task for a note. Silently skips if AIQueue is nil.
// This is necessary because note.Service.Create suppresses watcher events,
// so agent notes will not be auto-embedded by the watcher.
// The scope parameter controls ChromaDB metadata ("agent" for agent-memory notes).
func (s *Service) enqueueEmbed(ctx context.Context, userID, noteID string) {
	s.enqueueEmbedWithScope(ctx, userID, noteID, "agent")
}

// enqueueEmbedWithScope enqueues an embed task with explicit scope.
func (s *Service) enqueueEmbedWithScope(ctx context.Context, userID, noteID, scope string) {
	if s.cfg.AIQueue == nil {
		return
	}
	payload, marshalErr := json.Marshal(ai.EmbedPayload{NoteID: noteID, Scope: scope})
	if marshalErr != nil {
		s.cfg.Logger.Warn("agent: failed to marshal embed payload", "error", marshalErr)
		return
	}
	if err := s.cfg.AIQueue.Enqueue(ctx, &ai.Task{
		UserID:   userID,
		Type:     ai.TaskTypeEmbed,
		Priority: ai.PriorityBackground,
		Payload:  payload,
	}); err != nil {
		s.cfg.Logger.Warn("agent: failed to enqueue embed task",
			"note_id", noteID, "error", err)
	}
}

// enqueueDeleteEmbed enqueues a delete-embed task for a note.
func (s *Service) enqueueDeleteEmbed(ctx context.Context, userID, noteID string) {
	if s.cfg.AIQueue == nil {
		return
	}
	payload, marshalErr := json.Marshal(ai.DeleteEmbedPayload{NoteID: noteID})
	if marshalErr != nil {
		s.cfg.Logger.Warn("agent: failed to marshal delete-embed payload", "error", marshalErr)
		return
	}
	if err := s.cfg.AIQueue.Enqueue(ctx, &ai.Task{
		UserID:   userID,
		Type:     ai.TaskTypeDeleteEmbed,
		Priority: ai.PriorityBackground,
		Payload:  payload,
	}); err != nil {
		s.cfg.Logger.Warn("agent: failed to enqueue delete embed task",
			"note_id", noteID, "error", err)
	}
}

// notifyWS sends an agent event via WebSocket. Silently skips if WSNotifier is nil.
func (s *Service) notifyWS(userID, eventType string, payload interface{}) {
	if s.cfg.WSNotifier == nil {
		return
	}
	s.cfg.WSNotifier.SendAgentEvent(userID, eventType, payload)
}

// parseKnowledgeTitle extracts category and name from a knowledge note title.
// Title format: "Knowledge: {category} - {name}"
func parseKnowledgeTitle(title string) (string, string) {
	prefix := "Knowledge: "
	if !strings.HasPrefix(title, prefix) {
		return "", title
	}
	rest := title[len(prefix):]
	parts := strings.SplitN(rest, " - ", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", rest
}
