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
