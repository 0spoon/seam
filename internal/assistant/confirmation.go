package assistant

import "sync"

// ConfirmationManager determines which tool calls require user confirmation.
type ConfirmationManager struct {
	mu       sync.RWMutex
	required map[string]bool
}

// NewConfirmationManager creates a ConfirmationManager with the given list
// of tool names that require confirmation before execution.
func NewConfirmationManager(requiredTools []string) *ConfirmationManager {
	required := make(map[string]bool, len(requiredTools))
	for _, name := range requiredTools {
		required[name] = true
	}
	return &ConfirmationManager{required: required}
}

// RequiresConfirmation returns true if the given tool requires user approval.
func (m *ConfirmationManager) RequiresConfirmation(toolName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.required[toolName]
}

// SetRequired updates the list of tools requiring confirmation.
func (m *ConfirmationManager) SetRequired(toolNames []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	required := make(map[string]bool, len(toolNames))
	for _, name := range toolNames {
		required[name] = true
	}
	m.required = required
}
