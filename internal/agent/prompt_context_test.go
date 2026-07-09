package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/note"
)

func testCand(category, name, desc string) promptCandidate {
	toks := promptTokenize(name + " " + desc)
	set := make(map[string]struct{}, len(toks))
	for _, t := range toks {
		set[t] = struct{}{}
	}
	return promptCandidate{category: category, name: name, description: desc, tokens: toks, tokenSet: set}
}

// testCorpus builds a corpus with a fixed idf per token so floor behavior is
// deterministic and independent of corpus size.
func testCorpus(idfVal float64, cands ...promptCandidate) *promptCorpus {
	idf := map[string]float64{}
	for _, c := range cands {
		for t := range c.tokenSet {
			idf[t] = idfVal
		}
	}
	return &promptCorpus{candidates: cands, idf: idf}
}

func TestPromptTokenize_DropsShortAndStopwords(t *testing.T) {
	got := promptTokenize("The wear detection is inverted again")
	require.Equal(t, []string{"wear", "detection", "inverted"}, got)
}

func TestScorePrompt_MatchesAboveFloor(t *testing.T) {
	corpus := testCorpus(2.0, testCand("protocol", "mw75-wear-state-encoding", "wear detection polarity inverted"))
	prompt := promptTokenize("the wear detection is inverted again")

	hits := scorePrompt(prompt, corpus, PromptContextMinOverlap, PromptContextMinScore, 3)
	require.Len(t, hits, 1)
	require.Equal(t, "mw75-wear-state-encoding", hits[0].Name)
	require.Equal(t, "protocol", hits[0].Category)
}

func TestScorePrompt_UnrelatedPromptNoMatch(t *testing.T) {
	corpus := testCorpus(2.0, testCand("protocol", "mw75-wear-state-encoding", "wear detection polarity inverted"))
	// "commit and push to dev" shares no tokens with the memory.
	prompt := promptTokenize("commit and push to dev")

	hits := scorePrompt(prompt, corpus, PromptContextMinOverlap, PromptContextMinScore, 3)
	require.Empty(t, hits)
}

func TestScorePrompt_SingleOverlapBelowFloor(t *testing.T) {
	corpus := testCorpus(2.0, testCand("protocol", "mw75-wear-state-encoding", "wear detection polarity inverted"))
	// Only "wear" overlaps -> overlap 1 < min overlap 2.
	prompt := promptTokenize("the wear something completely different")

	hits := scorePrompt(prompt, corpus, PromptContextMinOverlap, PromptContextMinScore, 3)
	require.Empty(t, hits)
}

func TestScorePrompt_LowIDFBelowScoreFloor(t *testing.T) {
	// Two overlapping tokens but very low idf -> below the score floor.
	corpus := testCorpus(0.2, testCand("protocol", "wear-detection", "wear detection here now"))
	prompt := promptTokenize("wear detection issue")

	hits := scorePrompt(prompt, corpus, PromptContextMinOverlap, PromptContextMinScore, 3)
	require.Empty(t, hits)
}

func TestPromptContext_EndToEnd_Match(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// A few distractor memories so the target tokens are rare (high idf).
	_, err := svc.MemoryWrite(ctx, testUserID, "runbook", "release-flow", "how to cut a release", "release procedure", "")
	require.NoError(t, err)
	_, err = svc.MemoryWrite(ctx, testUserID, "reference", "docs-links", "external documentation pointers", "doc links", "")
	require.NoError(t, err)
	_, err = svc.MemoryWrite(ctx, testUserID, "decision", "db-choice", "we chose sqlite", "database decision", "")
	require.NoError(t, err)
	// Target.
	_, err = svc.MemoryWrite(ctx, testUserID, "protocol", "mw75-wear-state", "wear detection polarity inverted", "wear detection polarity inverted", "")
	require.NoError(t, err)

	hits, err := svc.PromptContext(ctx, testUserID, "", "the wear detection is inverted again", 3)
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.Equal(t, "mw75-wear-state", hits[0].Name)
}

func TestPromptContext_EmptyPrompt_ReturnsEmpty(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()
	hits, err := svc.PromptContext(ctx, testUserID, "", "   ", 3)
	require.NoError(t, err)
	require.Empty(t, hits)
}

// errListNoteCreator embeds NoteCreator (nil) and overrides List to error,
// simulating DB contention/timeout while building the corpus.
type errListNoteCreator struct{ NoteCreator }

func (errListNoteCreator) List(context.Context, string, note.NoteFilter) ([]*note.Note, int, error) {
	return nil, 0, context.DeadlineExceeded
}

func TestPromptContext_CorpusFetchError_ReturnsEmpty(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Seed a memory so the agent-memory project exists and its ID is cached.
	_, err := svc.MemoryWrite(ctx, testUserID, "protocol", "seed", "seed content here", "seed", "")
	require.NoError(t, err)

	// Now make the corpus fetch fail.
	svc.cfg.NoteService = errListNoteCreator{}
	hits, err := svc.PromptContext(ctx, testUserID, "", "seed content here protocol", 3)
	require.NoError(t, err)
	require.Empty(t, hits)
}
