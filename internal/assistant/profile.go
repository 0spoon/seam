package assistant

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// UserProfile represents the user's structured profile.
// Each field is stored as a separate row in the user_profile table.
type UserProfile struct {
	DisplayName   string `json:"display_name,omitempty"`
	Profession    string `json:"profession,omitempty"`
	Organization  string `json:"organization,omitempty"`
	Goals         string `json:"goals,omitempty"`
	Interests     string `json:"interests,omitempty"`
	Timezone      string `json:"timezone,omitempty"`
	Communication string `json:"communication,omitempty"` // concise, detailed, formal, casual
	Instructions  string `json:"instructions,omitempty"`  // custom instructions for assistant
}

// Known profile field names mapped to their UserProfile struct fields.
var profileFields = []string{
	"display_name", "profession", "organization",
	"goals", "interests", "timezone",
	"communication", "instructions",
}

// ProfileStore provides CRUD operations for the user profile.
type ProfileStore struct{}

// NewProfileStore creates a new ProfileStore.
func NewProfileStore() *ProfileStore {
	return &ProfileStore{}
}

// GetProfile loads the full user profile from the user_profile table.
func (s *ProfileStore) GetProfile(ctx context.Context, db *sql.DB) (*UserProfile, error) {
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM user_profile`)
	if err != nil {
		return nil, fmt.Errorf("assistant.ProfileStore.GetProfile: %w", err)
	}
	defer rows.Close()

	p := &UserProfile{}
	for rows.Next() {
		var key, value string
		if scanErr := rows.Scan(&key, &value); scanErr != nil {
			return nil, fmt.Errorf("assistant.ProfileStore.GetProfile: scan: %w", scanErr)
		}
		setProfileField(p, key, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("assistant.ProfileStore.GetProfile: rows: %w", err)
	}
	return p, nil
}

// SaveProfile upserts non-empty profile fields. Empty fields are left unchanged
// (not deleted). To explicitly clear a field, use UpdateProfileField with an empty value.
func (s *ProfileStore) SaveProfile(ctx context.Context, db *sql.DB, p *UserProfile) error {
	fields := map[string]string{
		"display_name":  p.DisplayName,
		"profession":    p.Profession,
		"organization":  p.Organization,
		"goals":         p.Goals,
		"interests":     p.Interests,
		"timezone":      p.Timezone,
		"communication": p.Communication,
		"instructions":  p.Instructions,
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("assistant.ProfileStore.SaveProfile: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for key, value := range fields {
		// Skip empty values -- they are left unchanged.
		if value == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_profile (key, value, updated_at) VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			key, value); err != nil {
			return fmt.Errorf("assistant.ProfileStore.SaveProfile: upsert %s: %w", key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("assistant.ProfileStore.SaveProfile: commit: %w", err)
	}
	return nil
}

// UpdateProfileField updates a single profile field.
func (s *ProfileStore) UpdateProfileField(ctx context.Context, db *sql.DB, key, value string) error {
	if !isValidProfileField(key) {
		return fmt.Errorf("assistant.ProfileStore.UpdateProfileField: unknown field %q", key)
	}
	if value == "" {
		_, err := db.ExecContext(ctx, `DELETE FROM user_profile WHERE key = ?`, key)
		if err != nil {
			return fmt.Errorf("assistant.ProfileStore.UpdateProfileField: %w", err)
		}
		return nil
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO user_profile (key, value, updated_at) VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value)
	if err != nil {
		return fmt.Errorf("assistant.ProfileStore.UpdateProfileField: %w", err)
	}
	return nil
}

func isValidProfileField(key string) bool {
	for _, f := range profileFields {
		if f == key {
			return true
		}
	}
	return false
}

func setProfileField(p *UserProfile, key, value string) {
	switch key {
	case "display_name":
		p.DisplayName = value
	case "profession":
		p.Profession = value
	case "organization":
		p.Organization = value
	case "goals":
		p.Goals = value
	case "interests":
		p.Interests = value
	case "timezone":
		p.Timezone = value
	case "communication":
		p.Communication = value
	case "instructions":
		p.Instructions = value
	}
}

// FormatForPrompt returns a human-readable string representation
// of the profile suitable for inclusion in a system prompt.
func (p *UserProfile) FormatForPrompt() string {
	var parts []string
	if p.DisplayName != "" {
		parts = append(parts, fmt.Sprintf("Name: %s", p.DisplayName))
	}
	if p.Profession != "" {
		parts = append(parts, fmt.Sprintf("Profession: %s", p.Profession))
	}
	if p.Organization != "" {
		parts = append(parts, fmt.Sprintf("Organization: %s", p.Organization))
	}
	if p.Goals != "" {
		parts = append(parts, fmt.Sprintf("Goals: %s", p.Goals))
	}
	if p.Interests != "" {
		parts = append(parts, fmt.Sprintf("Interests: %s", p.Interests))
	}
	if p.Timezone != "" {
		parts = append(parts, fmt.Sprintf("Timezone: %s", p.Timezone))
	}
	if p.Communication != "" {
		parts = append(parts, fmt.Sprintf("Communication style: %s", p.Communication))
	}
	if p.Instructions != "" {
		parts = append(parts, fmt.Sprintf("Custom instructions: %s", p.Instructions))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// IsEmpty returns true if no profile fields are set.
func (p *UserProfile) IsEmpty() bool {
	return p.DisplayName == "" && p.Profession == "" && p.Organization == "" &&
		p.Goals == "" && p.Interests == "" && p.Timezone == "" &&
		p.Communication == "" && p.Instructions == ""
}
