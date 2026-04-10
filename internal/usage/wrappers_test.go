package usage

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/testutil"
)

// mockChatCompleter returns a fixed response.
type mockChatCompleter struct {
	response *ai.ChatResponse
}

func (m *mockChatCompleter) ChatCompletion(_ context.Context, _ string, _ []ai.ChatMessage) (*ai.ChatResponse, error) {
	return m.response, nil
}

func (m *mockChatCompleter) ChatCompletionStream(_ context.Context, _ string, _ []ai.ChatMessage) (<-chan string, <-chan error) {
	tokenCh := make(chan string, 2)
	errCh := make(chan error)
	tokenCh <- "hello "
	tokenCh <- "world"
	close(tokenCh)
	close(errCh)
	return tokenCh, errCh
}

// mockToolChatCompleter returns a fixed response.
type mockToolChatCompleter struct {
	response *ai.ToolChatResponse
}

func (m *mockToolChatCompleter) ChatCompletionWithTools(_ context.Context, _ string, _ []ai.ToolMessage, _ []ai.ToolDefinition) (*ai.ToolChatResponse, error) {
	return m.response, nil
}

// mockEmbedder returns a fixed embedding.
type mockEmbedder struct{}

func (m *mockEmbedder) GenerateEmbedding(_ context.Context, _, _ string) ([]float64, error) {
	return []float64{0.1, 0.2, 0.3}, nil
}

// stubManager implements userdb.Manager for tests, returning a pre-opened DB.
type stubManager struct {
	db *sql.DB
}

func (s *stubManager) Open(_ context.Context, _ string) (*sql.DB, error) { return s.db, nil }
func (s *stubManager) Close(_ string) error                              { return nil }
func (s *stubManager) CloseAll() error                                   { return nil }
func (s *stubManager) UserNotesDir(_ string) string                      { return "/tmp" }
func (s *stubManager) UserDataDir(_ string) string                       { return "/tmp" }
func (s *stubManager) ListUsers(_ context.Context) ([]string, error)     { return []string{"u1"}, nil }
func (s *stubManager) EnsureUserDirs(_ string) error                     { return nil }

func TestTrackedChatCompleter_ChatCompletion(t *testing.T) {
	db := testutil.TestDB(t)
	mgr := &stubManager{db: db}
	store := NewStore()
	tracker := NewTracker(store, mgr, nil, nil)

	inner := &mockChatCompleter{
		response: &ai.ChatResponse{
			Content: "Hello!",
			Usage: &ai.TokenUsage{
				InputTokens:  10,
				OutputTokens: 5,
				TotalTokens:  15,
			},
		},
	}

	tracked := NewTrackedChatCompleter(inner, tracker, "openai", FuncChat)
	ctx := reqctx.WithUserID(context.Background(), "u1")

	resp, err := tracked.ChatCompletion(ctx, "gpt-4o", []ai.ChatMessage{
		{Role: "user", Content: "Hi"},
	})
	require.NoError(t, err)
	require.Equal(t, "Hello!", resp.Content)

	// Give the tracker a moment, then check the DB.
	now := time.Now().UTC()
	sum, err := store.GetSummary(ctx, db, now.Add(-1*time.Minute), now.Add(1*time.Minute))
	require.NoError(t, err)
	require.Equal(t, int64(15), sum.TotalTokens)
	require.Equal(t, int64(1), sum.CallCount)

	byModel, err := store.GetByModel(ctx, db, now.Add(-1*time.Minute), now.Add(1*time.Minute))
	require.NoError(t, err)
	require.Len(t, byModel, 1)
	require.Equal(t, "gpt-4o", byModel[0].Model)
	require.Equal(t, "openai", byModel[0].Provider)
}

func TestTrackedChatCompleter_ChatCompletionStream(t *testing.T) {
	db := testutil.TestDB(t)
	mgr := &stubManager{db: db}
	store := NewStore()
	tracker := NewTracker(store, mgr, nil, nil)

	inner := &mockChatCompleter{}
	tracked := NewTrackedChatCompleter(inner, tracker, "ollama", FuncSynthesis)
	ctx := reqctx.WithUserID(context.Background(), "u1")

	tokenCh, errCh := tracked.ChatCompletionStream(ctx, "llama3", nil)

	var tokens []string
	for tok := range tokenCh {
		tokens = append(tokens, tok)
	}
	for err := range errCh {
		require.NoError(t, err)
	}

	require.Equal(t, []string{"hello ", "world"}, tokens)

	// Wait briefly for the background tracking goroutine.
	time.Sleep(50 * time.Millisecond)

	now := time.Now().UTC()
	sum, err := store.GetSummary(ctx, db, now.Add(-1*time.Minute), now.Add(1*time.Minute))
	require.NoError(t, err)
	require.Equal(t, int64(1), sum.CallCount)
	require.Greater(t, sum.TotalTokens, int64(0))
}

func TestTrackedToolChatCompleter(t *testing.T) {
	db := testutil.TestDB(t)
	mgr := &stubManager{db: db}
	store := NewStore()
	tracker := NewTracker(store, mgr, nil, nil)

	inner := &mockToolChatCompleter{
		response: &ai.ToolChatResponse{
			Content:      "I found something.",
			FinishReason: "stop",
			Usage: &ai.TokenUsage{
				InputTokens:  200,
				OutputTokens: 50,
				TotalTokens:  250,
			},
		},
	}

	tracked := NewTrackedToolChatCompleter(inner, tracker, "anthropic", FuncAssistant)
	ctx := reqctx.WithUserID(context.Background(), "u1")

	resp, err := tracked.ChatCompletionWithTools(ctx, "claude-sonnet-4-20250514", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "I found something.", resp.Content)

	now := time.Now().UTC()
	byModel, err := store.GetByModel(ctx, db, now.Add(-1*time.Minute), now.Add(1*time.Minute))
	require.NoError(t, err)
	require.Len(t, byModel, 1)
	require.Equal(t, "claude-sonnet-4-20250514", byModel[0].Model)
	require.Equal(t, "anthropic", byModel[0].Provider)
	require.Equal(t, int64(250), byModel[0].TotalTokens)
}

func TestTrackedEmbedder(t *testing.T) {
	db := testutil.TestDB(t)
	mgr := &stubManager{db: db}
	store := NewStore()
	tracker := NewTracker(store, mgr, nil, nil)

	inner := &mockEmbedder{}
	tracked := NewTrackedEmbedder(inner, tracker, "ollama")
	ctx := reqctx.WithUserID(context.Background(), "u1")

	vec, err := tracked.GenerateEmbedding(ctx, "nomic-embed-text", "The quick brown fox")
	require.NoError(t, err)
	require.Len(t, vec, 3)

	now := time.Now().UTC()
	byFunc, err := store.GetByFunction(ctx, db, now.Add(-1*time.Minute), now.Add(1*time.Minute))
	require.NoError(t, err)
	require.Len(t, byFunc, 1)
	require.Equal(t, "embedding", byFunc[0].Function)
	require.Greater(t, byFunc[0].TotalTokens, int64(0))
}

func TestTrackedChatCompleter_EstimatesFallback(t *testing.T) {
	db := testutil.TestDB(t)
	mgr := &stubManager{db: db}
	store := NewStore()
	tracker := NewTracker(store, mgr, nil, nil)

	// Response without Usage -- should fall back to estimation.
	inner := &mockChatCompleter{
		response: &ai.ChatResponse{
			Content: "This is a response without usage data.",
		},
	}

	tracked := NewTrackedChatCompleter(inner, tracker, "ollama", FuncChat)
	ctx := reqctx.WithUserID(context.Background(), "u1")

	resp, err := tracked.ChatCompletion(ctx, "llama3", []ai.ChatMessage{
		{Role: "user", Content: "Tell me something"},
	})
	require.NoError(t, err)
	require.Equal(t, "This is a response without usage data.", resp.Content)

	now := time.Now().UTC()
	sum, err := store.GetSummary(ctx, db, now.Add(-1*time.Minute), now.Add(1*time.Minute))
	require.NoError(t, err)
	require.Equal(t, int64(1), sum.CallCount)
	require.Greater(t, sum.TotalTokens, int64(0))
}
