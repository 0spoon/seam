package ai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTagSuggestions_ValidJSON(t *testing.T) {
	raw := `[{"name": "golang", "confidence": 0.9}, {"name": "testing", "confidence": 0.7}]`
	existing := []string{"golang", "testing", "devops"}

	result, err := parseTagSuggestions(raw, existing)
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.Equal(t, "golang", result[0].Name)
	require.InDelta(t, 0.9, result[0].Confidence, 0.01)
	require.Equal(t, "testing", result[1].Name)
}

func TestParseTagSuggestions_CodeFence(t *testing.T) {
	raw := "```json\n[{\"name\": \"golang\", \"confidence\": 0.85}]\n```"
	existing := []string{"golang"}

	result, err := parseTagSuggestions(raw, existing)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "golang", result[0].Name)
}

func TestParseTagSuggestions_FiltersInvalidTags(t *testing.T) {
	raw := `[{"name": "golang", "confidence": 0.9}, {"name": "nonexistent", "confidence": 0.8}]`
	existing := []string{"golang"}

	result, err := parseTagSuggestions(raw, existing)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "golang", result[0].Name)
}

func TestParseTagSuggestions_FiltersLowConfidence(t *testing.T) {
	raw := `[{"name": "golang", "confidence": 0.3}]`
	existing := []string{"golang"}

	result, err := parseTagSuggestions(raw, existing)
	require.NoError(t, err)
	require.Empty(t, result)
}

func TestParseTagSuggestions_CaseInsensitive(t *testing.T) {
	raw := `[{"name": "GoLang", "confidence": 0.9}]`
	existing := []string{"golang"}

	result, err := parseTagSuggestions(raw, existing)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "golang", result[0].Name)
}

func TestParseProjectSuggestions_ValidJSON(t *testing.T) {
	raw := `[{"id": "proj1", "name": "Research", "confidence": 0.85}]`
	projects := []ProjectInfo{
		{ID: "proj1", Name: "Research", Description: "Research notes"},
	}

	result, err := parseProjectSuggestions(raw, projects)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "proj1", result[0].ID)
	require.Equal(t, "Research", result[0].Name)
}

func TestParseProjectSuggestions_FiltersUnknownProjects(t *testing.T) {
	raw := `[{"id": "proj1", "name": "Research", "confidence": 0.85}, {"id": "unknown", "name": "Fake", "confidence": 0.9}]`
	projects := []ProjectInfo{
		{ID: "proj1", Name: "Research", Description: "Research notes"},
	}

	result, err := parseProjectSuggestions(raw, projects)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "proj1", result[0].ID)
}

func TestExtractJSON_PlainArray(t *testing.T) {
	result := extractJSON(`[{"name": "test"}]`)
	require.Equal(t, `[{"name": "test"}]`, result)
}

func TestExtractJSON_WithSurroundingText(t *testing.T) {
	result := extractJSON(`Here are the results: [{"name": "test"}] hope this helps!`)
	require.Equal(t, `[{"name": "test"}]`, result)
}

func TestExtractJSON_CodeFence(t *testing.T) {
	result := extractJSON("```json\n[{\"name\": \"test\"}]\n```")
	require.Equal(t, `[{"name": "test"}]`, result)
}

func TestParseTagSuggestions_EmptyInput(t *testing.T) {
	result, err := parseTagSuggestions(`[]`, []string{"golang"})
	require.NoError(t, err)
	require.Empty(t, result)
}

func TestParseProjectSuggestions_CapsConfidence(t *testing.T) {
	raw := `[{"id": "proj1", "name": "Research", "confidence": 1.5}]`
	projects := []ProjectInfo{
		{ID: "proj1", Name: "Research", Description: ""},
	}

	result, err := parseProjectSuggestions(raw, projects)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.InDelta(t, 1.0, result[0].Confidence, 0.01)
}
