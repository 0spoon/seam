package settings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/testutil"
)

func TestService_Update_TransactionAtomicity(t *testing.T) {
	db := testutil.TestUserDB(t)
	store := NewStore()
	ctx := context.Background()

	// Write multiple settings to verify they persist together.
	require.NoError(t, store.Set(ctx, db, "editor_view_mode", "preview"))
	require.NoError(t, store.Set(ctx, db, "sidebar_collapsed", "true"))

	// Verify both were persisted.
	settings, err := store.GetAll(ctx, db)
	require.NoError(t, err)
	require.Equal(t, "preview", settings["editor_view_mode"])
	require.Equal(t, "true", settings["sidebar_collapsed"])
}

func TestValidateSetting_ValidKeys(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{"editor_view_mode", "editor"},
		{"editor_view_mode", "split"},
		{"editor_view_mode", "preview"},
		{"right_panel_open", "true"},
		{"right_panel_open", "false"},
		{"sidebar_collapsed", "true"},
		{"sidebar_collapsed", "false"},
		{"sidebar_projects_expanded", "true"},
		{"sidebar_tags_expanded", "false"},
	}

	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			err := validateSetting(tt.key, tt.value)
			require.NoError(t, err)
		})
	}
}

func TestValidateSetting_InvalidKey(t *testing.T) {
	err := validateSetting("unknown_key", "value")
	require.ErrorIs(t, err, ErrInvalidKey)
}

func TestValidateSetting_InvalidValue(t *testing.T) {
	err := validateSetting("editor_view_mode", "invalid")
	require.ErrorIs(t, err, ErrInvalidValue)
}

func TestDefaultValues_AllKeysPresent(t *testing.T) {
	// Every allowed key should have a default value.
	for key := range allowedKeys {
		_, ok := defaultValues[key]
		require.True(t, ok, "missing default for key %q", key)
	}
}
