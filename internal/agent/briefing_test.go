package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllocateBudget_AllSectionsPresent(t *testing.T) {
	budget := allocateBudget(4000, true, true, true, true)

	// All sections present: 30% session, 20% parent, 25% sibling, 25% knowledge.
	require.Equal(t, 1200, budget.sessionChars)
	require.Equal(t, 800, budget.parentChars)
	require.Equal(t, 1000, budget.siblingChars)
	require.Equal(t, 1000, budget.knowledgeChars)
	require.Equal(t, 4000, budget.sessionChars+budget.parentChars+budget.siblingChars+budget.knowledgeChars)
}

func TestAllocateBudget_NoSessionState(t *testing.T) {
	budget := allocateBudget(4000, false, true, true, true)

	// Session budget (30%) redistributes to the 3 remaining sections.
	require.Equal(t, 0, budget.sessionChars)
	total := budget.parentChars + budget.siblingChars + budget.knowledgeChars
	require.Equal(t, 4000, total)
}

func TestAllocateBudget_OnlyKnowledge(t *testing.T) {
	budget := allocateBudget(4000, false, false, false, true)

	// All budget goes to knowledge.
	require.Equal(t, 0, budget.sessionChars)
	require.Equal(t, 0, budget.parentChars)
	require.Equal(t, 0, budget.siblingChars)
	require.Equal(t, 4000, budget.knowledgeChars)
}

func TestAllocateBudget_NothingPresent(t *testing.T) {
	budget := allocateBudget(4000, false, false, false, false)

	require.Equal(t, 0, budget.sessionChars)
	require.Equal(t, 0, budget.parentChars)
	require.Equal(t, 0, budget.siblingChars)
	require.Equal(t, 0, budget.knowledgeChars)
}

func TestAllocateBudget_SmallBudget(t *testing.T) {
	budget := allocateBudget(100, true, true, true, true)

	total := budget.sessionChars + budget.parentChars + budget.siblingChars + budget.knowledgeChars
	require.Equal(t, 100, total)
}

func TestTruncateToChars_ShortString(t *testing.T) {
	input := "Hello, world!"
	got := truncateToChars(input, 100)
	require.Equal(t, input, got)
}

func TestTruncateToChars_ExactLength(t *testing.T) {
	input := "12345"
	got := truncateToChars(input, 5)
	require.Equal(t, input, got)
}

func TestTruncateToChars_TruncatesAtWordBoundary(t *testing.T) {
	input := "Hello world this is a long string"
	got := truncateToChars(input, 15)
	// Should truncate at a word boundary and add ellipsis.
	require.True(t, len(got) <= 15+3, "truncated string should be within budget + ellipsis")
	require.True(t, len(got) > 0)
}

func TestTruncateToChars_EmptyString(t *testing.T) {
	got := truncateToChars("", 100)
	require.Empty(t, got)
}

func TestTruncateToChars_ZeroBudget(t *testing.T) {
	got := truncateToChars("Hello", 0)
	require.Empty(t, got)
}

func TestBudgetRedistribution(t *testing.T) {
	// When parent plan is empty but other sections have content:
	// parent budget should redistribute to session + sibling + knowledge.
	budget := allocateBudget(4000, true, false, true, true)

	require.Equal(t, 0, budget.parentChars)
	total := budget.sessionChars + budget.siblingChars + budget.knowledgeChars
	require.Equal(t, 4000, total)

	// Session should get more than its default 30% (parent's 20% redistributed).
	require.True(t, budget.sessionChars > 1200, "session should get redistributed budget")
}

func TestBudgetRedistribution_OnlySiblings(t *testing.T) {
	budget := allocateBudget(3000, false, false, true, false)

	require.Equal(t, 0, budget.sessionChars)
	require.Equal(t, 0, budget.parentChars)
	require.Equal(t, 3000, budget.siblingChars)
	require.Equal(t, 0, budget.knowledgeChars)
}

// --- truncateSiblings tests ---

func TestTruncateSiblings_SingleSibling_FitsInBudget(t *testing.T) {
	siblings := []SiblingFinding{
		{SessionName: "task/analyze", Findings: "Found 3 patterns"},
	}
	result := truncateSiblings(siblings, 1000)

	require.Len(t, result, 1)
	require.Equal(t, "task/analyze", result[0].SessionName)
	require.Equal(t, "Found 3 patterns", result[0].Findings)
}

func TestTruncateSiblings_MultipleSiblings_AllFit(t *testing.T) {
	siblings := []SiblingFinding{
		{SessionName: "task/a", Findings: "Result A"},
		{SessionName: "task/b", Findings: "Result B"},
		{SessionName: "task/c", Findings: "Result C"},
	}
	result := truncateSiblings(siblings, 5000)

	require.Len(t, result, 3)
	for i, sib := range siblings {
		require.Equal(t, sib.SessionName, result[i].SessionName)
		require.Equal(t, sib.Findings, result[i].Findings)
	}
}

func TestTruncateSiblings_BudgetExhausted_TruncatesLater(t *testing.T) {
	siblings := []SiblingFinding{
		{SessionName: "task/a", Findings: "Short result A"},
		{SessionName: "task/b", Findings: "Short result B"},
		{SessionName: "task/c", Findings: "This should be dropped"},
	}
	// Calculate budget that fits first two but not the third.
	// "task/a: " = 8 chars, "Short result A" = 14 chars => 22 total
	// "task/b: " = 8 chars, "Short result B" = 14 chars => 22 total
	// Total for first two: 44 chars. Give budget that is tight.
	budget := len("task/a: ") + len("Short result A") + len("task/b: ") + len("Short result B") + 1
	result := truncateSiblings(siblings, budget)

	require.Len(t, result, 2)
	require.Equal(t, "task/a", result[0].SessionName)
	require.Equal(t, "task/b", result[1].SessionName)
}

func TestTruncateSiblings_VerySmallBudget(t *testing.T) {
	siblings := []SiblingFinding{
		{SessionName: "task/analyze-middleware", Findings: "Found patterns"},
	}
	// Budget smaller than the header "task/analyze-middleware: " (24 chars).
	result := truncateSiblings(siblings, 5)

	require.Empty(t, result)
}

func TestTruncateSiblings_EmptySiblings(t *testing.T) {
	result := truncateSiblings([]SiblingFinding{}, 1000)
	require.Nil(t, result)
}

func TestTruncateSiblings_ZeroBudget(t *testing.T) {
	siblings := []SiblingFinding{
		{SessionName: "task/a", Findings: "data"},
	}
	result := truncateSiblings(siblings, 0)
	require.Nil(t, result)
}

func TestTruncateSiblings_FindingsTruncated(t *testing.T) {
	longFindings := strings.Repeat("word ", 200) // 1000 chars
	siblings := []SiblingFinding{
		{SessionName: "task/a", Findings: longFindings},
	}
	// Budget: header "task/a: " = 8 chars + 50 chars for findings = 58 total.
	budget := len("task/a: ") + 50
	result := truncateSiblings(siblings, budget)

	require.Len(t, result, 1)
	require.Equal(t, "task/a", result[0].SessionName)
	// Findings should be truncated and end with "...".
	require.True(t, len(result[0].Findings) < len(longFindings),
		"findings should be truncated")
	require.True(t, strings.HasSuffix(result[0].Findings, "..."),
		"truncated findings should end with ellipsis")
}

// --- truncateKnowledge tests ---

func TestTruncateKnowledge_SingleHit_FitsInBudget(t *testing.T) {
	hits := []KnowledgeHit{
		{Title: "Knowledge: go - patterns", Snippet: "Use interfaces for decoupling", Score: 0.9},
	}
	result := truncateKnowledge(hits, 5000)

	require.Len(t, result, 1)
	require.Equal(t, "Knowledge: go - patterns", result[0].Title)
	require.Equal(t, "Use interfaces for decoupling", result[0].Snippet)
	require.Equal(t, 0.9, result[0].Score)
}

func TestTruncateKnowledge_MultipleHits_BudgetExhausted(t *testing.T) {
	hits := []KnowledgeHit{
		{Title: "hit-a", Snippet: "Snippet A"},
		{Title: "hit-b", Snippet: "Snippet B"},
		{Title: "hit-c", Snippet: "Snippet C that should be dropped"},
	}
	// Budget enough for first two hits only.
	budget := len("hit-a: ") + len("Snippet A") + len("hit-b: ") + len("Snippet B") + 1
	result := truncateKnowledge(hits, budget)

	require.Len(t, result, 2)
	require.Equal(t, "hit-a", result[0].Title)
	require.Equal(t, "hit-b", result[1].Title)
}

func TestTruncateKnowledge_VerySmallBudget(t *testing.T) {
	hits := []KnowledgeHit{
		{Title: "Knowledge: go - middleware-patterns", Snippet: "content"},
	}
	// Budget smaller than the header.
	result := truncateKnowledge(hits, 5)
	require.Empty(t, result)
}

func TestTruncateKnowledge_EmptyHits(t *testing.T) {
	result := truncateKnowledge([]KnowledgeHit{}, 1000)
	require.Nil(t, result)
}

func TestTruncateKnowledge_ZeroBudget(t *testing.T) {
	hits := []KnowledgeHit{
		{Title: "hit", Snippet: "data"},
	}
	result := truncateKnowledge(hits, 0)
	require.Nil(t, result)
}

func TestTruncateKnowledge_SnippetTruncated(t *testing.T) {
	longSnippet := strings.Repeat("token ", 200) // 1200 chars
	hits := []KnowledgeHit{
		{Title: "topic", Snippet: longSnippet, Score: 0.85},
	}
	budget := len("topic: ") + 60
	result := truncateKnowledge(hits, budget)

	require.Len(t, result, 1)
	require.Equal(t, "topic", result[0].Title)
	require.True(t, len(result[0].Snippet) < len(longSnippet),
		"snippet should be truncated")
	require.True(t, strings.HasSuffix(result[0].Snippet, "..."),
		"truncated snippet should end with ellipsis")
	require.Equal(t, 0.85, result[0].Score)
}

// --- parseKnowledgeTitle tests ---

func TestParseKnowledgeTitle_Valid(t *testing.T) {
	cat, name := parseKnowledgeTitle("Knowledge: go - middleware-patterns")
	require.Equal(t, "go", cat)
	require.Equal(t, "middleware-patterns", name)
}

func TestParseKnowledgeTitle_NoDash(t *testing.T) {
	cat, name := parseKnowledgeTitle("Knowledge: single-value")
	require.Equal(t, "", cat)
	require.Equal(t, "single-value", name)
}

func TestParseKnowledgeTitle_NotKnowledge(t *testing.T) {
	cat, name := parseKnowledgeTitle("Session Plan: test")
	require.Equal(t, "", cat)
	require.Equal(t, "Session Plan: test", name)
}

func TestParseKnowledgeTitle_Empty(t *testing.T) {
	cat, name := parseKnowledgeTitle("")
	require.Equal(t, "", cat)
	require.Equal(t, "", name)
}

func TestParseKnowledgeTitle_MultipleDashes(t *testing.T) {
	cat, name := parseKnowledgeTitle("Knowledge: go - middleware - patterns")
	require.Equal(t, "go", cat)
	require.Equal(t, "middleware - patterns", name)
}

// --- formatProgressEntry tests ---

func TestFormatProgressEntry_WithNotes(t *testing.T) {
	got := formatProgressEntry("Analyze middleware", "completed", "Found 3 patterns")
	require.Equal(t, "[completed] Analyze middleware: Found 3 patterns", got)
}

func TestFormatProgressEntry_WithoutNotes(t *testing.T) {
	got := formatProgressEntry("Start task", "in_progress", "")
	require.Equal(t, "[in_progress] Start task", got)
}

func TestFormatProgressEntry_EmptyTaskAndStatus(t *testing.T) {
	got := formatProgressEntry("", "", "")
	require.Equal(t, "[] ", got)
}

// --- Additional allocateBudget edge cases ---

func TestAllocateBudget_NegativeBudget(t *testing.T) {
	budget := allocateBudget(-100, true, true, true, true)

	require.Equal(t, 0, budget.sessionChars)
	require.Equal(t, 0, budget.parentChars)
	require.Equal(t, 0, budget.siblingChars)
	require.Equal(t, 0, budget.knowledgeChars)
}

func TestAllocateBudget_OnlyTwoSections(t *testing.T) {
	budget := allocateBudget(4000, true, false, false, true)

	require.Equal(t, 0, budget.parentChars)
	require.Equal(t, 0, budget.siblingChars)
	require.True(t, budget.sessionChars > 0, "session should have budget")
	require.True(t, budget.knowledgeChars > 0, "knowledge should have budget")
	total := budget.sessionChars + budget.knowledgeChars
	require.Equal(t, 4000, total, "redistributed budget should sum to maxChars")
}

// --- Additional truncateToChars edge cases ---

func TestTruncateToChars_SingleWord(t *testing.T) {
	input := "Superlongwordwithoutspaces"
	got := truncateToChars(input, 10)
	// No space found in first 10 chars, so truncation cannot break at word boundary.
	// Should still truncate and add "...".
	require.True(t, len(got) <= 13, "should be at most budget + ellipsis length")
	require.True(t, strings.HasSuffix(got, "..."), "should end with ellipsis")
}

func TestTruncateToChars_NegativeBudget(t *testing.T) {
	got := truncateToChars("Hello world", -5)
	require.Empty(t, got)
}
