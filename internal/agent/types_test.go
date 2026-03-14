package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSessionName_Valid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "simple", input: "refactor-auth"},
		{name: "with_underscore", input: "my_session"},
		{name: "hierarchical", input: "refactor-auth/analyze-middleware"},
		{name: "deep_hierarchy", input: "a/b/c/d"},
		{name: "numbers", input: "session-123"},
		{name: "uppercase", input: "MySession"},
		{name: "mixed", input: "Project_A/task-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSessionName(tt.input)
			require.NoError(t, err)
		})
	}
}

func TestValidateSessionName_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "leading_slash", input: "/leading"},
		{name: "trailing_slash", input: "trailing/"},
		{name: "consecutive_slashes", input: "a//b"},
		{name: "dot_dot", input: "a/../b"},
		{name: "only_dots", input: ".."},
		{name: "spaces", input: "has spaces"},
		{name: "special_chars", input: "has@special"},
		{name: "colon", input: "has:colon"},
		{name: "null_byte", input: "has\x00null"},
		{name: "dot_dot_at_start", input: "../etc"},
		{name: "dot_dot_segment", input: "a/.."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSessionName(tt.input)
			require.ErrorIs(t, err, ErrInvalidSessionName)
		})
	}
}

func TestFlattenSessionName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "no_slash", input: "simple", expected: "simple"},
		{name: "one_slash", input: "parent/child", expected: "parent - child"},
		{name: "multiple_slashes", input: "a/b/c", expected: "a - b - c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FlattenSessionName(tt.input)
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestParentSessionName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  string
		hasParent bool
	}{
		{name: "root", input: "root-session", expected: "", hasParent: false},
		{name: "child", input: "parent/child", expected: "parent", hasParent: true},
		{name: "grandchild", input: "a/b/c", expected: "a/b", hasParent: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParentSessionName(tt.input)
			require.Equal(t, tt.hasParent, ok)
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestSessionNoteTitles(t *testing.T) {
	tests := []struct {
		name        string
		sessionName string
		wantPlan    string
		wantProg    string
		wantCtx     string
	}{
		{
			name:        "simple",
			sessionName: "refactor-auth",
			wantPlan:    "Session Plan: refactor-auth",
			wantProg:    "Session Progress: refactor-auth",
			wantCtx:     "Session Context: refactor-auth",
		},
		{
			name:        "hierarchical",
			sessionName: "refactor-auth/analyze",
			wantPlan:    "Session Plan: refactor-auth - analyze",
			wantProg:    "Session Progress: refactor-auth - analyze",
			wantCtx:     "Session Context: refactor-auth - analyze",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.wantPlan, PlanNoteTitle(tt.sessionName))
			require.Equal(t, tt.wantProg, ProgressNoteTitle(tt.sessionName))
			require.Equal(t, tt.wantCtx, ContextNoteTitle(tt.sessionName))
		})
	}
}

func TestKnowledgeNoteTitle(t *testing.T) {
	got := KnowledgeNoteTitle("go", "middleware-patterns")
	require.Equal(t, "Knowledge: go - middleware-patterns", got)
}

func TestSessionTags(t *testing.T) {
	tags := SessionTags("refactor-auth/analyze", "plan", "active")
	require.Contains(t, tags, "session:refactor-auth/analyze")
	require.Contains(t, tags, "type:plan")
	require.Contains(t, tags, "status:active")
	require.Contains(t, tags, TagCreatedByAgent)
}

func TestConstants(t *testing.T) {
	require.Equal(t, 1500, MaxFindingsChars)
	require.Equal(t, 4000, DefaultMaxContextChars)
	require.Equal(t, "active", StatusActive)
	require.Equal(t, "completed", StatusCompleted)
	require.Equal(t, "archived", StatusArchived)
	require.Equal(t, "agent-memory", AgentMemoryProject)
	require.Equal(t, "created-by:agent", TagCreatedByAgent)
}

func TestKnowledgeTags(t *testing.T) {
	tags := KnowledgeTags("go")
	require.Len(t, tags, 3)
	require.Equal(t, "type:knowledge", tags[0])
	require.Equal(t, "domain:go", tags[1])
	require.Equal(t, TagCreatedByAgent, tags[2])
}

func TestKnowledgeTags_SpecialCategory(t *testing.T) {
	tests := []struct {
		name     string
		category string
		wantTag  string
	}{
		{name: "hyphenated", category: "code-review", wantTag: "domain:code-review"},
		{name: "underscored", category: "error_handling", wantTag: "domain:error_handling"},
		{name: "mixed", category: "go-api_design", wantTag: "domain:go-api_design"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := KnowledgeTags(tt.category)
			require.Len(t, tags, 3)
			require.Equal(t, "type:knowledge", tags[0])
			require.Equal(t, tt.wantTag, tags[1])
			require.Equal(t, TagCreatedByAgent, tags[2])
		})
	}
}

func TestKnowledgeNoteTitle_SpecialChars(t *testing.T) {
	tests := []struct {
		name     string
		category string
		itemName string
		expected string
	}{
		{
			name:     "hyphenated_category",
			category: "code-review",
			itemName: "best-practices",
			expected: "Knowledge: code-review - best-practices",
		},
		{
			name:     "multi_word_name",
			category: "architecture",
			itemName: "layer-separation-patterns",
			expected: "Knowledge: architecture - layer-separation-patterns",
		},
		{
			name:     "underscored",
			category: "error_handling",
			itemName: "retry_strategies",
			expected: "Knowledge: error_handling - retry_strategies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := KnowledgeNoteTitle(tt.category, tt.itemName)
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestSessionTags_Length(t *testing.T) {
	tags := SessionTags("my-session", "plan", "active")
	require.Len(t, tags, 4)
}

func TestSessionTags_HierarchicalName(t *testing.T) {
	tags := SessionTags("parent/child/grandchild", "progress", "completed")
	require.Equal(t, "session:parent/child/grandchild", tags[0])
	require.Equal(t, "type:progress", tags[1])
	require.Equal(t, "status:completed", tags[2])
	require.Equal(t, TagCreatedByAgent, tags[3])
}

func TestValidateSessionName_BoundaryAndEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "single_char",
			input:   "a",
			wantErr: false,
		},
		{
			name:    "single_digit",
			input:   "1",
			wantErr: false,
		},
		{
			name:    "very_long_name",
			input:   strings.Repeat("a", 1000),
			wantErr: false,
		},
		{
			name:    "max_depth_hierarchy",
			input:   "a/b/c/d/e/f/g/h/i/j",
			wantErr: false,
		},
		{
			name:    "single_underscore",
			input:   "_",
			wantErr: false,
		},
		{
			name:    "single_hyphen",
			input:   "-",
			wantErr: false,
		},
		{
			name:    "long_hierarchy_segments",
			input:   strings.Repeat("segment", 20) + "/" + strings.Repeat("child", 20),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSessionName(tt.input)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidSessionName)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFlattenSessionName_NoSlash(t *testing.T) {
	input := "simple-name"
	got := FlattenSessionName(input)
	require.Equal(t, input, got, "flattening a name with no slashes should return it unchanged")
}

func TestParentSessionName_SingleSegment(t *testing.T) {
	parent, ok := ParentSessionName("simple")
	require.False(t, ok)
	require.Equal(t, "", parent)
}

func TestSessionNoteTitles_DeepHierarchy(t *testing.T) {
	name := "a/b/c/d"
	flat := "a - b - c - d"

	require.Equal(t, "Session Plan: "+flat, PlanNoteTitle(name))
	require.Equal(t, "Session Progress: "+flat, ProgressNoteTitle(name))
	require.Equal(t, "Session Context: "+flat, ContextNoteTitle(name))
}

func TestErrors_AreDistinct(t *testing.T) {
	allErrors := []error{
		ErrNotFound,
		ErrSessionNotActive,
		ErrFindingsTooLong,
		ErrFindingsRequired,
		ErrInvalidSessionName,
	}

	for i := 0; i < len(allErrors); i++ {
		for j := i + 1; j < len(allErrors); j++ {
			require.NotErrorIs(t, allErrors[i], allErrors[j],
				"expected %v and %v to be distinct", allErrors[i], allErrors[j])
		}
	}
}

func TestErrors_Wrapping(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty_name", input: ""},
		{name: "invalid_chars", input: "bad name!"},
		{name: "leading_slash", input: "/leading"},
		{name: "trailing_slash", input: "trailing/"},
		{name: "consecutive_slashes", input: "a//b"},
		{name: "path_traversal", input: "a/../b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSessionName(tt.input)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrInvalidSessionName)

			// Verify the error can be unwrapped to the sentinel.
			unwrapped := errors.Unwrap(err)
			require.NotNil(t, unwrapped)
			require.ErrorIs(t, unwrapped, ErrInvalidSessionName)
		})
	}
}
