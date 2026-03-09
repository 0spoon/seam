package note

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseWikilinks_BasicLink(t *testing.T) {
	links := ParseWikilinks("Check out [[API Design]] for more info.")
	require.Len(t, links, 1)
	require.Equal(t, "API Design", links[0].Target)
	require.Empty(t, links[0].Display)
}

func TestParseWikilinks_AliasedLink(t *testing.T) {
	links := ParseWikilinks("See [[api-design|API Design Patterns]] for details.")
	require.Len(t, links, 1)
	require.Equal(t, "api-design", links[0].Target)
	require.Equal(t, "API Design Patterns", links[0].Display)
}

func TestParseWikilinks_MultipleLinks(t *testing.T) {
	links := ParseWikilinks("Link to [[Note A]] and [[Note B]] and [[Note C]].")
	require.Len(t, links, 3)
	require.Equal(t, "Note A", links[0].Target)
	require.Equal(t, "Note B", links[1].Target)
	require.Equal(t, "Note C", links[2].Target)
}

func TestParseWikilinks_Deduplicate(t *testing.T) {
	links := ParseWikilinks("See [[API Design]] and also [[API Design]] again.")
	require.Len(t, links, 1)
}

func TestParseWikilinks_InFencedCodeBlock(t *testing.T) {
	body := "Normal text [[real-link]]\n```\n[[inside-code]]\n```\nMore text."
	links := ParseWikilinks(body)
	require.Len(t, links, 1)
	require.Equal(t, "real-link", links[0].Target)
}

func TestParseWikilinks_InInlineCode(t *testing.T) {
	body := "Use `[[not-a-link]]` in code, but [[real-link]] is real."
	links := ParseWikilinks(body)
	require.Len(t, links, 1)
	require.Equal(t, "real-link", links[0].Target)
}

func TestParseWikilinks_Empty(t *testing.T) {
	links := ParseWikilinks("No links here.")
	require.Nil(t, links)
}

func TestParseWikilinks_EmptyBrackets(t *testing.T) {
	links := ParseWikilinks("Empty [[]] brackets.")
	require.Empty(t, links)
}

func TestParseWikilinks_WhitespaceOnly(t *testing.T) {
	links := ParseWikilinks("Whitespace [[  ]] only.")
	require.Empty(t, links)
}
