package agent

import (
	"strings"
	"unicode/utf8"
)

// budgetAllocation holds the character budget for each briefing section.
type budgetAllocation struct {
	sessionChars   int
	parentChars    int
	siblingChars   int
	knowledgeChars int
}

// Default budget percentages (out of 100).
const (
	sessionPercent   = 30
	parentPercent    = 20
	siblingPercent   = 25
	knowledgePercent = 25
)

// allocateBudget distributes maxChars across the four briefing sections.
// Empty sections (hasX = false) have their budget redistributed proportionally
// to the remaining non-empty sections.
func allocateBudget(maxChars int, hasSession, hasParent, hasSiblings, hasKnowledge bool) budgetAllocation {
	if maxChars <= 0 {
		return budgetAllocation{}
	}

	type section struct {
		present bool
		pct     int
		chars   *int
	}

	alloc := budgetAllocation{}
	sections := []section{
		{present: hasSession, pct: sessionPercent, chars: &alloc.sessionChars},
		{present: hasParent, pct: parentPercent, chars: &alloc.parentChars},
		{present: hasSiblings, pct: siblingPercent, chars: &alloc.siblingChars},
		{present: hasKnowledge, pct: knowledgePercent, chars: &alloc.knowledgeChars},
	}

	// Calculate total percentage of present sections.
	totalPresentPct := 0
	for _, s := range sections {
		if s.present {
			totalPresentPct += s.pct
		}
	}

	if totalPresentPct == 0 {
		return budgetAllocation{}
	}

	// Distribute budget proportionally among present sections.
	assigned := 0
	lastIdx := -1
	for i, s := range sections {
		if !s.present {
			continue
		}
		lastIdx = i
		*s.chars = maxChars * s.pct / totalPresentPct
		assigned += *s.chars
	}

	// Assign any remainder from integer division to the last present section.
	if lastIdx >= 0 {
		*sections[lastIdx].chars += maxChars - assigned
	}

	return alloc
}

// truncateToChars truncates text to at most maxChars characters (runes),
// trying to break at a word boundary. Appends "..." if truncated, with
// the ellipsis counted within the budget.
func truncateToChars(text string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	if utf8.RuneCountInString(text) <= maxChars {
		return text
	}

	// Reserve 3 chars for "..." suffix.
	cutAt := maxChars - 3
	if cutAt <= 0 {
		// Budget too small for meaningful truncation.
		return string([]rune(text)[:maxChars])
	}

	runes := []rune(text)
	truncated := string(runes[:cutAt])

	// Try to break at a word boundary.
	lastSpace := strings.LastIndex(truncated, " ")
	if lastSpace > cutAt/2 {
		truncated = truncated[:lastSpace]
	}

	return truncated + "..."
}
