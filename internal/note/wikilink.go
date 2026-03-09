package note

import (
	"regexp"
	"strings"
)

// Link represents a parsed wikilink from a note body.
type Link struct {
	Target  string // the link target text (before "|" if aliased)
	Display string // the display text (after "|"), empty if no alias
}

// wikilinkRe matches [[target]] and [[target|display]].
var wikilinkRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// ParseWikilinks extracts all wikilinks from a note body, skipping those
// inside code blocks (fenced ``` and inline `).
func ParseWikilinks(body string) []Link {
	// Remove code blocks before parsing.
	cleaned := removeCodeBlocks(body)

	matches := wikilinkRe.FindAllStringSubmatch(cleaned, -1)
	if matches == nil {
		return nil
	}

	var links []Link
	seen := make(map[string]bool)

	for _, match := range matches {
		inner := match[1]
		var link Link

		if idx := strings.Index(inner, "|"); idx != -1 {
			link.Target = strings.TrimSpace(inner[:idx])
			link.Display = strings.TrimSpace(inner[idx+1:])
		} else {
			link.Target = strings.TrimSpace(inner)
		}

		if link.Target == "" {
			continue
		}

		// Deduplicate by target+display.
		key := link.Target + "|" + link.Display
		if seen[key] {
			continue
		}
		seen[key] = true
		links = append(links, link)
	}

	return links
}

// fencedCodeRe matches fenced code blocks (``` ... ```).
var fencedCodeRe = regexp.MustCompile("(?s)```[^\n]*\n.*?```")

// inlineCodeRe matches inline code (`...`).
var inlineCodeRe = regexp.MustCompile("`[^`]+`")

// removeCodeBlocks removes fenced code blocks and inline code from text.
func removeCodeBlocks(text string) string {
	// Remove fenced blocks first (they may contain inline code).
	text = fencedCodeRe.ReplaceAllString(text, "")
	// Remove inline code.
	text = inlineCodeRe.ReplaceAllString(text, "")
	return text
}
