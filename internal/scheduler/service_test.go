package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/katata/seam/internal/testutil"
)

// mockDBManager wraps an in-memory SQLite DB for one user.
type mockDBManager struct {
	db *sql.DB
}

func (m *mockDBManager) Open(ctx context.Context, userID string) (*sql.DB, error) {
	return m.db, nil
}
func (m *mockDBManager) Close(userID string) error             { return nil }
func (m *mockDBManager) CloseAll() error                       { return nil }
func (m *mockDBManager) UserNotesDir(userID string) string     { return "" }
func (m *mockDBManager) UserDataDir(userID string) string      { return "" }
func (m *mockDBManager) EnsureUserDirs(userID string) error    { return nil }
func (m *mockDBManager) ListUsers(ctx context.Context) ([]string, error) {
	return []string{"default"}, nil
}

func newTestService(t *testing.T, now time.Time) (*Service, *mockDBManager) {
	t.Helper()
	db := testutil.TestDB(t)
	mgr := &mockDBManager{db: db}
	svc := NewService(Config{
		Store:        NewStore(),
		DBManager:    mgr,
		TickInterval: time.Hour, // never auto-fires in tests
		Now:          func() time.Time { return now },
	})
	return svc, mgr
}

func TestService_Create_Recurring(t *testing.T) {
	now := time.Date(2026, 4, 6, 7, 0, 0, 0, time.UTC)
	svc, _ := newTestService(t, now)

	sch, err := svc.Create(context.Background(), "default", CreateReq{
		Name:       "Daily Briefing",
		CronExpr:   "0 8 * * *",
		ActionType: ActionBriefing,
	})
	require.NoError(t, err)
	require.NotEmpty(t, sch.ID)
	require.True(t, sch.Enabled)
	require.True(t, sch.IsRecurring())
	require.NotNil(t, sch.NextRunAt)
	// Next run is today at 08:00.
	require.Equal(t, time.Date(2026, 4, 6, 8, 0, 0, 0, time.UTC), *sch.NextRunAt)
}

func TestService_Create_OneShot(t *testing.T) {
	now := time.Date(2026, 4, 6, 7, 0, 0, 0, time.UTC)
	svc, _ := newTestService(t, now)

	runAt := time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC)
	sch, err := svc.Create(context.Background(), "default", CreateReq{
		Name:       "Once",
		RunAt:      &runAt,
		ActionType: ActionReminder,
	})
	require.NoError(t, err)
	require.True(t, sch.IsOneShot())
	require.NotNil(t, sch.NextRunAt)
	require.Equal(t, runAt, *sch.NextRunAt)
}

func TestService_Create_Validation(t *testing.T) {
	now := time.Now()
	svc, _ := newTestService(t, now)
	ctx := context.Background()

	cases := []struct {
		name string
		req  CreateReq
		err  error
	}{
		{
			name: "missing name",
			req:  CreateReq{ActionType: ActionBriefing, CronExpr: "* * * * *"},
			err:  ErrNameRequired,
		},
		{
			name: "invalid action",
			req:  CreateReq{Name: "x", ActionType: "bogus", CronExpr: "* * * * *"},
			err:  ErrInvalidActionType,
		},
		{
			name: "no schedule",
			req:  CreateReq{Name: "x", ActionType: ActionBriefing},
			err:  ErrCronOrRunAtMissing,
		},
		{
			name: "both",
			req: CreateReq{
				Name:       "x",
				ActionType: ActionBriefing,
				CronExpr:   "* * * * *",
				RunAt:      func() *time.Time { t := time.Now(); return &t }(),
			},
			err: ErrCronAndRunAtBoth,
		},
		{
			name: "invalid cron",
			req:  CreateReq{Name: "x", ActionType: ActionBriefing, CronExpr: "not a cron"},
			err:  ErrInvalidCron,
		},
		{
			name: "invalid action config",
			req: CreateReq{
				Name:         "x",
				ActionType:   ActionBriefing,
				CronExpr:     "* * * * *",
				ActionConfig: json.RawMessage("{not json"),
			},
			err: ErrInvalidActionCfg,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Create(ctx, "default", tc.req)
			require.Error(t, err)
			require.ErrorIs(t, err, tc.err)
		})
	}
}

func TestService_RunOnce_FiresDueRecurringSchedule(t *testing.T) {
	// Start before 08:00 so the schedule's first next_run_at is today 08:00.
	now := time.Date(2026, 4, 6, 7, 30, 0, 0, time.UTC)
	svc, _ := newTestService(t, now)
	ctx := context.Background()

	var fired atomic.Int32
	svc.RegisterRunner(ActionBriefing, func(ctx context.Context, userID string, cfg json.RawMessage) error {
		fired.Add(1)
		return nil
	})

	sch, err := svc.Create(ctx, "default", CreateReq{
		Name:       "Daily Briefing",
		CronExpr:   "0 8 * * *",
		ActionType: ActionBriefing,
	})
	require.NoError(t, err)

	// Move clock past 08:00 and tick.
	svc.now = func() time.Time { return time.Date(2026, 4, 6, 8, 0, 0, 0, time.UTC) }
	dispatched, err := svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, dispatched)
	require.Equal(t, int32(1), fired.Load())

	// Reload to verify next_run_at advanced to tomorrow.
	reloaded, err := svc.Get(ctx, "default", sch.ID)
	require.NoError(t, err)
	require.NotNil(t, reloaded.NextRunAt)
	require.Equal(t, time.Date(2026, 4, 7, 8, 0, 0, 0, time.UTC), *reloaded.NextRunAt)
	require.NotNil(t, reloaded.LastRunAt)
	require.Empty(t, reloaded.LastError)
}

func TestService_RunOnce_OneShotFiresOnceOnly(t *testing.T) {
	now := time.Date(2026, 4, 6, 7, 0, 0, 0, time.UTC)
	svc, _ := newTestService(t, now)
	ctx := context.Background()

	var fired atomic.Int32
	svc.RegisterRunner(ActionReminder, func(ctx context.Context, userID string, cfg json.RawMessage) error {
		fired.Add(1)
		return nil
	})

	runAt := time.Date(2026, 4, 6, 7, 30, 0, 0, time.UTC)
	sch, err := svc.Create(ctx, "default", CreateReq{
		Name:       "Once",
		RunAt:      &runAt,
		ActionType: ActionReminder,
	})
	require.NoError(t, err)

	// Tick #1 -- before run_at, nothing fires.
	dispatched, err := svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, dispatched)
	require.Equal(t, int32(0), fired.Load())

	// Tick #2 -- past run_at, fires.
	svc.now = func() time.Time { return time.Date(2026, 4, 6, 8, 0, 0, 0, time.UTC) }
	dispatched, err = svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, dispatched)
	require.Equal(t, int32(1), fired.Load())

	// Tick #3 -- already ran, should not fire again.
	dispatched, err = svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, dispatched)
	require.Equal(t, int32(1), fired.Load())

	// next_run_at is cleared after the one-shot fires.
	reloaded, err := svc.Get(ctx, "default", sch.ID)
	require.NoError(t, err)
	require.Nil(t, reloaded.NextRunAt)
	require.NotNil(t, reloaded.LastRunAt)
}

func TestService_RunOnce_RunnerErrorIsRecorded(t *testing.T) {
	now := time.Date(2026, 4, 6, 7, 30, 0, 0, time.UTC)
	svc, _ := newTestService(t, now)
	ctx := context.Background()

	svc.RegisterRunner(ActionBriefing, func(ctx context.Context, userID string, cfg json.RawMessage) error {
		return errors.New("runner blew up")
	})

	sch, err := svc.Create(ctx, "default", CreateReq{
		Name:       "Daily Briefing",
		CronExpr:   "0 8 * * *",
		ActionType: ActionBriefing,
	})
	require.NoError(t, err)

	svc.now = func() time.Time { return time.Date(2026, 4, 6, 8, 0, 0, 0, time.UTC) }
	dispatched, err := svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, dispatched)

	reloaded, err := svc.Get(ctx, "default", sch.ID)
	require.NoError(t, err)
	require.Equal(t, "runner blew up", reloaded.LastError)
	require.NotNil(t, reloaded.NextRunAt) // still advances so we don't spin
	require.Equal(t, time.Date(2026, 4, 7, 8, 0, 0, 0, time.UTC), *reloaded.NextRunAt)
}

func TestService_RunOnce_NoRunnerRegistered(t *testing.T) {
	now := time.Date(2026, 4, 6, 8, 30, 0, 0, time.UTC)
	svc, _ := newTestService(t, now)
	ctx := context.Background()

	// No runner registered for ActionBriefing.
	sch, err := svc.Create(ctx, "default", CreateReq{
		Name:       "Daily Briefing",
		CronExpr:   "0 8 * * *",
		ActionType: ActionBriefing,
	})
	require.NoError(t, err)
	// Schedule was created with next_run = today 08:00, but the create
	// happened at 08:30 so next_run is tomorrow. Force it back to past.
	yesterday := time.Date(2026, 4, 5, 8, 0, 0, 0, time.UTC)
	sch.NextRunAt = &yesterday
	db, _ := svc.dbManager.Open(ctx, "default")
	require.NoError(t, svc.store.Update(ctx, db, sch))

	dispatched, err := svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, dispatched)

	reloaded, err := svc.Get(ctx, "default", sch.ID)
	require.NoError(t, err)
	require.Equal(t, "no runner registered", reloaded.LastError)
}

func TestService_Update_RecomputesNextRun(t *testing.T) {
	now := time.Date(2026, 4, 6, 7, 0, 0, 0, time.UTC)
	svc, _ := newTestService(t, now)
	ctx := context.Background()

	sch, err := svc.Create(ctx, "default", CreateReq{
		Name:       "Daily Briefing",
		CronExpr:   "0 8 * * *",
		ActionType: ActionBriefing,
	})
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 4, 6, 8, 0, 0, 0, time.UTC), *sch.NextRunAt)

	newCron := "0 18 * * *" // 6 PM
	updated, err := svc.Update(ctx, "default", sch.ID, UpdateReq{CronExpr: &newCron})
	require.NoError(t, err)
	require.Equal(t, "0 18 * * *", updated.CronExpr)
	require.NotNil(t, updated.NextRunAt)
	require.Equal(t, time.Date(2026, 4, 6, 18, 0, 0, 0, time.UTC), *updated.NextRunAt)
}

func TestService_Delete(t *testing.T) {
	svc, _ := newTestService(t, time.Now())
	ctx := context.Background()

	sch, err := svc.Create(ctx, "default", CreateReq{
		Name:       "Daily Briefing",
		CronExpr:   "0 8 * * *",
		ActionType: ActionBriefing,
	})
	require.NoError(t, err)

	require.NoError(t, svc.Delete(ctx, "default", sch.ID))

	_, err = svc.Get(ctx, "default", sch.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestService_RunSchedule_FiresOnDemand(t *testing.T) {
	now := time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC)
	svc, _ := newTestService(t, now)
	ctx := context.Background()

	var fired sync.WaitGroup
	fired.Add(1)
	svc.RegisterRunner(ActionBriefing, func(ctx context.Context, userID string, cfg json.RawMessage) error {
		fired.Done()
		return nil
	})

	sch, err := svc.Create(ctx, "default", CreateReq{
		Name:       "Daily Briefing",
		CronExpr:   "0 8 * * *",
		ActionType: ActionBriefing,
	})
	require.NoError(t, err)

	// Even though next_run is tomorrow, RunSchedule fires immediately.
	require.NoError(t, svc.RunSchedule(ctx, "default", sch.ID))
	fired.Wait()

	reloaded, err := svc.Get(ctx, "default", sch.ID)
	require.NoError(t, err)
	require.NotNil(t, reloaded.LastRunAt)
}

func TestService_DisabledScheduleSkipped(t *testing.T) {
	now := time.Date(2026, 4, 6, 7, 30, 0, 0, time.UTC)
	svc, _ := newTestService(t, now)
	ctx := context.Background()

	var fired atomic.Int32
	svc.RegisterRunner(ActionBriefing, func(ctx context.Context, userID string, cfg json.RawMessage) error {
		fired.Add(1)
		return nil
	})

	enabled := false
	sch, err := svc.Create(ctx, "default", CreateReq{
		Name:       "Daily Briefing",
		CronExpr:   "0 8 * * *",
		ActionType: ActionBriefing,
		Enabled:    &enabled,
	})
	require.NoError(t, err)
	require.False(t, sch.Enabled)

	svc.now = func() time.Time { return time.Date(2026, 4, 6, 9, 0, 0, 0, time.UTC) }
	dispatched, err := svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, dispatched)
	require.Equal(t, int32(0), fired.Load())
}
