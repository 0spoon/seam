package settings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/testutil"
)

func TestStore_SetAndGetAll(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewStore()
	ctx := context.Background()

	err := store.Set(ctx, db, "editor_view_mode", "preview")
	require.NoError(t, err)

	settings, err := store.GetAll(ctx, db)
	require.NoError(t, err)
	require.Equal(t, "preview", settings["editor_view_mode"])
}

func TestStore_Set_Upsert(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewStore()
	ctx := context.Background()

	require.NoError(t, store.Set(ctx, db, "sidebar_collapsed", "true"))
	require.NoError(t, store.Set(ctx, db, "sidebar_collapsed", "false"))

	settings, err := store.GetAll(ctx, db)
	require.NoError(t, err)
	require.Equal(t, "false", settings["sidebar_collapsed"])
}

func TestStore_Delete(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewStore()
	ctx := context.Background()

	require.NoError(t, store.Set(ctx, db, "right_panel_open", "false"))
	require.NoError(t, store.Delete(ctx, db, "right_panel_open"))

	settings, err := store.GetAll(ctx, db)
	require.NoError(t, err)
	_, exists := settings["right_panel_open"]
	require.False(t, exists)
}

func TestStore_GetAll_Empty(t *testing.T) {
	db := testutil.TestDB(t)
	store := NewStore()
	ctx := context.Background()

	settings, err := store.GetAll(ctx, db)
	require.NoError(t, err)
	require.Empty(t, settings)
}
