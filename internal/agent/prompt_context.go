package agent

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/katata/seam/internal/note"
)

// Tunable floors for prompt-context matching. Precision beats recall for
// injected context: a noisy injection is worse than none. These are exported
// so they can be tuned against real usage.
const (
	// PromptContextMinOverlap is the minimum number of distinct tokens that
	// must be shared between the prompt and a candidate memory.
	PromptContextMinOverlap = 2
	// PromptContextMinScore is the minimum IDF-weighted score.
	PromptContextMinScore = 1.5

	promptCorpusTTL        = 30 * time.Second
	promptCorpusFetchLimit = 200
	promptMinTokenLen      = 3
)

// PromptHit is a memory reference relevant to a user prompt.
type PromptHit struct {
	Category    string    `json:"category"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Project     string    `json:"project,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
	Score       float64   `json:"score"`
}

// promptStopwords is a small set of common English words (length >= 3) dropped
// during tokenization. Shorter words are already dropped by promptMinTokenLen.
var promptStopwords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "with": {}, "this": {}, "that": {},
	"from": {}, "have": {}, "has": {}, "had": {}, "are": {}, "was": {},
	"were": {}, "will": {}, "would": {}, "should": {}, "could": {}, "can": {},
	"not": {}, "but": {}, "you": {}, "your": {}, "our": {}, "its": {},
	"their": {}, "they": {}, "them": {}, "then": {}, "than": {}, "when": {},
	"what": {}, "who": {}, "why": {}, "how": {}, "all": {}, "any": {},
	"some": {}, "out": {}, "get": {}, "got": {}, "use": {}, "into": {},
	"per": {}, "via": {}, "again": {}, "just": {},
}

// promptTokenize lowercases, splits on non-alphanumerics, and drops short and
// stopword tokens.
func promptTokenize(s string) []string {
	s = strings.ToLower(s)
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) < promptMinTokenLen {
			continue
		}
		if _, stop := promptStopwords[f]; stop {
			continue
		}
		out = append(out, f)
	}
	return out
}

// promptCandidate is a scored corpus entry with precomputed tokens.
type promptCandidate struct {
	category    string
	name        string
	description string
	project     string
	updatedAt   time.Time
	tokens      []string
	tokenSet    map[string]struct{}
}

// promptCorpus is a cached, scored corpus for one project scope.
type promptCorpus struct {
	candidates []promptCandidate
	idf        map[string]float64
	builtAt    time.Time
}

// PromptContext returns up to maxHits memory references relevant to a user
// prompt, scored lexically (no embedder -- the hook budget is ~2s). Returns an
// empty slice when nothing clears the floor or when the corpus cannot be built
// (contention/timeout); it never returns an error to the hook path.
func (s *Service) PromptContext(ctx context.Context, userID, cwd, prompt string, maxHits int) ([]PromptHit, error) {
	if maxHits <= 0 {
		maxHits = 3
	}
	promptTokens := promptTokenize(prompt)
	if len(promptTokens) == 0 {
		return nil, nil
	}

	project := s.ResolveProjectForCWD(ctx, userID, cwd)
	corpus := s.promptCorpusFor(ctx, userID, project)
	if corpus == nil || len(corpus.candidates) == 0 {
		return nil, nil
	}

	return scorePrompt(promptTokens, corpus, PromptContextMinOverlap, PromptContextMinScore, maxHits), nil
}

// RenderPromptRecall renders the <seam-recall> additionalContext block for a
// set of prompt hits, or "" when there are none (Claude Code ignores empty
// additionalContext). Every injected field is sanitized.
func RenderPromptRecall(hits []PromptHit) string {
	if len(hits) == 0 {
		return ""
	}
	now := time.Now().UTC()
	var b strings.Builder
	b.WriteString("<seam-recall>Seam has possibly relevant memories:")
	for _, h := range hits {
		label := sanitizeHookField(h.Category + "/" + h.Name)
		desc := sanitizeHookFieldN(h.Description, 160)
		age := humanizeAge(now.Sub(h.UpdatedAt))
		b.WriteString(fmt.Sprintf("\n- %s (%s ago): %s", label, age, desc))
	}
	b.WriteString("\nRead with mcp__seam__memory_read before re-deriving.</seam-recall>")
	return b.String()
}

// scorePrompt is the pure scoring half: it ranks corpus candidates against the
// prompt tokens and applies the overlap/score floor.
func scorePrompt(promptTokens []string, corpus *promptCorpus, minOverlap int, minScore float64, maxHits int) []PromptHit {
	promptSet := make(map[string]struct{}, len(promptTokens))
	for _, t := range promptTokens {
		promptSet[t] = struct{}{}
	}

	var hits []PromptHit
	for _, c := range corpus.candidates {
		overlap := 0
		var score float64
		for t := range promptSet {
			if _, ok := c.tokenSet[t]; ok {
				overlap++
				score += corpus.idf[t]
			}
		}
		if len(c.tokens) > 0 {
			score /= math.Sqrt(float64(len(c.tokens)))
		}
		if overlap < minOverlap || score < minScore {
			continue
		}
		hits = append(hits, PromptHit{
			Category:    c.category,
			Name:        c.name,
			Description: c.description,
			Project:     c.project,
			UpdatedAt:   c.updatedAt,
			Score:       score,
		})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].UpdatedAt.After(hits[j].UpdatedAt)
	})
	if len(hits) > maxHits {
		hits = hits[:maxHits]
	}
	return hits
}

// promptCorpusFor returns the cached scored corpus for a project scope, or
// (re)builds it. The DB pool is SetMaxOpenConns(1) and note writes hold their
// transaction across disk I/O, so an uncached fetch arriving during a write can
// block; the 30s cache keeps prompt-submit off that hot path. A failed build
// returns nil (degrade to empty), never an error.
func (s *Service) promptCorpusFor(ctx context.Context, userID, project string) *promptCorpus {
	key := project // "" == unresolved (all knowledge)

	s.promptCorpusMu.RLock()
	if c, ok := s.promptCorpusCache[key]; ok && time.Since(c.builtAt) < promptCorpusTTL {
		s.promptCorpusMu.RUnlock()
		return c
	}
	s.promptCorpusMu.RUnlock()

	c := s.buildPromptCorpus(ctx, userID, project)
	if c == nil {
		return nil
	}

	s.promptCorpusMu.Lock()
	if s.promptCorpusCache == nil {
		s.promptCorpusCache = make(map[string]*promptCorpus)
	}
	s.promptCorpusCache[key] = c
	s.promptCorpusMu.Unlock()
	return c
}

// buildPromptCorpus fetches and scores the candidate memories for a scope.
func (s *Service) buildPromptCorpus(ctx context.Context, userID, project string) *promptCorpus {
	memID, ok := s.agentMemoryProjectID(ctx, userID)
	if !ok {
		// No agent-memory project yet: an empty (but valid) corpus we can cache.
		return &promptCorpus{builtAt: time.Now()}
	}

	tag := "type:knowledge"
	if project != "" {
		tag = "project:" + project
	}
	notes, _, err := s.cfg.NoteService.List(ctx, userID, note.NoteFilter{
		ProjectID: memID,
		Tag:       tag,
		Limit:     promptCorpusFetchLimit,
	})
	if err != nil {
		s.cfg.Logger.Debug("agent.PromptContext: corpus fetch failed, degrading to empty",
			"project", project, "error", err)
		return nil
	}

	cands := make([]promptCandidate, 0, len(notes))
	df := make(map[string]int)
	for _, n := range notes {
		cat, name := parseKnowledgeTitle(n.Title)
		if cat == "" {
			continue // not a "Knowledge: " note (session/lab note in agent scope)
		}
		toks := promptTokenize(name + " " + n.Description)
		set := make(map[string]struct{}, len(toks))
		for _, t := range toks {
			set[t] = struct{}{}
		}
		for t := range set {
			df[t]++
		}
		cands = append(cands, promptCandidate{
			category:    cat,
			name:        name,
			description: n.Description,
			project:     projectTag(n.Tags),
			updatedAt:   n.UpdatedAt,
			tokens:      toks,
			tokenSet:    set,
		})
	}

	numDocs := len(cands)
	idf := make(map[string]float64, len(df))
	for t, d := range df {
		idf[t] = 1 + math.Log(float64(numDocs)/(1+float64(d)))
	}
	return &promptCorpus{candidates: cands, idf: idf, builtAt: time.Now()}
}
