package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/search"
)

func TestRecall_SessionScope_DetectsTrialKind(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.SessionStart(ctx, testUserID, "fix-coex-bug", "", DefaultMaxContextChars)
	require.NoError(t, err)
	require.NoError(t, svc.SessionEnd(ctx, testUserID, "fix-coex-bug", "coex root cause found in the AGC gain step"))

	_, err = svc.SessionStart(ctx, testUserID, "lab/coex-investigation", "", DefaultMaxContextChars)
	require.NoError(t, err)
	require.NoError(t, svc.SessionEnd(ctx, testUserID, "lab/coex-investigation", "coex trials show gain instability"))

	hits, err := svc.Recall(ctx, testUserID, "coex gain", "sessions", "", 3000)
	require.NoError(t, err)
	require.NotEmpty(t, hits)

	kinds := map[string]bool{}
	for _, h := range hits {
		kinds[h.Kind] = true
		require.Equal(t, "lexical", h.Source)
		require.NotEmpty(t, h.Age)
	}
	require.True(t, kinds["trial"], "lab/ session must be kind trial")
	require.True(t, kinds["session"], "regular session must be kind session")
}

func TestRecall_MemoryScope_ClassifiesHits(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()
	svc.cfg.SearchService = &fakeSearcher{semantic: []search.SemanticResult{
		{NoteID: "n1", Title: KnowledgeNoteTitle("protocol", "wire-format"), Snippet: "byte layout", Score: 0.8},
		// A non-knowledge agent-scope note (session plan) must NOT be parsed as a memory.
		{NoteID: "n2", Title: "Session Plan: refactor", Snippet: "the plan", Score: 0.7},
	}}

	hits, err := svc.Recall(ctx, testUserID, "wire format", "memories", "", 3000)
	require.NoError(t, err)

	var mem, sess *RecallHit
	for i := range hits {
		switch hits[i].Kind {
		case "memory":
			mem = &hits[i]
		case "session":
			sess = &hits[i]
		}
	}
	require.NotNil(t, mem)
	require.Equal(t, "protocol/wire-format", mem.Key)
	require.Equal(t, "semantic", mem.Source)
	require.NotNil(t, sess, "non-knowledge agent note maps to session kind")
	require.Equal(t, "Session Plan: refactor", sess.Key)
}

func TestRecall_BudgetPacking(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()
	var sem []search.SemanticResult
	for i := 0; i < 10; i++ {
		sem = append(sem, search.SemanticResult{
			NoteID:  fmt.Sprintf("n%d", i),
			Title:   KnowledgeNoteTitle("protocol", fmt.Sprintf("mem-%d", i)),
			Snippet: strings.Repeat("x", 100),
			Score:   float64(10 - i),
		})
	}
	svc.cfg.SearchService = &fakeSearcher{semantic: sem}

	hits, err := svc.Recall(ctx, testUserID, "query", "memories", "", 250)
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.LessOrEqual(t, len(hits), 3, "must respect the character budget")
	// Highest score first.
	require.Equal(t, "protocol/mem-0", hits[0].Key)
}

func TestRecall_EmptyQuery_EmptyResult(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()
	hits, err := svc.Recall(ctx, testUserID, "", "all", "", 3000)
	require.NoError(t, err)
	require.Empty(t, hits)
}
