// Package assistant implements the agentic AI assistant with tool-use loop.
package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/katata/seam/internal/ai"
)

// Domain errors.
var (
	ErrToolNotFound       = errors.New("tool not found")
	ErrInvalidArguments   = errors.New("invalid tool arguments")
	ErrMaxIterations      = errors.New("assistant reached maximum iterations")
	ErrConfirmationNeeded = errors.New("action requires user confirmation")
	ErrNotFound           = errors.New("not found")
)

// ToolFunc is the function signature for tool implementations.
// It receives the user context, userID, and JSON-encoded arguments.
// It returns a JSON-encoded result or an error.
type ToolFunc func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error)

// Tool represents a registered tool that the assistant can invoke.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema for the arguments
	Func        ToolFunc        `json:"-"`
	ReadOnly    bool            `json:"-"` // true = safe (no confirmation needed)
}

// ToolRegistry manages the set of tools available to the assistant.
type ToolRegistry struct {
	tools map[string]*Tool
}

// NewToolRegistry creates an empty tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]*Tool),
	}
}

// Register adds a tool to the registry.
func (r *ToolRegistry) Register(t *Tool) {
	r.tools[t.Name] = t
}

// Get returns a tool by name or ErrToolNotFound.
func (r *ToolRegistry) Get(name string) (*Tool, error) {
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	return t, nil
}

// Definitions returns AI tool definitions for all registered tools.
// Results are sorted by name for deterministic ordering in LLM calls.
func (r *ToolRegistry) Definitions() []ai.ToolDefinition {
	defs := make([]ai.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, ai.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Name < defs[j].Name
	})
	return defs
}

// Names returns the names of all registered tools.
func (r *ToolRegistry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// ToolResult captures the outcome of a tool invocation for auditing.
type ToolResult struct {
	ToolName   string          `json:"tool_name"`
	Arguments  json.RawMessage `json:"arguments"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	DurationMs int64           `json:"duration_ms"`
}
