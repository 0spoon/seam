package usage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/testutil"
)

func TestStore_InsertAndGetSummary(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := context.Background()
	s := NewStore()

	now := time.Now().UTC()
	records := []*Record{
		{
			ID: "01TEST00000001", UserID: "u1", Function: FuncChat,
			Provider: "openai", Model: "gpt-4o", InputTokens: 100,
			OutputTokens: 50, TotalTokens: 150, IsLocal: false,
			DurationMS: 200, CreatedAt: now,
		},
		{
			ID: "01TEST00000002", UserID: "u1", Function: FuncEmbedding,
			Provider: "ollama", Model: "nomic-embed-text", InputTokens: 80,
			OutputTokens: 0, TotalTokens: 80, IsLocal: true,
			DurationMS: 50, CreatedAt: now,
		},
		{
			ID: "01TEST00000003", UserID: "u1", Function: FuncAssistant,
			Provider: "openai", Model: "gpt-4o-mini", InputTokens: 200,
			OutputTokens: 100, TotalTokens: 300, IsLocal: false,
			DurationMS: 500, CreatedAt: now,
		},
	}

	for _, r := range records {
		require.NoError(t, s.Insert(ctx, db, r))
	}

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	sum, err := s.GetSummary(ctx, db, from, to)
	require.NoError(t, err)
	require.Equal(t, int64(530), sum.TotalTokens)
	require.Equal(t, int64(380), sum.InputTokens)
	require.Equal(t, int64(150), sum.OutputTokens)
	require.Equal(t, int64(450), sum.BilledTokens)  // non-local
	require.Equal(t, int64(80), sum.LocalTokens)     // local
	require.Equal(t, int64(3), sum.CallCount)
}

func TestStore_GetByFunction(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := context.Background()
	s := NewStore()

	now := time.Now().UTC()
	require.NoError(t, s.Insert(ctx, db, &Record{
		ID: "01A", UserID: "u1", Function: FuncChat,
		Provider: "openai", Model: "gpt-4o", InputTokens: 100,
		OutputTokens: 50, TotalTokens: 150, CreatedAt: now,
	}))
	require.NoError(t, s.Insert(ctx, db, &Record{
		ID: "01B", UserID: "u1", Function: FuncChat,
		Provider: "openai", Model: "gpt-4o", InputTokens: 200,
		OutputTokens: 100, TotalTokens: 300, CreatedAt: now,
	}))
	require.NoError(t, s.Insert(ctx, db, &Record{
		ID: "01C", UserID: "u1", Function: FuncAssistant,
		Provider: "openai", Model: "gpt-4o-mini", InputTokens: 50,
		OutputTokens: 25, TotalTokens: 75, CreatedAt: now,
	}))

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	result, err := s.GetByFunction(ctx, db, from, to)
	require.NoError(t, err)
	require.Len(t, result, 2)
	// Ordered by total_tokens DESC.
	require.Equal(t, "chat", result[0].Function)
	require.Equal(t, int64(450), result[0].TotalTokens)
	require.Equal(t, int64(2), result[0].CallCount)
	require.Equal(t, "assistant", result[1].Function)
	require.Equal(t, int64(75), result[1].TotalTokens)
}

func TestStore_GetByModel(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := context.Background()
	s := NewStore()

	now := time.Now().UTC()
	require.NoError(t, s.Insert(ctx, db, &Record{
		ID: "01A", UserID: "u1", Function: FuncChat,
		Provider: "openai", Model: "gpt-4o", InputTokens: 100,
		OutputTokens: 50, TotalTokens: 150, CreatedAt: now,
	}))
	require.NoError(t, s.Insert(ctx, db, &Record{
		ID: "01B", UserID: "u1", Function: FuncAssistant,
		Provider: "openai", Model: "gpt-4o-mini", InputTokens: 200,
		OutputTokens: 100, TotalTokens: 300, CreatedAt: now,
	}))
	require.NoError(t, s.Insert(ctx, db, &Record{
		ID: "01C", UserID: "u1", Function: FuncEmbedding,
		Provider: "ollama", Model: "nomic-embed-text", InputTokens: 80,
		OutputTokens: 0, TotalTokens: 80, IsLocal: true, CreatedAt: now,
	}))

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	result, err := s.GetByModel(ctx, db, from, to)
	require.NoError(t, err)
	require.Len(t, result, 3)
	// Ordered by total_tokens DESC.
	require.Equal(t, "gpt-4o-mini", result[0].Model)
	require.Equal(t, "openai", result[0].Provider)
	require.Equal(t, int64(300), result[0].TotalTokens)
	require.Equal(t, "gpt-4o", result[1].Model)
	require.Equal(t, "nomic-embed-text", result[2].Model)
	require.Equal(t, "ollama", result[2].Provider)
}

func TestStore_GetByProvider(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := context.Background()
	s := NewStore()

	now := time.Now().UTC()
	require.NoError(t, s.Insert(ctx, db, &Record{
		ID: "01A", UserID: "u1", Function: FuncChat,
		Provider: "openai", Model: "gpt-4o", InputTokens: 100,
		OutputTokens: 50, TotalTokens: 150, CreatedAt: now,
	}))
	require.NoError(t, s.Insert(ctx, db, &Record{
		ID: "01B", UserID: "u1", Function: FuncEmbedding,
		Provider: "ollama", Model: "nomic", InputTokens: 80,
		TotalTokens: 80, IsLocal: true, CreatedAt: now,
	}))

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	result, err := s.GetByProvider(ctx, db, from, to)
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.Equal(t, "openai", result[0].Provider)
	require.Equal(t, int64(150), result[0].TotalTokens)
}

func TestStore_GetTimeSeries(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := context.Background()
	s := NewStore()

	day1 := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 4, 2, 14, 0, 0, 0, time.UTC)

	require.NoError(t, s.Insert(ctx, db, &Record{
		ID: "01A", UserID: "u1", Function: FuncChat,
		Provider: "openai", Model: "gpt-4o", TotalTokens: 100,
		CreatedAt: day1,
	}))
	require.NoError(t, s.Insert(ctx, db, &Record{
		ID: "01B", UserID: "u1", Function: FuncChat,
		Provider: "openai", Model: "gpt-4o", TotalTokens: 200,
		CreatedAt: day2,
	}))

	from := day1.Add(-1 * time.Hour)
	to := day2.Add(1 * time.Hour)

	result, err := s.GetTimeSeries(ctx, db, from, to, "day")
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.Equal(t, "2026-04-01", result[0].Bucket)
	require.Equal(t, int64(100), result[0].TotalTokens)
	require.Equal(t, "2026-04-02", result[1].Bucket)
	require.Equal(t, int64(200), result[1].TotalTokens)
}

func TestStore_GetPeriodTotal(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := context.Background()
	s := NewStore()

	now := time.Now().UTC()
	require.NoError(t, s.Insert(ctx, db, &Record{
		ID: "01A", UserID: "u1", Function: FuncChat,
		Provider: "openai", Model: "gpt-4o", TotalTokens: 100,
		CreatedAt: now,
	}))
	require.NoError(t, s.Insert(ctx, db, &Record{
		ID: "01B", UserID: "u1", Function: FuncEmbedding,
		Provider: "ollama", Model: "nomic", TotalTokens: 80,
		IsLocal: true, CreatedAt: now,
	}))

	// gateLocal=false: only non-local
	total, err := s.GetPeriodTotal(ctx, db, "daily", false)
	require.NoError(t, err)
	require.Equal(t, int64(100), total)

	// gateLocal=true: all
	total, err = s.GetPeriodTotal(ctx, db, "daily", true)
	require.NoError(t, err)
	require.Equal(t, int64(180), total)
}

func TestStore_ConversationID(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := context.Background()
	s := NewStore()

	now := time.Now().UTC()
	require.NoError(t, s.Insert(ctx, db, &Record{
		ID: "01A", UserID: "u1", Function: FuncAssistant,
		Provider: "openai", Model: "gpt-4o", TotalTokens: 100,
		ConversationID: "conv_123", CreatedAt: now,
	}))
	require.NoError(t, s.Insert(ctx, db, &Record{
		ID: "01B", UserID: "u1", Function: FuncChat,
		Provider: "openai", Model: "gpt-4o", TotalTokens: 50,
		CreatedAt: now,
	}))

	// Both should be visible in summary.
	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)
	sum, err := s.GetSummary(ctx, db, from, to)
	require.NoError(t, err)
	require.Equal(t, int64(150), sum.TotalTokens)
}
