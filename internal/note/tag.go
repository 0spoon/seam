package note

import (
	"regexp"
	"sort"
	"strings"
)

// tagRe matches inline #tags. A tag must be preceded by whitespace or start of line,
// and consists of alphanumeric characters, underscores, and hyphens.
var tagRe = regexp.MustCompile(`(?:^|\s)#([a-zA-Z0-9][a-zA-Z0-9_-]*)`)

// headingRe matches markdown headings (## Heading is NOT a tag).
var headingRe = regexp.MustCompile(`(?m)^#{1,6}\s`)

// urlRe matches HTTP/HTTPS URLs for removal before tag scanning.
var urlRe = regexp.MustCompile(`https?://\S+`)

// ParseTags extracts unique tags from a note body, ignoring tags inside
// code blocks, URLs, and markdown headings. Merges with any frontmatter tags.
// Returns a sorted, deduplicated, lowercased list.
func ParseTags(body string, frontmatterTags []string) []string {
	seen := make(map[string]bool)
	var tags []string

	// Add frontmatter tags first.
	for _, t := range frontmatterTags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			tags = append(tags, t)
		}
	}

	// Remove code blocks.
	cleaned := removeCodeBlocks(body)

	// Remove URLs before tag scanning to avoid false positives from URL fragments.
	cleaned = urlRe.ReplaceAllString(cleaned, "")

	// Process line by line to skip headings.
	for _, line := range strings.Split(cleaned, "\n") {
		// Skip heading lines (## Heading).
		if headingRe.MatchString(line) {
			continue
		}

		// Find all tag matches in this line.
		matches := tagRe.FindAllStringSubmatch(line, -1)
		for _, match := range matches {
			tag := strings.ToLower(match[1])
			if !seen[tag] {
				seen[tag] = true
				tags = append(tags, tag)
			}
		}
	}

	sort.Strings(tags)
	return tags
}
