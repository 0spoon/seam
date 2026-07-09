package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/search"
)

// fakeSearcher implements the Searcher interface with configurable semantic
// results so tests can exercise the dedup-hint path deterministically.
type fakeSearcher struct {
	semantic []search.SemanticResult
}

func (f *fakeSearcher) SearchFTS(context.Context, string, string, int, int) ([]search.FTSResult, int, error) {
	return nil, 0, nil
}

func (f *fakeSearcher) SearchSemantic(context.Context, string, string, int) ([]search.SemanticResult, error) {
	return f.semantic, nil
}

func (f *fakeSearcher) SearchFTSScoped(context.Context, string, string, int, int, string, string) ([]search.FTSResult, int, error) {
	return nil, 0, nil
}

func (f *fakeSearcher) SearchSemanticScoped(context.Context, string, string, int, map[string]interface{}) ([]search.SemanticResult, error) {
	return f.semantic, nil
}

func (f *fakeSearcher) SearchSemanticScopedWithRecency(context.Context, string, string, int, map[string]interface{}, float64) ([]search.SemanticResult, error) {
	return f.semantic, nil
}

func (f *fakeSearcher) SearchFTSScopedWithRecency(context.Context, string, string, int, int, string, string, float64) ([]search.FTSResult, int, error) {
	return nil, 0, nil
}

func (f *fakeSearcher) SearchFTSWithRecency(context.Context, string, string, int, int, float64) ([]search.FTSResult, int, error) {
	return nil, 0, nil
}

func TestMemoryWrite_InvalidCategory_Rejected(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.MemoryWrite(ctx, testUserID, "go", "name", "content", "desc", "")
	require.ErrorIs(t, err, ErrInvalidCategory)
}

func TestMemoryWrite_DerivesDescription_WhenEmpty(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	res, err := svc.MemoryWrite(ctx, testUserID, "protocol", "wire-format",
		"## Wire format\n\nThe first byte is the opcode.", "", "")
	require.NoError(t, err)

	title, description, _, err := svc.MemoryRead(ctx, testUserID, "protocol", "wire-format")
	require.NoError(t, err)
	require.Equal(t, "Wire format", description)

	// The derived description is also persisted on the note itself.
	n, err := svc.cfg.NoteService.Get(ctx, testUserID, res.NoteID)
	require.NoError(t, err)
	require.Equal(t, "Wire format", n.Description)
	require.Equal(t, KnowledgeNoteTitle("protocol", "wire-format"), title)
}

func TestMemoryWrite_ProjectTagApplied(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	p, err := svc.cfg.ProjectService.Create(ctx, testUserID, "MW75 Neuro", "")
	require.NoError(t, err)

	res, err := svc.MemoryWrite(ctx, testUserID, "protocol", "coex",
		"BLE coex causes a music dip.", "one-liner", p.Slug)
	require.NoError(t, err)

	n, err := svc.cfg.NoteService.Get(ctx, testUserID, res.NoteID)
	require.NoError(t, err)
	require.Contains(t, n.Tags, "project:"+p.Slug)

	// The project surfaces in MemoryList items.
	items, err := svc.MemoryList(ctx, testUserID, "protocol")
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, p.Slug, items[0].Project)
	require.Equal(t, "one-liner", items[0].Description)
}

func TestMemoryWrite_UnknownProject_Rejected(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.MemoryWrite(ctx, testUserID, "protocol", "n", "c", "d", "no-such-project")
	require.ErrorIs(t, err, ErrUnknownProject)
}

func TestMemoryWrite_DedupHint_ReturnsSimilar(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()
	svc.cfg.SearchService = &fakeSearcher{
		semantic: []search.SemanticResult{
			{NoteID: "n1", Title: KnowledgeNoteTitle("protocol", "jwt-auth"), Snippet: "...", Score: 0.9},
			// A non-knowledge agent note (e.g. session plan) must be ignored.
			{NoteID: "n2", Title: "Session Plan: something", Snippet: "...", Score: 0.95},
		},
	}

	res, err := svc.MemoryWrite(ctx, testUserID, "protocol", "auth-tokens",
		"JWT auth token handling.", "jwt handling", "")
	require.NoError(t, err)
	require.NotNil(t, res.Similar)
	require.Equal(t, "protocol", res.Similar.Category)
	require.Equal(t, "jwt-auth", res.Similar.Name)
	require.InDelta(t, 0.9, res.Similar.Score, 0.001)
}

func TestMemoryWrite_DedupHint_BelowThreshold_NoSimilar(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()
	svc.cfg.SearchService = &fakeSearcher{
		semantic: []search.SemanticResult{
			{NoteID: "n1", Title: KnowledgeNoteTitle("protocol", "jwt-auth"), Snippet: "...", Score: 0.5},
		},
	}

	res, err := svc.MemoryWrite(ctx, testUserID, "protocol", "auth-tokens",
		"JWT auth token handling.", "jwt handling", "")
	require.NoError(t, err)
	require.Nil(t, res.Similar)
}

func TestMemoryRead_NameOnlyFallback_UniqueMatch(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.MemoryWrite(ctx, testUserID, "protocol", "distinctive-name",
		"body text", "desc", "")
	require.NoError(t, err)

	// Wrong category, but the name is unique -> fallback resolves it.
	title, _, body, err := svc.MemoryRead(ctx, testUserID, "gotcha", "distinctive-name")
	require.NoError(t, err)
	require.Equal(t, KnowledgeNoteTitle("protocol", "distinctive-name"), title)
	require.Contains(t, body, "body text")
}

func TestMemoryRead_NameOnlyFallback_Ambiguous(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	_, err := svc.MemoryWrite(ctx, testUserID, "protocol", "dup", "a", "d", "")
	require.NoError(t, err)
	_, err = svc.MemoryWrite(ctx, testUserID, "gotcha", "dup", "b", "d", "")
	require.NoError(t, err)

	// Wrong category and the name is ambiguous across two categories.
	_, _, _, err = svc.MemoryRead(ctx, testUserID, "reference", "dup")
	require.ErrorIs(t, err, ErrNotFound)
}
