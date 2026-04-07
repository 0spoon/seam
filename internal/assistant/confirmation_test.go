package assistant

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfirmationManager_RequiresConfirmation(t *testing.T) {
	cm := NewConfirmationManager([]string{"create_note", "update_note"})

	require.True(t, cm.RequiresConfirmation("create_note"))
	require.True(t, cm.RequiresConfirmation("update_note"))
	require.False(t, cm.RequiresConfirmation("search_notes"))
	require.False(t, cm.RequiresConfirmation("read_note"))
}

func TestConfirmationManager_SetRequired(t *testing.T) {
	cm := NewConfirmationManager([]string{"create_note"})
	require.True(t, cm.RequiresConfirmation("create_note"))
	require.False(t, cm.RequiresConfirmation("delete_note"))

	cm.SetRequired([]string{"delete_note"})
	require.False(t, cm.RequiresConfirmation("create_note"))
	require.True(t, cm.RequiresConfirmation("delete_note"))
}

func TestConfirmationManager_Empty(t *testing.T) {
	cm := NewConfirmationManager(nil)
	require.False(t, cm.RequiresConfirmation("create_note"))
	require.False(t, cm.RequiresConfirmation("anything"))
}
