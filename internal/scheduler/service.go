package scheduler

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/katata/seam/internal/userdb"
)

// ActionRunner executes a single scheduled action. Implementations live in
// other packages (e.g. internal/briefing) and are registered with the
// scheduler at startup. The runner receives the user ID and the schedule's
// raw action_config JSON document.
type ActionRunner func(ctx context.Context, userID string, config json.RawMessage) error

// Sentinel validation errors.
var (
	ErrNameRequired       = errors.New("name is required")
	ErrInvalidActionType  = errors.New("invalid action type")
	ErrCronOrRunAtMissing = errors.New("cron_expr or run_at must be set")
	ErrCronAndRunAtBoth   = errors.New("cron_expr and run_at are mutually exclusive")
	ErrInvalidActionCfg   = errors.New("invalid action config")
)

// CreateReq is the input for creating a schedule. Either CronExpr or RunAt
// must be provided, but not both.
type CreateReq struct {
	// ID lets callers force a deterministic schedule ID. Leave empty to
	// auto-generate a ULID. Used by startup provisioning so the default
	// schedule has a stable identity that survives user renames.
	ID           string          `json:"-"`
	Name         string          `json:"name"`
	CronExpr     string          `json:"cron_expr,omitempty"`
	RunAt        *time.Time      `json:"run_at,omitempty"`
	ActionType   ActionType      `json:"action_type"`
	ActionConfig json.RawMessage `json:"action_config,omitempty"`
	Enabled      *bool           `json:"enabled,omitempty"`
}

// UpdateReq holds optional fields for an update.
type UpdateReq struct {
	Name         *string         `json:"name,omitempty"`
	CronExpr     *string         `json:"cron_expr,omitempty"`
	RunAt        *time.Time      `json:"run_at,omitempty"`
	ActionType   *ActionType     `json:"action_type,omitempty"`
	ActionConfig json.RawMessage `json:"action_config,omitempty"`
	Enabled      *bool           `json:"enabled,omitempty"`
}

// Service is the scheduler entry point. It owns the polling loop, the
// registry of action runners, and CRUD over the schedules table.
type Service struct {
	store     *Store
	dbManager userdb.Manager
	logger    *slog.Logger

	mu      sync.RWMutex
	runners map[ActionType]ActionRunner

	// tickInterval is how often the polling loop checks for due jobs.
	// Default 1 minute; tests can override via NewService config.
	tickInterval time.Duration

	// now returns the current time. Tests inject a deterministic clock.
	now func() time.Time
}

// Config bundles optional dependencies for NewService.
type Config struct {
	Store        *Store
	DBManager    userdb.Manager
	Logger       *slog.Logger
	TickInterval time.Duration // default: 1 minute
	Now          func() time.Time
}

// NewService creates a scheduler service.
func NewService(cfg Config) *Service {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		store:        cfg.Store,
		dbManager:    cfg.DBManager,
		logger:       cfg.Logger,
		runners:      make(map[ActionType]ActionRunner),
		tickInterval: cfg.TickInterval,
		now:          cfg.Now,
	}
}

// RegisterRunner attaches a runner for the given action type. Calling this
// twice for the same action overwrites the previous runner.
func (s *Service) RegisterRunner(action ActionType, fn ActionRunner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runners[action] = fn
}

func (s *Service) lookupRunner(action ActionType) (ActionRunner, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn, ok := s.runners[action]
	return fn, ok
}

// Create validates and stores a new schedule. The returned schedule has
// next_run_at populated based on cron_expr or run_at.
func (s *Service) Create(ctx context.Context, userID string, req CreateReq) (*Schedule, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("scheduler.Service.Create: %w", ErrNameRequired)
	}
	if !ValidActionTypes[req.ActionType] {
		return nil, fmt.Errorf("scheduler.Service.Create: %w: %q", ErrInvalidActionType, req.ActionType)
	}

	cronExpr := strings.TrimSpace(req.CronExpr)
	if cronExpr == "" && req.RunAt == nil {
		return nil, fmt.Errorf("scheduler.Service.Create: %w", ErrCronOrRunAtMissing)
	}
	if cronExpr != "" && req.RunAt != nil {
		return nil, fmt.Errorf("scheduler.Service.Create: %w", ErrCronAndRunAtBoth)
	}

	cfgJSON := strings.TrimSpace(string(req.ActionConfig))
	if cfgJSON == "" {
		cfgJSON = "{}"
	} else if !json.Valid([]byte(cfgJSON)) {
		return nil, fmt.Errorf("scheduler.Service.Create: %w", ErrInvalidActionCfg)
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	now := s.now()
	schID := strings.TrimSpace(req.ID)
	if schID == "" {
		id, err := ulid.New(ulid.Now(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("scheduler.Service.Create: generate id: %w", err)
		}
		schID = id.String()
	}

	sch := &Schedule{
		ID:           schID,
		Name:         name,
		CronExpr:     cronExpr,
		RunAt:        req.RunAt,
		ActionType:   req.ActionType,
		ActionConfig: cfgJSON,
		Enabled:      enabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	next, err := computeNextRun(sch, now)
	if err != nil {
		return nil, fmt.Errorf("scheduler.Service.Create: %w", err)
	}
	sch.NextRunAt = next

	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("scheduler.Service.Create: open db: %w", err)
	}
	if err := s.store.Create(ctx, db, sch); err != nil {
		return nil, fmt.Errorf("scheduler.Service.Create: %w", err)
	}
	s.logger.Info("schedule created",
		"id", sch.ID, "name", sch.Name, "action", sch.ActionType,
		"cron", sch.CronExpr, "next_run", formatPtr(sch.NextRunAt))
	return sch, nil
}

// Get returns a schedule by ID.
func (s *Service) Get(ctx context.Context, userID, id string) (*Schedule, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("scheduler.Service.Get: open db: %w", err)
	}
	sch, err := s.store.Get(ctx, db, id)
	if err != nil {
		return nil, fmt.Errorf("scheduler.Service.Get: %w", err)
	}
	return sch, nil
}

// List returns all schedules for a user.
func (s *Service) List(ctx context.Context, userID string, enabledOnly bool) ([]*Schedule, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("scheduler.Service.List: open db: %w", err)
	}
	out, err := s.store.List(ctx, db, enabledOnly)
	if err != nil {
		return nil, fmt.Errorf("scheduler.Service.List: %w", err)
	}
	return out, nil
}

// Update modifies an existing schedule. Only non-nil fields are touched.
func (s *Service) Update(ctx context.Context, userID, id string, req UpdateReq) (*Schedule, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("scheduler.Service.Update: open db: %w", err)
	}
	sch, err := s.store.Get(ctx, db, id)
	if err != nil {
		return nil, fmt.Errorf("scheduler.Service.Update: %w", err)
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, fmt.Errorf("scheduler.Service.Update: %w", ErrNameRequired)
		}
		sch.Name = name
	}
	if req.CronExpr != nil {
		sch.CronExpr = strings.TrimSpace(*req.CronExpr)
	}
	if req.RunAt != nil {
		t := *req.RunAt
		sch.RunAt = &t
	}
	if req.ActionType != nil {
		if !ValidActionTypes[*req.ActionType] {
			return nil, fmt.Errorf("scheduler.Service.Update: %w: %q", ErrInvalidActionType, *req.ActionType)
		}
		sch.ActionType = *req.ActionType
	}
	if len(req.ActionConfig) > 0 {
		if !json.Valid(req.ActionConfig) {
			return nil, fmt.Errorf("scheduler.Service.Update: %w", ErrInvalidActionCfg)
		}
		sch.ActionConfig = string(req.ActionConfig)
	}
	if req.Enabled != nil {
		sch.Enabled = *req.Enabled
	}

	if sch.CronExpr == "" && sch.RunAt == nil {
		return nil, fmt.Errorf("scheduler.Service.Update: %w", ErrCronOrRunAtMissing)
	}
	// Mirror Create's validation: cron and run_at are mutually
	// exclusive. Silently dropping run_at would hide caller mistakes
	// and surprise the API consumer with a 200 response that lost
	// fields.
	if sch.CronExpr != "" && sch.RunAt != nil {
		return nil, fmt.Errorf("scheduler.Service.Update: %w", ErrCronAndRunAtBoth)
	}

	sch.UpdatedAt = s.now()

	// Recompute next_run_at to reflect any cron_expr / run_at change. For a
	// disabled schedule we still record the next firing time so re-enabling
	// resumes immediately.
	next, err := computeNextRun(sch, sch.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("scheduler.Service.Update: %w", err)
	}
	sch.NextRunAt = next

	if err := s.store.Update(ctx, db, sch); err != nil {
		return nil, fmt.Errorf("scheduler.Service.Update: %w", err)
	}
	return sch, nil
}

// RunSchedule fires a single schedule by ID right now, regardless of its
// next_run_at. The on-disk last_run_at and next_run_at are updated to
// reflect the manual invocation. Used by the "run now" handler so users
// can preview a freshly created job without waiting for the next tick.
func (s *Service) RunSchedule(ctx context.Context, userID, id string) error {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("scheduler.Service.RunSchedule: open db: %w", err)
	}
	sch, err := s.store.Get(ctx, db, id)
	if err != nil {
		return fmt.Errorf("scheduler.Service.RunSchedule: %w", err)
	}
	now := s.now()
	s.dispatch(ctx, userID, db, sch, now)
	return nil
}

// Delete removes a schedule.
func (s *Service) Delete(ctx context.Context, userID, id string) error {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("scheduler.Service.Delete: open db: %w", err)
	}
	if err := s.store.Delete(ctx, db, id); err != nil {
		return fmt.Errorf("scheduler.Service.Delete: %w", err)
	}
	s.logger.Info("schedule deleted", "id", id)
	return nil
}

// RunOnce checks for due schedules across all users and runs them. Errors
// from individual schedules are logged but do not abort the loop. Returns
// the number of schedules that were dispatched (whether or not they
// succeeded).
func (s *Service) RunOnce(ctx context.Context) (int, error) {
	users, err := s.dbManager.ListUsers(ctx)
	if err != nil {
		return 0, fmt.Errorf("scheduler.Service.RunOnce: list users: %w", err)
	}

	now := s.now()
	dispatched := 0
	for _, userID := range users {
		n, runErr := s.runForUser(ctx, userID, now)
		dispatched += n
		if runErr != nil {
			s.logger.Warn("scheduler.Service.RunOnce: per-user error",
				"user_id", userID, "error", runErr)
		}
	}
	return dispatched, nil
}

func (s *Service) runForUser(ctx context.Context, userID string, now time.Time) (int, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("open db: %w", err)
	}
	due, err := s.store.ListDue(ctx, db, now)
	if err != nil {
		return 0, fmt.Errorf("list due: %w", err)
	}
	if len(due) == 0 {
		return 0, nil
	}

	for _, sch := range due {
		s.dispatch(ctx, userID, db, sch, now)
	}
	return len(due), nil
}

// dispatch runs the configured action for a single schedule and records
// the outcome (success or failure) plus the next firing time. Errors are
// logged and persisted but never propagated -- a single broken schedule
// must not block the rest of the queue.
func (s *Service) dispatch(ctx context.Context, userID string, db *sql.DB, sch *Schedule, now time.Time) {
	runner, ok := s.lookupRunner(sch.ActionType)
	if !ok {
		s.logger.Warn("scheduler.Service.dispatch: no runner registered",
			"id", sch.ID, "action", sch.ActionType)
		// Treat missing runner as a soft failure: advance next_run_at so
		// we don't spin on this schedule until the runner is wired in.
		next, _ := computeNextRun(sch, now)
		if err := s.store.MarkError(ctx, db, sch.ID, now, next, "no runner registered"); err != nil {
			s.logger.Warn("scheduler.Service.dispatch: mark error",
				"id", sch.ID, "error", err)
		}
		return
	}

	cfg := json.RawMessage(sch.ActionConfig)
	if len(cfg) == 0 {
		cfg = json.RawMessage("{}")
	}

	runErr := runner(ctx, userID, cfg)

	next, nextErr := computeNextRun(sch, now)
	if nextErr != nil {
		s.logger.Warn("scheduler.Service.dispatch: compute next",
			"id", sch.ID, "error", nextErr)
	}

	// One-shot schedules clear next_run_at after running so they never fire
	// again. The MarkRun/MarkError calls below honor a nil pointer.
	if sch.IsOneShot() {
		next = nil
	}

	if runErr != nil {
		s.logger.Warn("scheduler.Service.dispatch: action failed",
			"id", sch.ID, "action", sch.ActionType, "error", runErr)
		if err := s.store.MarkError(ctx, db, sch.ID, now, next, runErr.Error()); err != nil {
			s.logger.Warn("scheduler.Service.dispatch: mark error",
				"id", sch.ID, "error", err)
		}
		return
	}

	if err := s.store.MarkRun(ctx, db, sch.ID, now, next); err != nil {
		s.logger.Warn("scheduler.Service.dispatch: mark run",
			"id", sch.ID, "error", err)
	}
	s.logger.Info("schedule fired",
		"id", sch.ID, "action", sch.ActionType, "next", formatPtr(next))
}

// Run blocks until ctx is cancelled, ticking once per tickInterval and
// dispatching due jobs on each tick. Pass to a goroutine in main.
func (s *Service) Run(ctx context.Context) error {
	// Run one tick on startup so jobs that came due during downtime fire
	// promptly instead of waiting up to tickInterval.
	if _, err := s.RunOnce(ctx); err != nil {
		s.logger.Warn("scheduler.Service.Run: initial tick error", "error", err)
	}

	ticker := time.NewTicker(s.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := s.RunOnce(ctx); err != nil {
				s.logger.Warn("scheduler.Service.Run: tick error", "error", err)
			}
		}
	}
}

// computeNextRun returns the next firing time for a schedule, given a
// reference time. Recurring schedules use cron evaluation; one-shot
// schedules return their RunAt verbatim until last_run_at advances past it.
// A schedule that has already run and has no future firing returns nil.
func computeNextRun(sch *Schedule, after time.Time) (*time.Time, error) {
	if sch.IsRecurring() {
		expr, err := ParseCron(sch.CronExpr)
		if err != nil {
			return nil, err
		}
		next := expr.Next(after)
		if next.IsZero() {
			return nil, nil
		}
		return &next, nil
	}
	if sch.RunAt == nil {
		return nil, nil
	}
	// One-shot: keep its scheduled run_at as next_run_at until it has fired.
	if sch.LastRunAt != nil && !sch.LastRunAt.Before(*sch.RunAt) {
		return nil, nil
	}
	t := *sch.RunAt
	return &t, nil
}

func formatPtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
