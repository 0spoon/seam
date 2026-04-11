package agent

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
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
// maxChars is the soft target length. The output is hard-capped at
// approximately maxChars*3 (or maxChars+1000, whichever is larger) so a
// pathological input cannot blow past the configured budget.
func (s *Service) HookBriefing(ctx context.Context, userID string, payload HookPayload, maxChars, openTaskCount int) (string, error) {
	if maxChars <= 0 {
		maxChars = 500
	}

	hardCap := maxChars * 3
	if minCap := maxChars + 1000; minCap > hardCap {
		hardCap = minCap
	}

	db, err := s.cfg.UserDBManager.Open(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("agent.Service.HookBriefing: open db: %w", err)
	}

	// Sessions: last 3 active sessions, most recently touched first.
	var sessions []*Session
	if list, err := s.cfg.Store.ListSessions(ctx, db, StatusActive, 3, 0); err == nil {
		sessions = list
	}

	// Memories: 3 most recent, any category. MemoryList may return them in
	// any order, so we sort by UpdatedAt DESC and slice to 3.
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

	out := assembleHookBriefing(payload, sessions, memories, openTaskCount, maxChars, hardCap)
	return out, nil
}

// assembleHookBriefing is the pure-formatting half of HookBriefing, factored
// out so unit tests can exercise it without spinning up a database. It is
// safe to call with any combination of nil/empty inputs.
func assembleHookBriefing(payload HookPayload, sessions []*Session, memories []MemoryItem, openTaskCount, maxChars, hardCap int) string {
	var b strings.Builder
	b.WriteString("<seam-briefing>\n")

	header := buildHookHeader(sessions, memories, openTaskCount)
	b.WriteString(header)
	b.WriteByte('\n')

	if line := buildHookSessionsLine(sessions); line != "" {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if line := buildHookMemoriesLine(memories); line != "" {
		b.WriteString(line)
		b.WriteByte('\n')
	}

	b.WriteString(buildHookTrailer(payload, sessions))
	b.WriteByte('\n')
	b.WriteString("</seam-briefing>")

	out := b.String()

	if len(out) > hardCap {
		// Truncate the body, keeping the closing tag intact.
		const closing = "\n</seam-briefing>"
		const ellipsis = "..."
		bodyBudget := hardCap - len(closing) - len(ellipsis)
		if bodyBudget < 0 {
			bodyBudget = 0
		}
		body := out[:len(out)-len(closing)]
		if bodyBudget < len(body) {
			body = body[:bodyBudget]
		}
		out = body + ellipsis + closing
	}

	// maxChars is a soft target; we don't enforce it here unless it would
	// shrink the output. The caller chose the soft/hard split.
	_ = maxChars

	return out
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
		label := m.Category + "/" + m.Name
		label = sanitizeHookField(label)
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
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = hookBriefingPromptInjectionRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	// Collapse internal whitespace runs.
	s = strings.Join(strings.Fields(s), " ")
	// Avoid letting a single field bloat the briefing.
	if len(s) > 80 {
		s = s[:77] + "..."
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
