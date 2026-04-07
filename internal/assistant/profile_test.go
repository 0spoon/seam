package assistant

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProfileStore_SaveAndGet(t *testing.T) {
	db := setupTestDB(t)
	store := NewProfileStore()

	profile := &UserProfile{
		DisplayName:   "Alice",
		Profession:    "Software Engineer",
		Organization:  "Acme Corp",
		Goals:         "Build a personal knowledge system",
		Interests:     "Go, distributed systems, AI",
		Timezone:      "America/New_York",
		Communication: "concise",
		Instructions:  "Always suggest next actions",
	}

	err := store.SaveProfile(context.Background(), db, profile)
	require.NoError(t, err)

	got, err := store.GetProfile(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, "Alice", got.DisplayName)
	require.Equal(t, "Software Engineer", got.Profession)
	require.Equal(t, "Acme Corp", got.Organization)
	require.Equal(t, "Build a personal knowledge system", got.Goals)
	require.Equal(t, "Go, distributed systems, AI", got.Interests)
	require.Equal(t, "America/New_York", got.Timezone)
	require.Equal(t, "concise", got.Communication)
	require.Equal(t, "Always suggest next actions", got.Instructions)
}

func TestProfileStore_PartialUpdate(t *testing.T) {
	db := setupTestDB(t)
	store := NewProfileStore()

	// Initial save.
	profile := &UserProfile{
		DisplayName: "Alice",
		Profession:  "Engineer",
	}
	require.NoError(t, store.SaveProfile(context.Background(), db, profile))

	// Partial update -- only change profession.
	update := &UserProfile{
		DisplayName: "Alice",
		Profession:  "Senior Engineer",
	}
	require.NoError(t, store.SaveProfile(context.Background(), db, update))

	got, err := store.GetProfile(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, "Alice", got.DisplayName)
	require.Equal(t, "Senior Engineer", got.Profession)
}

func TestProfileStore_EmptyFieldPreserves(t *testing.T) {
	db := setupTestDB(t)
	store := NewProfileStore()

	// Save with a profession.
	profile := &UserProfile{
		DisplayName: "Alice",
		Profession:  "Engineer",
	}
	require.NoError(t, store.SaveProfile(context.Background(), db, profile))

	// Save with empty profession -- should preserve existing value.
	update := &UserProfile{
		DisplayName: "Alice",
		Profession:  "", // empty -- existing value preserved
	}
	require.NoError(t, store.SaveProfile(context.Background(), db, update))

	got, err := store.GetProfile(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, "Alice", got.DisplayName)
	require.Equal(t, "Engineer", got.Profession, "empty field should preserve existing value")
}

func TestProfileStore_ExplicitClearViaUpdateField(t *testing.T) {
	db := setupTestDB(t)
	store := NewProfileStore()

	// Set a field.
	require.NoError(t, store.UpdateProfileField(context.Background(), db, "profession", "Engineer"))

	got, err := store.GetProfile(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, "Engineer", got.Profession)

	// Explicitly clear via UpdateProfileField.
	require.NoError(t, store.UpdateProfileField(context.Background(), db, "profession", ""))

	got, err = store.GetProfile(context.Background(), db)
	require.NoError(t, err)
	require.Empty(t, got.Profession, "field should be cleared")
}

func TestProfileStore_GetProfile_Empty(t *testing.T) {
	db := setupTestDB(t)
	store := NewProfileStore()

	got, err := store.GetProfile(context.Background(), db)
	require.NoError(t, err)
	require.True(t, got.IsEmpty())
}

func TestProfileStore_UpdateProfileField(t *testing.T) {
	db := setupTestDB(t)
	store := NewProfileStore()

	err := store.UpdateProfileField(context.Background(), db, "display_name", "Bob")
	require.NoError(t, err)

	got, err := store.GetProfile(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, "Bob", got.DisplayName)
}

func TestProfileStore_UpdateProfileField_InvalidField(t *testing.T) {
	db := setupTestDB(t)
	store := NewProfileStore()

	err := store.UpdateProfileField(context.Background(), db, "invalid_field", "value")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown field")
}

func TestUserProfile_FormatForPrompt(t *testing.T) {
	profile := &UserProfile{
		DisplayName: "Alice",
		Profession:  "Engineer",
		Goals:       "Build great software",
	}

	prompt := profile.FormatForPrompt()
	require.Contains(t, prompt, "Name: Alice")
	require.Contains(t, prompt, "Profession: Engineer")
	require.Contains(t, prompt, "Goals: Build great software")
}

func TestUserProfile_FormatForPrompt_Empty(t *testing.T) {
	profile := &UserProfile{}
	prompt := profile.FormatForPrompt()
	require.Empty(t, prompt)
}

func TestUserProfile_IsEmpty(t *testing.T) {
	require.True(t, (&UserProfile{}).IsEmpty())
	require.False(t, (&UserProfile{DisplayName: "Alice"}).IsEmpty())
}
