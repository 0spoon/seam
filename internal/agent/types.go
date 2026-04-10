// Package agent implements the agent memory domain: session lifecycle,
// knowledge management, and context gathering for AI agents using Seam
// as their long-term memory via MCP.
package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Status constants for agent sessions.
const (
	StatusActive    = "active"
	StatusCompleted = "completed"
	StatusArchived  = "archived"
)

// AgentMemoryProject is the slug of the project that holds all agent notes.
const AgentMemoryProject = "agent-memory"

// TagCreatedByAgent is the tag applied to all notes created by agents.
const TagCreatedByAgent = "created-by:agent"

// Budget and limit constants.
const (
	MaxFindingsChars       = 1500
	DefaultMaxContextChars = 4000
	MaxSessionNameLen      = 200
)

// Domain errors.
var (
	ErrNotFound           = errors.New("not found")
	ErrSessionNotActive   = errors.New("session is not active")
	ErrFindingsTooLong    = errors.New("findings exceed maximum length")
	ErrFindingsRequired   = errors.New("findings are required")
	ErrInvalidSessionName = errors.New("invalid session name")
)

// Session represents an agent working session.
// Sessions form a tree via ParentSessionID (derived from "/" in the name).
type Session struct {
	ID              string
	Name            string   // hierarchical: "refactor-auth/analyze-middleware"
	ParentSessionID string   // ULID of parent, empty for root sessions
	Status          string   // "active", "completed", "archived"
	Findings        string   // compact summary (max 1500 chars), set on session_end
	Metadata        Metadata // agent identity, config
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Metadata holds agent identity and configuration.
type Metadata struct {
	AgentName string `json:"agent_name,omitempty"`
}

// Briefing is the size-budgeted context package returned by session_start.
type Briefing struct {
	Session         *Session         `json:"session"`
	Plan            string           `json:"plan,omitempty"`
	LastProgress    string           `json:"last_progress,omitempty"`
	ParentPlan      string           `json:"parent_plan,omitempty"`
	SiblingFindings []SiblingFinding `json:"sibling_findings,omitempty"`
	Knowledge       []KnowledgeHit   `json:"knowledge,omitempty"`
}

// SiblingFinding holds a completed sibling session's findings.
type SiblingFinding struct {
	SessionName string `json:"session_name"`
	Findings    string `json:"findings"`
}

// KnowledgeHit represents a search result from the knowledge base.
type KnowledgeHit struct {
	Title   string  `json:"title"`
	Snippet string  `json:"snippet"`
	Source  string  `json:"source"`
	Score   float64 `json:"score"`
}

// ToolCallRecord is an audit log entry for a single MCP tool invocation.
type ToolCallRecord struct {
	ID         string
	SessionID  string
	ToolName   string
	Arguments  string // JSON
	Result     string // JSON (nullable)
	Error      string // error message (nullable)
	DurationMs int64
	CreatedAt  time.Time
}

// MemoryItem is a summary of a knowledge note for listing.
type MemoryItem struct {
	Category  string    `json:"category"`
	Name      string    `json:"name"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SessionMetrics holds aggregate statistics for a session.
type SessionMetrics struct {
	SessionName   string         `json:"session_name"`
	Status        string         `json:"status"`
	DurationSec   int64          `json:"duration_sec"`
	ToolCallCount int            `json:"tool_call_count"`
	ToolBreakdown map[string]int `json:"tool_breakdown"`
	NotesCreated  int            `json:"notes_created"`
	NotesModified int            `json:"notes_modified"`
	ErrorCount    int            `json:"error_count"`
	AvgDurationMs int64          `json:"avg_duration_ms"`
}

// DBTX is satisfied by both *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// Store is the data access layer for agent sessions and tool calls.
type Store interface {
	CreateSession(ctx context.Context, db DBTX, s *Session) error
	GetSession(ctx context.Context, db DBTX, id string) (*Session, error)
	GetSessionByName(ctx context.Context, db DBTX, name string) (*Session, error)
	UpdateSession(ctx context.Context, db DBTX, s *Session) error
	ListSessions(ctx context.Context, db DBTX, status string, limit, offset int) ([]*Session, error)
	ListChildSessions(ctx context.Context, db DBTX, parentID string) ([]*Session, error)
	ReconcileChildren(ctx context.Context, db DBTX, parentID, parentName string) (int64, error)
	LogToolCall(ctx context.Context, db DBTX, tc *ToolCallRecord) error
	ListToolCalls(ctx context.Context, db DBTX, sessionID string, limit int) ([]*ToolCallRecord, error)
	GetSessionMetrics(ctx context.Context, db DBTX, sessionID string) (int, map[string]int, int, int64, error)
}

// sessionNameRe allows only alphanumeric, hyphens, underscores, and forward slashes.
var sessionNameRe = regexp.MustCompile(`^[a-zA-Z0-9_\-/]+$`)

// ValidateSessionName checks that a session name is valid.
// Allowed characters: [a-zA-Z0-9_-/]. No "..", no leading/trailing "/",
// no consecutive "/".
func ValidateSessionName(name string) error {
	if name == "" {
		return fmt.Errorf("agent.ValidateSessionName: empty name: %w", ErrInvalidSessionName)
	}
	if len(name) > MaxSessionNameLen {
		return fmt.Errorf("agent.ValidateSessionName: name too long (%d chars, max %d): %w",
			len(name), MaxSessionNameLen, ErrInvalidSessionName)
	}
	if !sessionNameRe.MatchString(name) {
		return fmt.Errorf("agent.ValidateSessionName: invalid characters in %q: %w", name, ErrInvalidSessionName)
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return fmt.Errorf("agent.ValidateSessionName: leading or trailing slash in %q: %w", name, ErrInvalidSessionName)
	}
	if strings.Contains(name, "//") {
		return fmt.Errorf("agent.ValidateSessionName: consecutive slashes in %q: %w", name, ErrInvalidSessionName)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("agent.ValidateSessionName: path traversal in %q: %w", name, ErrInvalidSessionName)
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("agent.ValidateSessionName: null byte in %q: %w", name, ErrInvalidSessionName)
	}
	return nil
}

// FlattenSessionName replaces "/" with " - " for use in note titles.
// e.g., "refactor-auth/analyze" -> "refactor-auth - analyze".
func FlattenSessionName(name string) string {
	return strings.ReplaceAll(name, "/", " - ")
}

// ParentSessionName extracts the parent session name from a hierarchical name.
// Returns the parent name and true, or empty and false for root sessions.
func ParentSessionName(name string) (string, bool) {
	idx := strings.LastIndex(name, "/")
	if idx < 0 {
		return "", false
	}
	return name[:idx], true
}

// PlanNoteTitle returns the title for a session plan note.
func PlanNoteTitle(sessionName string) string {
	return "Session Plan: " + FlattenSessionName(sessionName)
}

// ProgressNoteTitle returns the title for a session progress note.
func ProgressNoteTitle(sessionName string) string {
	return "Session Progress: " + FlattenSessionName(sessionName)
}

// ContextNoteTitle returns the title for a session context note.
func ContextNoteTitle(sessionName string) string {
	return "Session Context: " + FlattenSessionName(sessionName)
}

// KnowledgeNoteTitle returns the title for a knowledge note.
func KnowledgeNoteTitle(category, name string) string {
	return "Knowledge: " + category + " - " + name
}

// SessionTags returns the standard set of tags for a session note.
func SessionTags(sessionName, noteType, status string) []string {
	return []string{
		"session:" + sessionName,
		"type:" + noteType,
		"status:" + status,
		TagCreatedByAgent,
	}
}

// KnowledgeTags returns the standard set of tags for a knowledge note.
func KnowledgeTags(category string) []string {
	return []string{
		"type:knowledge",
		"domain:" + category,
		TagCreatedByAgent,
	}
}

// --- Research Lab ---

// ResearchProject is the slug of the project that holds lab notebooks and trials.
const ResearchProject = "research"

// LabSessionPrefix is the session name prefix for lab sessions.
const LabSessionPrefix = "lab/"

// Trial outcome constants.
const (
	OutcomeSuccess      = "success"
	OutcomeFailure      = "failure"
	OutcomePartial      = "partial"
	OutcomeInconclusive = "inconclusive"
	OutcomePending      = "pending"
)

// Domain errors for research lab.
var (
	ErrInvalidLabName = errors.New("invalid lab name")
	ErrInvalidOutcome = errors.New("invalid outcome")
)

// LabInfo is the response from lab_open.
type LabInfo struct {
	SessionName    string         `json:"session_name"`
	NotebookNoteID string         `json:"notebook_note_id"`
	Problem        string         `json:"problem"`
	Domain         string         `json:"domain"`
	Status         string         `json:"status"`
	Trials         []TrialSummary `json:"trials"`
	Briefing       *Briefing      `json:"briefing,omitempty"`
}

// TrialSummary is the structured representation of a trial note.
type TrialSummary struct {
	Title     string    `json:"title"`
	NoteID    string    `json:"note_id"`
	Outcome   string    `json:"outcome"`
	Changes   string    `json:"changes,omitempty"`
	Expected  string    `json:"expected,omitempty"`
	Actual    string    `json:"actual,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DecisionInfo is the response from decision_record.
type DecisionInfo struct {
	Title  string `json:"title"`
	NoteID string `json:"note_id"`
}

// LabSessionName returns the session name for a lab.
func LabSessionName(name string) string {
	return LabSessionPrefix + name
}

// TrialSessionName returns the child session name for a trial.
func TrialSessionName(lab, slug string) string {
	return LabSessionPrefix + lab + "/" + slug
}

// LabNotebookTitle returns the note title for a lab notebook.
func LabNotebookTitle(name string) string {
	return "Lab Notebook: " + name
}

// TrialNoteTitle returns the note title for a trial.
func TrialNoteTitle(title string) string {
	return "Trial: " + title
}

// DecisionNoteTitle returns the note title for a decision.
func DecisionNoteTitle(title string) string {
	return "Decision: " + title
}

// LabTags returns the standard set of tags for a lab notebook note.
func LabTags(name, domain string) []string {
	return []string{
		"type:lab-notebook",
		"lab:" + name,
		"domain:" + domain,
		TagCreatedByAgent,
	}
}

// TrialTags returns the standard set of tags for a trial note.
func TrialTags(name, domain string) []string {
	return []string{
		"type:trial",
		"lab:" + name,
		"domain:" + domain,
		TagCreatedByAgent,
	}
}

// TrialTagsWithOutcome returns trial tags including the outcome.
func TrialTagsWithOutcome(name, domain, outcome string) []string {
	tags := TrialTags(name, domain)
	return append(tags, "outcome:"+outcome)
}

// DecisionTags returns the standard set of tags for a decision note.
func DecisionTags(name, domain string) []string {
	return []string{
		"type:decision",
		"lab:" + name,
		"domain:" + domain,
		TagCreatedByAgent,
	}
}

// labNameRe allows alphanumeric characters and hyphens.
var labNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*$`)

// ValidateLabName checks that a lab name is valid.
func ValidateLabName(name string) error {
	if name == "" {
		return fmt.Errorf("agent.ValidateLabName: empty name: %w", ErrInvalidLabName)
	}
	if len(name) > 100 {
		return fmt.Errorf("agent.ValidateLabName: name too long (%d chars, max 100): %w", len(name), ErrInvalidLabName)
	}
	if !labNameRe.MatchString(name) {
		return fmt.Errorf("agent.ValidateLabName: invalid characters in %q (alphanumeric and hyphens only): %w", name, ErrInvalidLabName)
	}
	return nil
}

// ValidateOutcome checks that an outcome value is valid.
// Empty string is allowed (means outcome not yet recorded).
func ValidateOutcome(outcome string) error {
	if outcome == "" {
		return nil
	}
	switch outcome {
	case OutcomeSuccess, OutcomeFailure, OutcomePartial, OutcomeInconclusive:
		return nil
	default:
		return fmt.Errorf("agent.ValidateOutcome: %q is not a valid outcome (success/failure/partial/inconclusive): %w", outcome, ErrInvalidOutcome)
	}
}

// SlugifyTrialTitle converts a trial title to a URL-safe slug for session names.
func SlugifyTrialTitle(title string) string {
	s := strings.ToLower(title)
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	result := b.String()
	result = strings.TrimRight(result, "-")
	if len(result) > 80 {
		result = result[:80]
		result = strings.TrimRight(result, "-")
	}
	return result
}

// ParseTrialSections extracts structured fields from a trial note body.
// It parses the markdown sections delimited by "## " headers.
func ParseTrialSections(body string) (changes, expected, actual, notes string) {
	sections := splitMarkdownSections(body)
	changes = strings.TrimSpace(sections["Changes"])
	expected = strings.TrimSpace(sections["Expected"])
	actual = strings.TrimSpace(sections["Actual"])
	notes = strings.TrimSpace(sections["Notes"])
	return
}

// splitMarkdownSections splits a markdown body into a map of section name -> content.
func splitMarkdownSections(body string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(body, "\n")
	var currentSection string
	var sectionLines []string

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if currentSection != "" {
				result[currentSection] = strings.Join(sectionLines, "\n")
			}
			currentSection = strings.TrimPrefix(line, "## ")
			sectionLines = nil
		} else if currentSection != "" {
			sectionLines = append(sectionLines, line)
		}
	}
	if currentSection != "" {
		result[currentSection] = strings.Join(sectionLines, "\n")
	}
	return result
}

// ExtractOutcomeFromTags finds the outcome tag value from a tag slice.
// Returns OutcomePending if no outcome tag is found.
func ExtractOutcomeFromTags(tags []string) string {
	for _, t := range tags {
		if v, ok := strings.CutPrefix(t, "outcome:"); ok {
			return v
		}
	}
	return OutcomePending
}

// ExtractDomainFromTags finds the domain tag value from a tag slice.
func ExtractDomainFromTags(tags []string) string {
	for _, t := range tags {
		if v, ok := strings.CutPrefix(t, "domain:"); ok {
			return v
		}
	}
	return ""
}

// HasTag checks whether a tag slice contains a specific tag.
func HasTag(tags []string, tag string) bool {
	return slices.Contains(tags, tag)
}
