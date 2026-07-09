package agent

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/katata/seam/internal/note"
)

// HookPayload mirrors the JSON Claude Code POSTs to a SessionStart HTTP hook.
// We accept it as a struct so the handler can ignore unknown fields cleanly
// and so HookBriefing can specialize on Source ("startup", "resume", "clear",
// "compact").
type HookPayload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	Source         string `json:"source"`
	// AgentType is set for subagent SessionStart events. When non-empty the
	// briefing is trimmed to constraints only (subagents inherit task context
	// from their parent but must still see never-violate constraints).
	AgentType string `json:"agent_type"`
}

// HookSourceCompact is the value Claude Code sends in HookPayload.Source after
// a /compact — the case where a fresh briefing is most valuable, since the
// agent has just lost its prior context.
const HookSourceCompact = "compact"

// hookBriefingPromptInjectionRe matches imperative phrases that an attacker
// might smuggle into a memory or session name to pivot the agent. We strip
// them when assembling the briefing because the briefing is presented to
// the model as data, not user instructions.
var hookBriefingPromptInjectionRe = regexp.MustCompile(
	`(?i)\b(ignore|disregard|from now on|you must|override)\b[^\n]*`,
)

// HookBriefing assembles a short Seam-awareness blurb for the Claude Code
// SessionStart hook. The briefing is a flat plain-text string wrapped in a
// `<seam-briefing>` XML tag — we use the tag so the agent can distinguish
// system-injected context from user input (Claude Code issue #23537).
//
// This is intentionally NOT s.SessionStart: it must run on every Claude Code
// launch and must therefore be free of side effects (no project creation, no
// session row insert, no WebSocket events). It is a read-only synthesis of
// existing data.
//
// openTaskCount is the open-task count computed by the caller (the hook
// handler holds the task service). Pass -1 to omit task counts from the
// briefing entirely; pass 0 for "no open tasks".
//
// maxChars is the soft target length; hardCap is the absolute ceiling (a
// pathological input cannot blow past it). When payload.CWD resolves to a Seam
// project via repo_project_map, the briefing is repo-scoped: pinned
// constraints first, then a per-project memory index, recent findings, and the
// open-task count. Otherwise it falls back to recent sessions + memories with
// a hint that the cwd is unmapped.
func (s *Service) HookBriefing(ctx context.Context, userID string, payload HookPayload, maxChars, hardCap, openTaskCount int) (string, error) {
	if maxChars <= 0 {
		maxChars = 2000
	}
	if hardCap < maxChars {
		hardCap = maxChars
	}

	db, err := s.cfg.UserDBManager.Open(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("agent.Service.HookBriefing: open db: %w", err)
	}

	project := s.ResolveProjectForCWD(ctx, userID, payload.CWD)

	// Unmapped cwd: fall back to the recent-activity briefing.
	if project == "" {
		return s.assembleFallbackBriefing(ctx, userID, db, payload, maxChars, hardCap, openTaskCount), nil
	}

	constraints := s.gatherConstraints(ctx, userID, db, project)

	// Subagents get a constraints-only briefing: they inherit task context
	// from their parent but must still never violate standing constraints.
	if payload.AgentType != "" {
		return assembleSubagentBriefing(project, constraints, hardCap), nil
	}

	index := s.gatherProjectIndex(ctx, userID, db, project)
	findings := s.gatherProjectFindings(ctx, db, project)
	return assembleProjectBriefing(payload, project, constraints, index, findings, openTaskCount, maxChars, hardCap), nil
}

// constraintLine is a pinned standing rule rendered at the top of a briefing.
type constraintLine struct {
	name        string
	description string
}

// findingLine summarizes a completed session's findings.
type findingLine struct {
	name    string
	age     string
	snippet string
}

// agentMemoryProjectID returns the agent-memory project ID without creating it
// (read-only, so HookBriefing stays side-effect free). Returns false when the
// project does not exist yet.
func (s *Service) agentMemoryProjectID(ctx context.Context, userID string) (string, bool) {
	s.projectCacheMu.RLock()
	if id, ok := s.projectCache[userID]; ok {
		s.projectCacheMu.RUnlock()
		return id, true
	}
	s.projectCacheMu.RUnlock()

	p, err := s.cfg.ProjectService.GetBySlug(ctx, userID, AgentMemoryProject)
	if err != nil {
		return "", false
	}
	s.projectCacheMu.Lock()
	s.projectCache[userID] = p.ID
	s.projectCacheMu.Unlock()
	return p.ID, true
}

// gatherConstraints collects constraint memories relevant to a project: the
// project's own constraints plus global (unscoped) constraints that apply
// everywhere. Deduplicated by note ID.
func (s *Service) gatherConstraints(ctx context.Context, userID string, db DBTX, project string) []constraintLine {
	memID, ok := s.agentMemoryProjectID(ctx, userID)
	if !ok {
		return nil
	}

	var out []constraintLine
	seen := map[string]bool{}
	add := func(n *note.Note) {
		if seen[n.ID] {
			return
		}
		cat, name := parseKnowledgeTitle(n.Title)
		if cat != "constraint" {
			return
		}
		seen[n.ID] = true
		out = append(out, constraintLine{name: name, description: n.Description})
	}

	// Project-scoped constraints.
	if notes, _, err := s.cfg.NoteService.List(ctx, userID, note.NoteFilter{
		ProjectID: memID, Tag: "project:" + project, Limit: 200,
	}); err == nil {
		for _, n := range notes {
			add(n)
		}
	}
	// Global constraints: domain:constraint memories with no project tag.
	if notes, _, err := s.cfg.NoteService.List(ctx, userID, note.NoteFilter{
		ProjectID: memID, Tag: "domain:constraint", Limit: 200,
	}); err == nil {
		for _, n := range notes {
			if projectTag(n.Tags) == "" {
				add(n)
			}
		}
	}
	return out
}

// gatherProjectIndex returns the project's memories (excluding constraints,
// which are rendered separately) sorted newest first.
func (s *Service) gatherProjectIndex(ctx context.Context, userID string, db DBTX, project string) []MemoryItem {
	memID, ok := s.agentMemoryProjectID(ctx, userID)
	if !ok {
		return nil
	}
	notes, _, err := s.cfg.NoteService.List(ctx, userID, note.NoteFilter{
		ProjectID: memID, Tag: "project:" + project, Limit: 200,
	})
	if err != nil {
		return nil
	}
	items := make([]MemoryItem, 0, len(notes))
	for _, n := range notes {
		cat, name := parseKnowledgeTitle(n.Title)
		if cat == "" || cat == "constraint" {
			continue
		}
		items = append(items, MemoryItem{
			Category:    cat,
			Name:        name,
			Description: n.Description,
			UpdatedAt:   n.UpdatedAt,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items
}

// gatherProjectFindings returns up to 3 recent completed-session findings.
func (s *Service) gatherProjectFindings(ctx context.Context, db DBTX, project string) []findingLine {
	sessions, err := s.cfg.Store.ListSessionsByProject(ctx, db, StatusCompleted, project, 3)
	if err != nil {
		return nil
	}
	now := time.Now().UTC()
	out := make([]findingLine, 0, len(sessions))
	for _, sess := range sessions {
		if strings.TrimSpace(sess.Findings) == "" {
			continue
		}
		snippet := sess.Findings
		if r := []rune(snippet); len(r) > 200 {
			snippet = string(r[:200])
		}
		out = append(out, findingLine{
			name:    sess.Name,
			age:     humanizeAge(now.Sub(sess.UpdatedAt)),
			snippet: snippet,
		})
	}
	return out
}

// assembleProjectBriefing renders the repo-scoped briefing. Constraints are
// never dropped; the memory index is trimmed (newest kept) to fit the soft
// budget, then findings, and hardCap is the absolute ceiling.
func assembleProjectBriefing(payload HookPayload, project string, constraints []constraintLine, index []MemoryItem, findings []findingLine, openTaskCount, maxChars, hardCap int) string {
	var head strings.Builder
	head.WriteString("<seam-briefing>\n")
	head.WriteString(fmt.Sprintf("Seam project: %s -- %s, %s, %s",
		sanitizeHookField(project),
		pluralize(len(constraints), "constraint", "constraints"),
		pluralize(len(index), "memory", "memories"),
		pluralize(len(findings), "recent finding", "recent findings")))
	if openTaskCount >= 0 {
		head.WriteString(", " + pluralize(openTaskCount, "open task", "open tasks"))
	}
	head.WriteString(".\n")

	// Constraints (always included in full).
	for _, c := range constraints {
		head.WriteString(renderConstraintLine(c))
		head.WriteByte('\n')
	}

	// Fixed trailer sections.
	var tail strings.Builder
	if openTaskCount > 0 {
		tail.WriteString("\n")
		tail.WriteString(fmt.Sprintf("%s. Read tasks with mcp__seam__tasks_list.\n",
			pluralize(openTaskCount, "open task", "open tasks")))
	}
	tail.WriteString("\nRecall on demand with mcp__seam__recall; read a memory with mcp__seam__memory_read.")
	if t := buildHookTrailer(payload, nil); t != "" {
		tail.WriteString("\n" + t)
	}
	tail.WriteString("\n</seam-briefing>")

	// Budget the variable sections (index, findings) against the soft target.
	used := head.Len() + tail.Len()
	var body strings.Builder

	// Memory index, newest first, dropping the tail when over budget.
	dropped := 0
	if len(index) > 0 {
		body.WriteString("\nMemories (" + sanitizeHookField(project) + "):\n")
		used += body.Len()
		for i, m := range index {
			line := renderIndexLine(m) + "\n"
			if used+len(line) > maxChars && i > 0 {
				dropped = len(index) - i
				break
			}
			body.WriteString(line)
			used += len(line)
		}
		if dropped > 0 {
			extra := fmt.Sprintf("- (+%d older -- use mcp__seam__recall)\n", dropped)
			body.WriteString(extra)
			used += len(extra)
		}
	}

	// Recent findings.
	if len(findings) > 0 {
		fhead := "\nRecent findings:\n"
		if used+len(fhead) <= maxChars {
			body.WriteString(fhead)
			used += len(fhead)
			for _, f := range findings {
				line := renderFindingLine(f) + "\n"
				if used+len(line) > maxChars {
					break
				}
				body.WriteString(line)
				used += len(line)
			}
		}
	}

	out := head.String() + body.String() + tail.String()
	return hardTruncateBriefing(out, hardCap)
}

// assembleSubagentBriefing renders a constraints-only briefing for subagents.
func assembleSubagentBriefing(project string, constraints []constraintLine, hardCap int) string {
	var b strings.Builder
	b.WriteString("<seam-briefing>\n")
	if len(constraints) == 0 {
		b.WriteString(fmt.Sprintf("Seam project: %s -- no constraints on record.\n", sanitizeHookField(project)))
	} else {
		b.WriteString(fmt.Sprintf("Seam project: %s -- %s (must not be violated):\n",
			sanitizeHookField(project), pluralize(len(constraints), "constraint", "constraints")))
		for _, c := range constraints {
			b.WriteString(renderConstraintLine(c))
			b.WriteByte('\n')
		}
	}
	b.WriteString("</seam-briefing>")
	return hardTruncateBriefing(b.String(), hardCap)
}

// assembleFallbackBriefing is used when the cwd does not map to a project. It
// mirrors the pre-v2 recent-activity briefing and adds a hint that the cwd is
// unmapped (only when a cwd was actually provided).
func (s *Service) assembleFallbackBriefing(ctx context.Context, userID string, db DBTX, payload HookPayload, maxChars, hardCap, openTaskCount int) string {
	var sessions []*Session
	if list, err := s.cfg.Store.ListSessions(ctx, db, StatusActive, 3, 0); err == nil {
		sessions = list
	}
	var memories []MemoryItem
	if items, err := s.MemoryList(ctx, userID, ""); err == nil {
		sort.Slice(items, func(i, j int) bool {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		})
		if len(items) > 3 {
			items = items[:3]
		}
		memories = items
	}

	_ = maxChars
	return renderFallbackBriefing(payload, sessions, memories, openTaskCount, hardCap)
}

// renderFallbackBriefing is the pure-formatting half of the unmapped-cwd
// briefing: recent sessions + memories, plus an unmapped-cwd hint when a cwd
// was provided. Factored out so unit tests can exercise it without a database.
func renderFallbackBriefing(payload HookPayload, sessions []*Session, memories []MemoryItem, openTaskCount, hardCap int) string {
	var b strings.Builder
	b.WriteString("<seam-briefing>\n")
	b.WriteString(buildHookHeader(sessions, memories, openTaskCount))
	b.WriteByte('\n')
	if line := buildHookSessionsLine(sessions); line != "" {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if line := buildHookMemoriesLine(memories); line != "" {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if payload.CWD != "" {
		b.WriteString(fmt.Sprintf(
			"This repo (%s) has no repo_project_map entry, so the briefing is not project-scoped.\n",
			sanitizeHookField(payload.CWD)))
	}
	b.WriteString(buildHookTrailer(payload, sessions))
	b.WriteByte('\n')
	b.WriteString("</seam-briefing>")
	return hardTruncateBriefing(b.String(), hardCap)
}

func renderConstraintLine(c constraintLine) string {
	name := sanitizeHookField(c.name)
	desc := sanitizeHookFieldN(c.description, 160)
	if desc == "" {
		return "CONSTRAINT: " + name
	}
	return "CONSTRAINT: " + name + ": " + desc
}

func renderIndexLine(m MemoryItem) string {
	label := sanitizeHookField(m.Category + "/" + m.Name)
	prefix := "- "
	if m.Category == "refuted" {
		prefix = "- [REFUTED] "
	}
	desc := sanitizeHookFieldN(m.Description, 160)
	if desc == "" {
		return prefix + label
	}
	return prefix + label + " -- " + desc
}

func renderFindingLine(f findingLine) string {
	name := sanitizeHookField(f.name)
	snippet := sanitizeHookFieldN(f.snippet, 200)
	return fmt.Sprintf("- %s (%s ago): %s", name, f.age, snippet)
}

// hardTruncateBriefing enforces the absolute ceiling, keeping the closing tag.
func hardTruncateBriefing(out string, hardCap int) string {
	if len(out) <= hardCap {
		return out
	}
	const closing = "\n</seam-briefing>"
	const ellipsis = "..."
	bodyBudget := hardCap - len(closing) - len(ellipsis)
	if bodyBudget < 0 {
		bodyBudget = 0
	}
	body := out
	if strings.HasSuffix(out, closing) {
		body = out[:len(out)-len(closing)]
	}
	if bodyBudget < len(body) {
		body = body[:bodyBudget]
	}
	return body + ellipsis + closing
}

func buildHookHeader(sessions []*Session, memories []MemoryItem, openTaskCount int) string {
	sessPart := pluralize(len(sessions), "open session", "open sessions")
	memPart := pluralize(len(memories), "recent memory", "recent memories")
	if openTaskCount < 0 {
		return fmt.Sprintf("Seam: %s, %s.", sessPart, memPart)
	}
	taskPart := pluralize(openTaskCount, "open task", "open tasks")
	return fmt.Sprintf("Seam: %s, %s, %s.", sessPart, memPart, taskPart)
}

func buildHookSessionsLine(sessions []*Session) string {
	if len(sessions) == 0 {
		return ""
	}
	parts := make([]string, 0, len(sessions))
	now := time.Now().UTC()
	for _, sess := range sessions {
		name := sanitizeHookField(sess.Name)
		if name == "" {
			continue
		}
		age := humanizeAge(now.Sub(sess.UpdatedAt))
		parts = append(parts, fmt.Sprintf("%s (active %s ago)", name, age))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Sessions: " + strings.Join(parts, ", ")
}

func buildHookMemoriesLine(memories []MemoryItem) string {
	if len(memories) == 0 {
		return ""
	}
	parts := make([]string, 0, len(memories))
	for _, m := range memories {
		label := sanitizeHookField(m.Category + "/" + m.Name)
		if label == "" {
			continue
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return ""
	}
	return "Memories: " + strings.Join(parts, ", ")
}

func buildHookTrailer(payload HookPayload, sessions []*Session) string {
	if payload.Source == HookSourceCompact && len(sessions) > 0 {
		name := sanitizeHookField(sessions[0].Name)
		if name != "" {
			return fmt.Sprintf(
				"Context was just compacted. If you had session %s in flight, resume with mcp__seam__session_start %s.",
				name, name,
			)
		}
	}
	if payload.Source == HookSourceCompact {
		return "Context was just compacted. Call mcp__seam__session_start <name> if a previous session is relevant."
	}
	return "Call mcp__seam__session_start <name> for a full briefing if this task is non-trivial."
}

// sanitizeHookField scrubs a single field from data we are about to inline
// into the briefing. Memories and session names are user-controlled (and in
// the agent-shared case, other-agent-controlled), so we treat them as data,
// not as prompt instructions: strip newlines and any imperative phrases that
// look like attempts to override the system prompt.
func sanitizeHookField(s string) string {
	return sanitizeHookFieldN(s, 80)
}

// sanitizeHookFieldN is the rune-safe, length-parameterized scrubber. It strips
// newlines and imperative injection phrases, collapses whitespace, and
// truncates to maxRunes runes (never splitting a UTF-8 rune).
func sanitizeHookFieldN(s string, maxRunes int) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = hookBriefingPromptInjectionRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	// Collapse internal whitespace runs.
	s = strings.Join(strings.Fields(s), " ")
	// Avoid letting a single field bloat the briefing.
	if maxRunes > 3 && utf8.RuneCountInString(s) > maxRunes {
		r := []rune(s)
		s = string(r[:maxRunes-3]) + "..."
	}
	return s
}

func pluralize(n int, singular, plural string) string {
	if n == 0 {
		return "no " + plural
	}
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}

func humanizeAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
