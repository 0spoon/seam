package usage

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/settings"
	"github.com/katata/seam/internal/userdb"
	"github.com/oklog/ulid/v2"
)

// ErrBudgetExceeded is returned when the token usage budget is exhausted.
var ErrBudgetExceeded = errors.New("token usage budget exceeded")

// contextKey is used for context-based overrides.
type contextKey string

const conversationIDKey contextKey = "usage_conversation_id"

// WithConversationID attaches a conversation ID to the context so the
// wrapper can associate usage records with a conversation.
func WithConversationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, conversationIDKey, id)
}

func conversationIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(conversationIDKey).(string)
	return v
}

// Tracker records token usage and optionally enforces budgets.
type Tracker struct {
	store       *Store
	dbManager   userdb.Manager
	settingsSvc *settings.Service
	logger      *slog.Logger
}

// NewTracker creates a Tracker. settingsSvc may be nil to disable budget enforcement.
func NewTracker(store *Store, dbManager userdb.Manager, settingsSvc *settings.Service, logger *slog.Logger) *Tracker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Tracker{
		store:       store,
		dbManager:   dbManager,
		settingsSvc: settingsSvc,
		logger:      logger,
	}
}

// Track persists a usage record. The record's ID and CreatedAt are
// populated automatically if empty.
func (t *Tracker) Track(ctx context.Context, r *Record) error {
	if r.ID == "" {
		id, err := ulid.New(ulid.Now(), rand.Reader)
		if err != nil {
			return fmt.Errorf("usage.Tracker.Track: generate id: %w", err)
		}
		r.ID = id.String()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	if r.UserID == "" {
		r.UserID = reqctx.UserIDFromContext(ctx)
	}
	if r.UserID == "" {
		r.UserID = userdb.DefaultUserID
	}

	r.ConversationID = conversationIDFromContext(ctx)

	db, err := t.dbManager.Open(ctx, r.UserID)
	if err != nil {
		return fmt.Errorf("usage.Tracker.Track: open db: %w", err)
	}

	if err := t.store.Insert(ctx, db, r); err != nil {
		return fmt.Errorf("usage.Tracker.Track: %w", err)
	}

	t.logger.Debug("token usage recorded",
		"function", r.Function, "provider", r.Provider, "model", r.Model,
		"input", r.InputTokens, "output", r.OutputTokens, "total", r.TotalTokens,
		"local", r.IsLocal, "duration_ms", r.DurationMS)
	return nil
}

// CheckBudget returns ErrBudgetExceeded if the current period's usage
// exceeds the configured budget. Returns nil if budgets are disabled.
func (t *Tracker) CheckBudget(ctx context.Context, userID string) error {
	if t.settingsSvc == nil {
		return nil
	}

	all, err := t.settingsSvc.GetAll(ctx, userID)
	if err != nil {
		t.logger.Warn("usage.Tracker.CheckBudget: failed to read settings", "error", err)
		return nil // fail open
	}

	if all["usage_budget_enabled"] != "true" {
		return nil
	}

	maxTokens, _ := strconv.ParseInt(all["usage_budget_max_tokens"], 10, 64)
	if maxTokens <= 0 {
		return nil // 0 = unlimited
	}

	period := all["usage_budget_period"]
	if period == "" {
		period = "monthly"
	}
	gateLocal := all["usage_budget_gate_local"] == "true"

	db, err := t.dbManager.Open(ctx, userID)
	if err != nil {
		t.logger.Warn("usage.Tracker.CheckBudget: failed to open db", "error", err)
		return nil // fail open
	}

	total, err := t.store.GetPeriodTotal(ctx, db, period, gateLocal)
	if err != nil {
		t.logger.Warn("usage.Tracker.CheckBudget: failed to query total", "error", err)
		return nil // fail open
	}

	if total >= maxTokens {
		return ErrBudgetExceeded
	}
	return nil
}
