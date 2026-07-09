package usage

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/testutil"
	"github.com/katata/seam/internal/userdb"
)

func TestRetrievalStore_InsertAndSummary(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewRetrievalStore()
	ctx := context.Background()
	now := time.Now().UTC()

	events := []*RetrievalEvent{
		{ID: "e1", UserID: "u", Kind: RetrievalKindBriefing, Hit: true, CreatedAt: now.Add(-time.Hour)},
		{ID: "e2", UserID: "u", Kind: RetrievalKindPromptContext, Items: []string{"protocol/x"}, Hit: true, CreatedAt: now.Add(-30 * time.Minute)},
		{ID: "e3", UserID: "u", Kind: RetrievalKindPromptContext, Items: []string{"protocol/y"}, Hit: false, CreatedAt: now.Add(-20 * time.Minute)},
	}
	for _, ev := range events {
		require.NoError(t, store.Insert(ctx, db, ev))
	}

	sum, err := store.Summary(ctx, db, now.Add(-2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 3, sum.Total)

	byKind := map[string]RetrievalKindStat{}
	for _, k := range sum.Kinds {
		byKind[k.Kind] = k
	}
	require.Equal(t, 1, byKind[RetrievalKindBriefing].Total)
	require.Equal(t, 2, byKind[RetrievalKindPromptContext].Total)
	require.Equal(t, 1, byKind[RetrievalKindPromptContext].Hits)
}

func TestRetrievalStore_ReadAfterInject_WithinWindow(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewRetrievalStore()
	ctx := context.Background()
	now := time.Now().UTC()

	// Injection of protocol/x followed by a read of protocol/x 2 minutes later.
	require.NoError(t, store.Insert(ctx, db, &RetrievalEvent{ID: "i1", UserID: "u", Kind: RetrievalKindPromptContext, Items: []string{"protocol/x"}, Hit: true, CreatedAt: now.Add(-10 * time.Minute)}))
	require.NoError(t, store.Insert(ctx, db, &RetrievalEvent{ID: "r1", UserID: "u", Kind: RetrievalKindMemoryRead, Items: []string{"protocol/x"}, Hit: true, CreatedAt: now.Add(-8 * time.Minute)}))
	// A second injection with no follow-up read.
	require.NoError(t, store.Insert(ctx, db, &RetrievalEvent{ID: "i2", UserID: "u", Kind: RetrievalKindPromptContext, Items: []string{"protocol/z"}, Hit: true, CreatedAt: now.Add(-5 * time.Minute)}))

	sum, err := store.Summary(ctx, db, now.Add(-time.Hour))
	require.NoError(t, err)
	require.Equal(t, 2, sum.InjectionEvents)
	require.Equal(t, 1, sum.ReadFollowups)
	require.InDelta(t, 0.5, sum.ReadAfterInjectRate, 0.001)
}

func TestRetrievalStore_ReadOutsideWindow_NotCounted(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewRetrievalStore()
	ctx := context.Background()
	now := time.Now().UTC()

	require.NoError(t, store.Insert(ctx, db, &RetrievalEvent{ID: "i1", UserID: "u", Kind: RetrievalKindPromptContext, Items: []string{"protocol/x"}, Hit: true, CreatedAt: now.Add(-40 * time.Minute)}))
	// Read 20 minutes after the injection -> outside the 15-minute window.
	require.NoError(t, store.Insert(ctx, db, &RetrievalEvent{ID: "r1", UserID: "u", Kind: RetrievalKindMemoryRead, Items: []string{"protocol/x"}, Hit: true, CreatedAt: now.Add(-20 * time.Minute)}))

	sum, err := store.Summary(ctx, db, now.Add(-time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, sum.InjectionEvents)
	require.Equal(t, 0, sum.ReadFollowups)
	require.Equal(t, 0.0, sum.ReadAfterInjectRate)
}

func TestRetrievalStore_TruncatesQuery(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewRetrievalStore()
	ctx := context.Background()

	longQuery := strings.Repeat("a", 600)
	require.NoError(t, store.Insert(ctx, db, &RetrievalEvent{ID: "e1", UserID: "u", Kind: RetrievalKindRecall, Query: longQuery, CreatedAt: time.Now().UTC()}))

	var got string
	require.NoError(t, db.QueryRow("SELECT query FROM retrieval_events WHERE id = 'e1'").Scan(&got))
	require.Equal(t, 500, len([]rune(got)))
}

func TestHandler_GetRetrieval(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := userdb.NewSQLManager(testutil.TestDataDir(t), logger)
	t.Cleanup(func() { mgr.CloseAll() })

	ctx := context.Background()
	db, err := mgr.Open(ctx, userdb.DefaultUserID)
	require.NoError(t, err)

	store := NewRetrievalStore()
	now := time.Now().UTC()
	require.NoError(t, store.Insert(ctx, db, &RetrievalEvent{ID: "i1", UserID: userdb.DefaultUserID, Kind: RetrievalKindPromptContext, Items: []string{"protocol/x"}, Hit: true, CreatedAt: now.Add(-time.Minute)}))
	require.NoError(t, store.Insert(ctx, db, &RetrievalEvent{ID: "r1", UserID: userdb.DefaultUserID, Kind: RetrievalKindMemoryRead, Items: []string{"protocol/x"}, Hit: true, CreatedAt: now}))

	h := NewHandler(nil, store, mgr, nil, logger)

	req := httptest.NewRequest(http.MethodGet, "/retrieval?days=7", nil)
	req = req.WithContext(reqctx.WithUserID(req.Context(), userdb.DefaultUserID))
	w := httptest.NewRecorder()
	h.getRetrieval(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	require.Contains(t, body, `"read_after_inject_rate":1`)
	require.Contains(t, body, `"total":2`)
}
