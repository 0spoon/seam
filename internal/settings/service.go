package settings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/katata/seam/internal/userdb"
)

// allowedKeys maps setting keys to their allowed values. A nil slice
// means any string value is accepted.
var allowedKeys = map[string][]string{
	"editor_view_mode":          {"editor", "split", "preview"},
	"right_panel_open":          {"true", "false"},
	"sidebar_collapsed":         {"true", "false"},
	"sidebar_projects_expanded": {"true", "false"},
	"sidebar_tags_expanded":     {"true", "false"},
	"zen_mode_typewriter":       {"true", "false"},
}

// defaultValues maps setting keys to their default values. These are
// returned by GetAll when a key has never been explicitly set.
var defaultValues = map[string]string{
	"editor_view_mode":          "split",
	"right_panel_open":          "true",
	"sidebar_collapsed":         "false",
	"sidebar_projects_expanded": "true",
	"sidebar_tags_expanded":     "true",
	"zen_mode_typewriter":       "false",
}

// ErrInvalidKey is returned when a setting key is not in the allowlist.
var ErrInvalidKey = errors.New("invalid setting key")

// ErrInvalidValue is returned when a setting value is not valid for the key.
var ErrInvalidValue = errors.New("invalid setting value")

// Service handles settings business logic.
type Service struct {
	store         *Store
	userDBManager userdb.Manager
	logger        *slog.Logger
}

// NewService creates a new settings Service.
func NewService(store *Store, userDBManager userdb.Manager, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:         store,
		userDBManager: userDBManager,
		logger:        logger,
	}
}

// GetAll retrieves all settings for a user, merged with defaults so
// that every known key is present in the response.
func (s *Service) GetAll(ctx context.Context, userID string) (map[string]string, error) {
	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("settings.Service.GetAll: open db: %w", err)
	}

	stored, err := s.store.GetAll(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("settings.Service.GetAll: %w", err)
	}

	// Start with defaults and overlay stored values.
	merged := make(map[string]string, len(defaultValues))
	for k, v := range defaultValues {
		merged[k] = v
	}
	for k, v := range stored {
		merged[k] = v
	}
	return merged, nil
}

// Update upserts one or more settings inside a single transaction.
// Each key is validated against the allowlist before writing. Returns
// the first validation error encountered.
func (s *Service) Update(ctx context.Context, userID string, settings map[string]string) error {
	// Validate all keys and values before writing.
	for key, value := range settings {
		if err := validateSetting(key, value); err != nil {
			return fmt.Errorf("settings.Service.Update: key %q: %w", key, err)
		}
	}

	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("settings.Service.Update: open db: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("settings.Service.Update: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for key, value := range settings {
		if err = s.store.Set(ctx, tx, key, value); err != nil {
			return fmt.Errorf("settings.Service.Update: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("settings.Service.Update: commit: %w", err)
	}

	s.logger.Debug("settings updated", "user_id", userID, "count", len(settings))
	return nil
}

// Delete removes a single setting, resetting it to default.
func (s *Service) Delete(ctx context.Context, userID, key string) error {
	if _, ok := allowedKeys[key]; !ok {
		return fmt.Errorf("settings.Service.Delete: %w", ErrInvalidKey)
	}

	db, err := s.userDBManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("settings.Service.Delete: open db: %w", err)
	}

	if err := s.store.Delete(ctx, db, key); err != nil {
		return fmt.Errorf("settings.Service.Delete: %w", err)
	}

	s.logger.Debug("setting deleted", "user_id", userID, "key", key)
	return nil
}

// validateSetting checks that a key is in the allowlist and the value is valid.
func validateSetting(key, value string) error {
	allowed, ok := allowedKeys[key]
	if !ok {
		return ErrInvalidKey
	}
	if allowed == nil {
		return nil
	}
	for _, v := range allowed {
		if value == v {
			return nil
		}
	}
	return ErrInvalidValue
}
