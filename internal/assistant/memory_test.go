package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMemoryStore_SaveAndGet(t *testing.T) {
	db := setupTestDB(t)
	store := NewMemoryStore()

	m := &Memory{
		ID:         "mem1",
		Category:   MemoryCategoryFact,
		Content:    "User is working on a Go backend",
		Source:     "conv1",
		Confidence: 1.0,
		CreatedAt:  time.Now().UTC(),
	}

	err := store.SaveMemory(context.Background(), db, m)
	require.NoError(t, err)

	got, err := store.GetMemory(context.Background(), db, "mem1")
	require.NoError(t, err)
	require.Equal(t, "mem1", got.ID)
	require.Equal(t, MemoryCategoryFact, got.Category)
	require.Equal(t, "User is working on a Go backend", got.Content)
	require.Equal(t, "conv1", got.Source)
	require.InDelta(t, 1.0, got.Confidence, 0.01)
}

func TestMemoryStore_SaveAndGet_NotFound(t *testing.T) {
	db := setupTestDB(t)
	store := NewMemoryStore()

	_, err := store.GetMemory(context.Background(), db, "nonexistent")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryStore_ListMemories(t *testing.T) {
	db := setupTestDB(t)
	store := NewMemoryStore()

	for i := 0; i < 5; i++ {
		m := &Memory{
			ID:         mustGenULID(t),
			Category:   MemoryCategoryFact,
			Content:    "Fact number something",
			Confidence: 1.0,
			CreatedAt:  time.Now().UTC(),
		}
		require.NoError(t, store.SaveMemory(context.Background(), db, m))
	}
	for i := 0; i < 3; i++ {
		m := &Memory{
			ID:         mustGenULID(t),
			Category:   MemoryCategoryPreference,
			Content:    "Preference something",
			Confidence: 1.0,
			CreatedAt:  time.Now().UTC(),
		}
		require.NoError(t, store.SaveMemory(context.Background(), db, m))
	}

	// List all.
	all, total, err := store.ListMemories(context.Background(), db, "", 50, 0)
	require.NoError(t, err)
	require.Equal(t, 8, total)
	require.Len(t, all, 8)

	// List by category.
	facts, factTotal, err := store.ListMemories(context.Background(), db, MemoryCategoryFact, 50, 0)
	require.NoError(t, err)
	require.Equal(t, 5, factTotal)
	require.Len(t, facts, 5)

	prefs, prefTotal, err := store.ListMemories(context.Background(), db, MemoryCategoryPreference, 50, 0)
	require.NoError(t, err)
	require.Equal(t, 3, prefTotal)
	require.Len(t, prefs, 3)
}

func TestMemoryStore_SearchMemories(t *testing.T) {
	db := setupTestDB(t)
	store := NewMemoryStore()

	memories := []Memory{
		{ID: mustGenULID(t), Category: MemoryCategoryFact, Content: "User works with Go programming language", Confidence: 1.0, CreatedAt: time.Now().UTC()},
		{ID: mustGenULID(t), Category: MemoryCategoryFact, Content: "User enjoys reading science fiction novels", Confidence: 1.0, CreatedAt: time.Now().UTC()},
		{ID: mustGenULID(t), Category: MemoryCategoryPreference, Content: "User prefers concise responses", Confidence: 1.0, CreatedAt: time.Now().UTC()},
	}
	for i := range memories {
		require.NoError(t, store.SaveMemory(context.Background(), db, &memories[i]))
	}

	// Search for Go.
	results, err := store.SearchMemories(context.Background(), db, "Go programming", 10)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Contains(t, results[0].Content, "Go programming")

	// Search for fiction.
	results, err = store.SearchMemories(context.Background(), db, "science fiction", 10)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Contains(t, results[0].Content, "fiction")
}

func TestMemoryStore_DeleteMemory(t *testing.T) {
	db := setupTestDB(t)
	store := NewMemoryStore()

	m := &Memory{
		ID: "mem_del", Category: MemoryCategoryFact, Content: "To be deleted",
		Confidence: 1.0, CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, store.SaveMemory(context.Background(), db, m))

	// Verify it exists.
	_, err := store.GetMemory(context.Background(), db, "mem_del")
	require.NoError(t, err)

	// Delete.
	err = store.DeleteMemory(context.Background(), db, "mem_del")
	require.NoError(t, err)

	// Verify it's gone.
	_, err = store.GetMemory(context.Background(), db, "mem_del")
	require.ErrorIs(t, err, ErrNotFound)

	// Delete non-existent.
	err = store.DeleteMemory(context.Background(), db, "nonexistent")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryStore_TouchMemory(t *testing.T) {
	db := setupTestDB(t)
	store := NewMemoryStore()

	m := &Memory{
		ID: "mem_touch", Category: MemoryCategoryFact, Content: "Touch me",
		Confidence: 1.0, CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, store.SaveMemory(context.Background(), db, m))

	err := store.TouchMemory(context.Background(), db, "mem_touch")
	require.NoError(t, err)

	got, err := store.GetMemory(context.Background(), db, "mem_touch")
	require.NoError(t, err)
	require.False(t, got.LastAccessed.IsZero(), "last_accessed should be set")
}

func TestMemoryStore_SearchMemories_DecayPrefersFresher(t *testing.T) {
	db := setupTestDB(t)
	store := NewMemoryStore()
	now := time.Now().UTC()

	// Two memories with the same content; one is fresh, the other is
	// 120 days old (4 half-lives -> ~1/16th decay). The fresh one
	// should rank higher even though their FTS scores are identical.
	stale := &Memory{
		ID:         "stale",
		Category:   MemoryCategoryFact,
		Content:    "User collects vintage typewriters",
		Confidence: 1.0,
		CreatedAt:  now.AddDate(0, 0, -120),
	}
	fresh := &Memory{
		ID:         "fresh",
		Category:   MemoryCategoryFact,
		Content:    "User collects vintage typewriters",
		Confidence: 1.0,
		CreatedAt:  now,
	}
	require.NoError(t, store.SaveMemory(context.Background(), db, stale))
	require.NoError(t, store.SaveMemory(context.Background(), db, fresh))

	results, err := store.SearchMemories(context.Background(), db, "vintage typewriters", 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "fresh", results[0].ID, "fresh memory should outrank stale memory of identical content")
	require.Equal(t, "stale", results[1].ID)
}

func TestMemoryStore_SearchMemories_ConfidenceDemotes(t *testing.T) {
	db := setupTestDB(t)
	store := NewMemoryStore()
	now := time.Now().UTC()

	low := &Memory{
		ID: "low", Category: MemoryCategoryFact,
		Content: "User likes bread", Confidence: 0.1, CreatedAt: now,
	}
	high := &Memory{
		ID: "high", Category: MemoryCategoryFact,
		Content: "User likes bread", Confidence: 1.0, CreatedAt: now,
	}
	require.NoError(t, store.SaveMemory(context.Background(), db, low))
	require.NoError(t, store.SaveMemory(context.Background(), db, high))

	results, err := store.SearchMemories(context.Background(), db, "bread", 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "high", results[0].ID, "high-confidence memory should rank above low-confidence twin")
}

func TestMemoryStore_SearchMemories_FreshWeakBeatsStaleStrong(t *testing.T) {
	db := setupTestDB(t)
	store := NewMemoryStore()
	now := time.Now().UTC()

	// A stale, content-rich memory vs a fresh, lighter-touch one. With
	// a 90-day age (3 half-lives -> ~0.125x decay) the fresh one
	// should win even though the stale one would otherwise dominate
	// the BM25 ranking.
	stale := &Memory{
		ID: "stale_strong", Category: MemoryCategoryFact,
		Content:    "alpha alpha alpha alpha alpha alpha alpha",
		Confidence: 1.0,
		CreatedAt:  now.AddDate(0, 0, -90),
	}
	fresh := &Memory{
		ID: "fresh_weak", Category: MemoryCategoryFact,
		Content:    "alpha bravo charlie",
		Confidence: 1.0,
		CreatedAt:  now,
	}
	require.NoError(t, store.SaveMemory(context.Background(), db, stale))
	require.NoError(t, store.SaveMemory(context.Background(), db, fresh))

	results, err := store.SearchMemories(context.Background(), db, "alpha", 10)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Equal(t, "fresh_weak", results[0].ID,
		"a fresh weak match should outrank a stale strong match after 3 half-lives")
}

func TestMemoryStore_TouchMemories_BatchUpdates(t *testing.T) {
	db := setupTestDB(t)
	store := NewMemoryStore()
	now := time.Now().UTC().Add(-time.Hour)

	for _, id := range []string{"a", "b", "c"} {
		m := &Memory{
			ID: id, Category: MemoryCategoryFact,
			Content: "memory " + id, Confidence: 1.0, CreatedAt: now,
		}
		require.NoError(t, store.SaveMemory(context.Background(), db, m))
	}

	require.NoError(t, store.TouchMemories(context.Background(), db, []string{"a", "c"}))

	a, err := store.GetMemory(context.Background(), db, "a")
	require.NoError(t, err)
	require.False(t, a.LastAccessed.IsZero(), "a should be touched")

	b, err := store.GetMemory(context.Background(), db, "b")
	require.NoError(t, err)
	require.True(t, b.LastAccessed.IsZero(), "b should not be touched")

	c, err := store.GetMemory(context.Background(), db, "c")
	require.NoError(t, err)
	require.False(t, c.LastAccessed.IsZero(), "c should be touched")
}

func TestMemoryStore_TouchMemories_EmptyIsNoop(t *testing.T) {
	db := setupTestDB(t)
	store := NewMemoryStore()

	require.NoError(t, store.TouchMemories(context.Background(), db, nil))
	require.NoError(t, store.TouchMemories(context.Background(), db, []string{}))
}

func TestMemoryStore_ConversationSummary_RoundTrip(t *testing.T) {
	db := setupTestDB(t)
	store := NewMemoryStore()
	ctx := context.Background()

	// Initially: no summary.
	_, err := store.GetConversationSummary(ctx, db, "conv1")
	require.ErrorIs(t, err, ErrNotFound)

	// Save and read back.
	require.NoError(t, store.SaveConversationSummary(ctx, db, "conv1", "First version of summary."))
	got, err := store.GetConversationSummary(ctx, db, "conv1")
	require.NoError(t, err)
	require.Equal(t, "First version of summary.", got.Content)
	require.Equal(t, MemoryCategorySummary, got.Category)
	require.Equal(t, "conv1", got.Source)

	// Saving again upserts the same row.
	require.NoError(t, store.SaveConversationSummary(ctx, db, "conv1", "Refreshed summary v2."))
	got2, err := store.GetConversationSummary(ctx, db, "conv1")
	require.NoError(t, err)
	require.Equal(t, "Refreshed summary v2.", got2.Content)
	require.Equal(t, got.ID, got2.ID, "summary ID should be deterministic per conversation")

	// Empty summary deletes the entry.
	require.NoError(t, store.SaveConversationSummary(ctx, db, "conv1", "   "))
	_, err = store.GetConversationSummary(ctx, db, "conv1")
	require.ErrorIs(t, err, ErrNotFound)

	// Empty conversation ID is rejected on save and lookup.
	require.Error(t, store.SaveConversationSummary(ctx, db, "", "x"))
	_, err = store.GetConversationSummary(ctx, db, "")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryStore_ConversationSummary_IsolatedPerConversation(t *testing.T) {
	db := setupTestDB(t)
	store := NewMemoryStore()
	ctx := context.Background()

	require.NoError(t, store.SaveConversationSummary(ctx, db, "conv_a", "summary for A"))
	require.NoError(t, store.SaveConversationSummary(ctx, db, "conv_b", "summary for B"))

	a, err := store.GetConversationSummary(ctx, db, "conv_a")
	require.NoError(t, err)
	require.Equal(t, "summary for A", a.Content)

	b, err := store.GetConversationSummary(ctx, db, "conv_b")
	require.NoError(t, err)
	require.Equal(t, "summary for B", b.Content)

	require.NotEqual(t, a.ID, b.ID)
}

func TestRelevanceScore_DecayMonotonic(t *testing.T) {
	now := time.Now().UTC()
	bm25 := -2.0 // a strong-ish match

	// Same content/confidence, varying age -> score should decrease.
	mFresh := &Memory{Confidence: 1.0, CreatedAt: now}
	m30 := &Memory{Confidence: 1.0, CreatedAt: now.AddDate(0, 0, -30)}
	m90 := &Memory{Confidence: 1.0, CreatedAt: now.AddDate(0, 0, -90)}

	sFresh := relevanceScore(mFresh, bm25, now)
	s30 := relevanceScore(m30, bm25, now)
	s90 := relevanceScore(m90, bm25, now)

	require.Greater(t, sFresh, s30)
	require.Greater(t, s30, s90)
	// 30 days = 1 half-life -> roughly half.
	require.InDelta(t, sFresh/2, s30, sFresh*0.05)
}

func mustGenULID(t *testing.T) string {
	t.Helper()
	id, err := generateULID()
	require.NoError(t, err)
	return id
}
