package note

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseFrontmatter_Complete(t *testing.T) {
	content := `---
id: "01HTEST000000000000000001"
title: "API Design Patterns"
project: "backend"
tags:
  - architecture
  - api
created: 2026-03-01T10:00:00Z
modified: 2026-03-02T14:30:00Z
source_url: "https://example.com/article"
---
# API Design Patterns

Some body text here.
`
	fm, body, err := ParseFrontmatter(content)
	require.NoError(t, err)
	require.Equal(t, "01HTEST000000000000000001", fm.ID)
	require.Equal(t, "API Design Patterns", fm.Title)
	require.Equal(t, "backend", fm.Project)
	require.Equal(t, []string{"architecture", "api"}, fm.Tags)
	require.Equal(t, "https://example.com/article", fm.SourceURL)
	require.Equal(t, time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC), fm.Created)
	require.Contains(t, body, "# API Design Patterns")
	require.Contains(t, body, "Some body text here.")
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	content := "# Just a heading\n\nSome text."
	fm, body, err := ParseFrontmatter(content)
	require.NoError(t, err)
	require.Empty(t, fm.ID)
	require.Equal(t, content, body)
}

func TestParseFrontmatter_EmptyFrontmatter(t *testing.T) {
	content := "---\n---\nBody text."
	fm, body, err := ParseFrontmatter(content)
	require.NoError(t, err)
	require.Empty(t, fm.ID)
	require.Equal(t, "Body text.", body)
}

func TestParseFrontmatter_NoClosingDelimiter(t *testing.T) {
	content := "---\nid: test\nno closing"
	fm, body, err := ParseFrontmatter(content)
	require.NoError(t, err)
	// Treated as no frontmatter.
	require.Empty(t, fm.ID)
	require.Equal(t, content, body)
}

func TestParseFrontmatter_MalformedYAML(t *testing.T) {
	content := "---\n[invalid yaml\n---\nBody."
	_, _, err := ParseFrontmatter(content)
	require.Error(t, err)
}

func TestSerializeFrontmatter_RoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	fm := &Frontmatter{
		ID:       "01HTEST000000000000000001",
		Title:    "Test Note",
		Project:  "myproject",
		Tags:     []string{"go", "testing"},
		Created:  now,
		Modified: now,
	}
	body := "# Test Note\n\nSome content.\n"

	serialized, err := SerializeFrontmatter(fm, body)
	require.NoError(t, err)
	require.Contains(t, serialized, "---\n")
	require.Contains(t, serialized, "id: 01HTEST000000000000000001")

	// Parse it back.
	fm2, body2, err := ParseFrontmatter(serialized)
	require.NoError(t, err)
	require.Equal(t, fm.ID, fm2.ID)
	require.Equal(t, fm.Title, fm2.Title)
	require.Equal(t, fm.Project, fm2.Project)
	require.Equal(t, fm.Tags, fm2.Tags)
	require.Equal(t, body, body2)
}

func TestSerializeFrontmatter_EmptyBody(t *testing.T) {
	fm := &Frontmatter{
		ID:       "01HTEST000000000000000001",
		Title:    "Empty Note",
		Created:  time.Now().UTC(),
		Modified: time.Now().UTC(),
	}

	serialized, err := SerializeFrontmatter(fm, "")
	require.NoError(t, err)
	require.Contains(t, serialized, "---\n")
	require.True(t, len(serialized) > 10) // not empty
}

func TestSerializeFrontmatter_TrailingNewline(t *testing.T) {
	fm := &Frontmatter{
		ID:       "01HTEST000000000000000001",
		Title:    "Test",
		Created:  time.Now().UTC(),
		Modified: time.Now().UTC(),
	}

	// Body without trailing newline.
	serialized, err := SerializeFrontmatter(fm, "content without newline")
	require.NoError(t, err)
	require.True(t, serialized[len(serialized)-1] == '\n', "file should end with newline")
}
