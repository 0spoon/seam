package note

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTags_InlineTags(t *testing.T) {
	body := "This is about #architecture and #api design."
	tags := ParseTags(body, nil)
	require.Equal(t, []string{"api", "architecture"}, tags)
}

func TestParseTags_FrontmatterTags(t *testing.T) {
	tags := ParseTags("No inline tags.", []string{"Go", "Testing"})
	require.Equal(t, []string{"go", "testing"}, tags)
}

func TestParseTags_MergedAndDeduplicated(t *testing.T) {
	body := "This is about #architecture and #go stuff."
	tags := ParseTags(body, []string{"Go", "api"})
	require.Equal(t, []string{"api", "architecture", "go"}, tags)
}

func TestParseTags_IgnoredInCodeBlock(t *testing.T) {
	body := "Normal #real-tag\n```\n#code-tag\n```\nMore text."
	tags := ParseTags(body, nil)
	require.Equal(t, []string{"real-tag"}, tags)
}

func TestParseTags_IgnoredInInlineCode(t *testing.T) {
	body := "Use `#not-a-tag` but #real-tag is fine."
	tags := ParseTags(body, nil)
	require.Equal(t, []string{"real-tag"}, tags)
}

func TestParseTags_IgnoredInHeadings(t *testing.T) {
	body := "## Heading\n\nText with #valid-tag."
	tags := ParseTags(body, nil)
	require.Equal(t, []string{"valid-tag"}, tags)
}

func TestParseTags_IgnoredInURLs(t *testing.T) {
	body := "Visit https://example.com/page#section but #real-tag is fine."
	tags := ParseTags(body, nil)
	require.Equal(t, []string{"real-tag"}, tags)
}

func TestParseTags_NoTags(t *testing.T) {
	tags := ParseTags("No tags here.", nil)
	require.Empty(t, tags)
}

func TestParseTags_TagWithHyphensAndUnderscores(t *testing.T) {
	body := "#my-tag #another_tag #simple"
	tags := ParseTags(body, nil)
	require.Equal(t, []string{"another_tag", "my-tag", "simple"}, tags)
}

func TestParseTags_Lowercased(t *testing.T) {
	body := "#Architecture #API"
	tags := ParseTags(body, nil)
	require.Equal(t, []string{"api", "architecture"}, tags)
}
