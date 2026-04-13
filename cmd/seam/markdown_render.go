package main

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
)

// renderMarkdown converts a markdown body into a styled multi-line string
// suitable for a read-only preview inside the TUI editor. It is not a
// spec-compliant renderer; it targets the common subset that makes notes
// skimmable: ATX headings, bullet and ordered lists, blockquotes, fenced
// code blocks, horizontal rules, and the inline marks **bold**, *italic*,
// `code`, and [label](url). Frontmatter is stripped. Width is used to size
// dividers; soft wrapping is left to the enclosing viewport.
func renderMarkdown(body string, width int) string {
	if width < 10 {
		width = 10
	}
	body = stripFrontmatter(body)

	h1 := lipgloss.NewStyle().Foreground(activeTheme.Primary).Bold(true)
	h2 := lipgloss.NewStyle().Foreground(activeTheme.Secondary).Bold(true)
	h3 := lipgloss.NewStyle().Foreground(activeTheme.Fg).Bold(true)
	quoteBar := lipgloss.NewStyle().Foreground(activeTheme.Secondary)
	quoteText := lipgloss.NewStyle().Foreground(activeTheme.Muted).Italic(true)
	code := lipgloss.NewStyle().Foreground(activeTheme.Secondary).Background(activeTheme.HeaderBg)
	codeBlock := lipgloss.NewStyle().
		Foreground(activeTheme.Secondary).
		Background(activeTheme.HeaderBg)
	bullet := lipgloss.NewStyle().Foreground(activeTheme.Primary).Bold(true)
	text := lipgloss.NewStyle().Foreground(activeTheme.Fg)
	divider := lipgloss.NewStyle().Foreground(activeTheme.Border)

	var out []string
	inCode := false
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimRight(line, " \t")

		if strings.HasPrefix(strings.TrimSpace(trimmed), "```") {
			inCode = !inCode
			out = append(out, divider.Render(strings.Repeat("─", width)))
			continue
		}
		if inCode {
			out = append(out, codeBlock.Render(" "+trimmed))
			continue
		}

		if trimmed == "" {
			out = append(out, "")
			continue
		}

		if reHR.MatchString(trimmed) {
			out = append(out, divider.Render(strings.Repeat("─", width)))
			continue
		}

		if m := reHeading.FindStringSubmatch(trimmed); m != nil {
			level := len(m[1])
			content := renderInline(strings.TrimSpace(m[2]), code)
			switch level {
			case 1:
				underline := divider.Render(strings.Repeat("═", width))
				out = append(out, "", h1.Render("▌ "+content), underline)
			case 2:
				out = append(out, "", h2.Render("▌ "+content))
			default:
				out = append(out, h3.Render("▸ "+content))
			}
			continue
		}

		if m := reBlockquote.FindStringSubmatch(trimmed); m != nil {
			content := renderInline(m[1], code)
			out = append(out, quoteBar.Render("│ ")+quoteText.Render(content))
			continue
		}

		if m := reBullet.FindStringSubmatch(line); m != nil {
			indent := m[1]
			content := renderInline(m[2], code)
			out = append(out, indent+bullet.Render("• ")+text.Render(content))
			continue
		}

		if m := reOrdered.FindStringSubmatch(line); m != nil {
			indent := m[1]
			num := m[2]
			content := renderInline(m[3], code)
			out = append(out, indent+bullet.Render(num+". ")+text.Render(content))
			continue
		}

		out = append(out, text.Render(renderInline(trimmed, code)))
	}

	return strings.Join(out, "\n")
}

var (
	reHeading    = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
	reBlockquote = regexp.MustCompile(`^>\s?(.*)$`)
	reBullet     = regexp.MustCompile(`^(\s*)[-*+]\s+(.+)$`)
	reOrdered    = regexp.MustCompile(`^(\s*)(\d+)\.\s+(.+)$`)
	reHR         = regexp.MustCompile(`^(-{3,}|\*{3,}|_{3,})$`)
	reInlineBold = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reInlineEm   = regexp.MustCompile(`(?:^|[^*])\*([^*]+)\*`)
	reInlineCode = regexp.MustCompile("`([^`]+)`")
	reInlineLink = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
)

// renderInline applies inline marks (code, bold, italic, links) to a single
// line. Code spans are substituted first so later passes do not touch their
// contents. Styling uses Render on just the inner text so surrounding words
// keep the body's foreground color.
func renderInline(s string, codeStyle lipgloss.Style) string {
	boldStyle := lipgloss.NewStyle().Foreground(activeTheme.Fg).Bold(true)
	italicStyle := lipgloss.NewStyle().Foreground(activeTheme.Fg).Italic(true)
	linkStyle := lipgloss.NewStyle().Foreground(activeTheme.Primary).Underline(true)

	s = reInlineCode.ReplaceAllStringFunc(s, func(match string) string {
		inner := match[1 : len(match)-1]
		return codeStyle.Render(" " + inner + " ")
	})
	s = reInlineLink.ReplaceAllStringFunc(s, func(match string) string {
		sub := reInlineLink.FindStringSubmatch(match)
		if len(sub) != 3 {
			return match
		}
		return linkStyle.Render(sub[1])
	})
	s = reInlineBold.ReplaceAllStringFunc(s, func(match string) string {
		inner := match[2 : len(match)-2]
		return boldStyle.Render(inner)
	})
	s = reInlineEm.ReplaceAllStringFunc(s, func(match string) string {
		start := 0
		if !strings.HasPrefix(match, "*") {
			start = 1
		}
		inner := match[start+1 : len(match)-1]
		prefix := ""
		if start == 1 {
			prefix = string(match[0])
		}
		return prefix + italicStyle.Render(inner)
	})
	return s
}

// stripFrontmatter removes a leading YAML frontmatter block (between `---`
// fences) before rendering, since notes stored on disk include it but users
// viewing the preview do not want to see it.
func stripFrontmatter(body string) string {
	if !strings.HasPrefix(body, "---\n") && !strings.HasPrefix(body, "---\r\n") {
		return body
	}
	rest := strings.TrimPrefix(body, "---\n")
	rest = strings.TrimPrefix(rest, "---\r\n")
	_, after, ok := strings.Cut(rest, "\n---")
	if !ok {
		return body
	}
	after = strings.TrimPrefix(after, "\n")
	after = strings.TrimPrefix(after, "\r\n")
	return after
}
