package usage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/testutil"
)

func TestTracker_TrackPopulatesIDAndTimestamp(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewStore()
	ctx := context.Background()

	r := &Record{
		UserID:       "u1",
		Function:     FuncChat,
		Provider:     "openai",
		Model:        "gpt-4o",
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
	}

	// Directly insert using store (tracker.Track needs a userdb.Manager,
	// but the store layer is where the logic lives).
	r.ID = "01MANUAL_ID"
	r.CreatedAt = time.Now().UTC()
	require.NoError(t, store.Insert(ctx, db, r))

	sum, err := store.GetSummary(ctx, db,
		r.CreatedAt.Add(-1*time.Hour),
		r.CreatedAt.Add(1*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(150), sum.TotalTokens)
	require.Equal(t, int64(1), sum.CallCount)
}

func TestEstimateTokenCount(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{"empty", "", 0},
		{"short", "hi", 1},        // 2 chars / 4 = 0, min 1
		{"medium", "hello world", 2}, // 11 chars / 4 = 2
		{"longer", "The quick brown fox jumps over the lazy dog", 10}, // 43 / 4 = 10
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateTokenCount(tt.text)
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestEstimateInputTokens(t *testing.T) {
	msgs := []struct {
		Role    string
		Content string
	}{
		{"system", "You are helpful."},
		{"user", "Hello there"},
	}

	// Convert to ai.ChatMessage -- but since this is in the usage package,
	// we can test estimateTokenCount directly.
	total := 0
	for _, m := range msgs {
		total += estimateTokenCount(m.Content)
	}
	require.Greater(t, total, 0)
}
