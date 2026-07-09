package agent

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"
)

// RecallHit is a single unified-recall result with provenance and staleness.
type RecallHit struct {
	Kind      string    `json:"kind"` // "memory" | "note" | "session" | "trial"
	Key       string    `json:"key"`  // memory: "category/name"; note: note ID; session: name
	Title     string    `json:"title"`
	Snippet   string    `json:"snippet"`
	Project   string    `json:"project,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	Age       string    `json:"age,omitempty"` // humanized, e.g. "3d"
	Source    string    `json:"source"`        // "semantic" | "fts" | "lexical"
	Score     float64   `json:"score"`
}

const (
	recallDefaultMaxChars = 3000
	recallPerScopeLimit   = 10
	recallSessionScanCap  = 200
)

// recallItem wraps a RecallHit with fields needed only during assembly.
type recallItem struct {
	RecallHit
	noteID string
	prio   int
}

// recallKindPriority orders kinds on score ties (memory > session/trial > note).
func recallKindPriority(kind string) int {
	switch kind {
	case "memory":
		return 0
	case "session", "trial":
		return 1
	default:
		return 2
	}
}

// Recall is the unified discovery entrypoint across agent memories, user notes,
// session findings, and lab trials. scope is one of all|memories|notes|sessions.
func (s *Service) Recall(ctx context.Context, userID, query, scope, projectSlug string, maxChars int) ([]RecallHit, error) {
	if maxChars <= 0 {
		maxChars = recallDefaultMaxChars
	}
	if scope == "" {
		scope = "all"
	}
	now := time.Now().UTC()

	var items []recallItem

	if scope == "all" || scope == "memories" {
		for _, h := range s.searchKnowledgeScoped(ctx, userID, query, "agent", recallPerScopeLimit, 0.3) {
			cat, name := parseKnowledgeTitle(h.Title)
			if strings.HasPrefix(h.Title, "Knowledge: ") && cat != "" {
				items = append(items, recallItem{
					RecallHit: RecallHit{Kind: "memory", Key: cat + "/" + name, Title: h.Title, Snippet: h.Snippet, UpdatedAt: h.UpdatedAt, Source: h.Source, Score: h.Score},
					noteID:    h.NoteID,
					prio:      recallKindPriority("memory"),
				})
			} else {
				// Agent-scope but not a knowledge memory: a session plan/progress/
				// context note or a lab notebook. Never parse these as memories.
				items = append(items, recallItem{
					RecallHit: RecallHit{Kind: "session", Key: h.Title, Title: h.Title, Snippet: h.Snippet, UpdatedAt: h.UpdatedAt, Source: h.Source, Score: h.Score},
					noteID:    h.NoteID,
					prio:      recallKindPriority("session"),
				})
			}
		}
	}

	if scope == "all" || scope == "notes" {
		for _, h := range s.searchKnowledgeScoped(ctx, userID, query, "user", recallPerScopeLimit, 0.3) {
			items = append(items, recallItem{
				RecallHit: RecallHit{Kind: "note", Key: h.NoteID, Title: h.Title, Snippet: h.Snippet, UpdatedAt: h.UpdatedAt, Source: h.Source, Score: h.Score},
				noteID:    h.NoteID,
				prio:      recallKindPriority("note"),
			})
		}
	}

	if scope == "all" || scope == "sessions" {
		items = append(items, s.recallSessions(ctx, userID, query, projectSlug, now)...)
	}

	// Enrich memory/note hits (search results carry no timestamp for the
	// semantic path, and no project tag) so age and project scoping are
	// accurate. Bounded by the collected hit count.
	for i := range items {
		it := &items[i]
		if it.Kind != "memory" && it.Kind != "note" {
			continue
		}
		if it.noteID == "" {
			continue
		}
		if !it.UpdatedAt.IsZero() && (it.Kind != "memory" || it.Project != "") {
			continue
		}
		if n, err := s.cfg.NoteService.Get(ctx, userID, it.noteID); err == nil {
			if it.UpdatedAt.IsZero() {
				it.UpdatedAt = n.UpdatedAt
			}
			if it.Kind == "memory" {
				it.Project = projectTag(n.Tags)
			}
		}
	}

	// Optional project filter: memory/session/trial hits must match; notes
	// (which recall does not project-resolve) pass through.
	if projectSlug != "" {
		filtered := items[:0]
		for _, it := range items {
			if it.Kind == "note" || it.Project == projectSlug {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	// Fill the humanized age.
	for i := range items {
		if !items[i].UpdatedAt.IsZero() {
			items[i].Age = humanizeAge(now.Sub(items[i].UpdatedAt))
		}
	}

	// Sort by score desc, then kind priority on ties.
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].prio < items[j].prio
	})

	// Greedy pack into the character budget.
	out := make([]RecallHit, 0, len(items))
	used := 0
	for _, it := range items {
		cost := len(it.Title) + len(": ") + len(it.Snippet)
		if used+cost > maxChars && len(out) > 0 {
			break
		}
		out = append(out, it.RecallHit)
		used += cost
	}
	return out, nil
}

// recallSessions lexically scores sessions (name + findings) against the query,
// reusing the prompt-context tokenizer and IDF-lite weighting.
func (s *Service) recallSessions(ctx context.Context, userID, query, projectSlug string, now time.Time) []recallItem {
	qtoks := promptTokenize(query)
	if len(qtoks) == 0 {
		return nil
	}
	qset := make(map[string]struct{}, len(qtoks))
	for _, t := range qtoks {
		qset[t] = struct{}{}
	}

	db, err := s.cfg.UserDBManager.Open(ctx, userID)
	if err != nil {
		return nil
	}
	var sessions []*Session
	if projectSlug != "" {
		sessions, err = s.cfg.Store.ListSessionsByProject(ctx, db, "", projectSlug, recallSessionScanCap)
	} else {
		sessions, err = s.cfg.Store.ListSessions(ctx, db, "", recallSessionScanCap, 0)
	}
	if err != nil {
		return nil
	}

	type cand struct {
		sess *Session
		set  map[string]struct{}
		ntok int
	}
	df := make(map[string]int)
	cands := make([]cand, 0, len(sessions))
	for _, sess := range sessions {
		toks := promptTokenize(sess.Name + " " + sess.Findings)
		set := make(map[string]struct{}, len(toks))
		for _, t := range toks {
			set[t] = struct{}{}
		}
		for t := range set {
			df[t]++
		}
		cands = append(cands, cand{sess: sess, set: set, ntok: len(toks)})
	}

	n := len(cands)
	var items []recallItem
	for _, c := range cands {
		overlap := 0
		var score float64
		for t := range qset {
			if _, ok := c.set[t]; ok {
				overlap++
				score += 1 + math.Log(float64(n)/(1+float64(df[t])))
			}
		}
		if overlap == 0 {
			continue
		}
		if c.ntok > 0 {
			score /= math.Sqrt(float64(c.ntok))
		}
		kind := "session"
		if strings.HasPrefix(c.sess.Name, LabSessionPrefix) {
			kind = "trial"
		}
		snippet := c.sess.Findings
		if r := []rune(snippet); len(r) > 200 {
			snippet = string(r[:200])
		}
		items = append(items, recallItem{
			RecallHit: RecallHit{
				Kind:      kind,
				Key:       c.sess.Name,
				Title:     c.sess.Name,
				Snippet:   snippet,
				Project:   c.sess.ProjectSlug,
				UpdatedAt: c.sess.UpdatedAt,
				Source:    "lexical",
				Score:     score,
			},
			prio: recallKindPriority(kind),
		})
	}
	return items
}
