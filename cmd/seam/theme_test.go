package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestThemeRegistry asserts every registered theme has a name and a
// non-nil entry in every semantic slot. Catches palette typos that
// would otherwise render as transparent or default-colored text.
func TestThemeRegistry(t *testing.T) {
	require.NotEmpty(t, themeRegistry, "registry should not be empty")
	for slug, th := range themeRegistry {
		t.Run(slug, func(t *testing.T) {
			require.Equal(t, slug, th.Name, "theme name should match its registry key")
			require.NotNil(t, th.Primary, "Primary")
			require.NotNil(t, th.Secondary, "Secondary")
			require.NotNil(t, th.Muted, "Muted")
			require.NotNil(t, th.Fg, "Fg")
			require.NotNil(t, th.HeaderBg, "HeaderBg")
			require.NotNil(t, th.StatusBarBg, "StatusBarBg")
			require.NotNil(t, th.Selected, "Selected")
			require.NotNil(t, th.Border, "Border")
			require.NotNil(t, th.Error, "Error")
			require.NotNil(t, th.Success, "Success")
			require.NotNil(t, th.Dim, "Dim")
		})
	}
}

// TestBuildStyleSet verifies buildStyleSet and buildAssistantStyleSet
// are total over every registered theme (don't panic on any).
func TestBuildStyleSet(t *testing.T) {
	for slug, th := range themeRegistry {
		t.Run(slug, func(t *testing.T) {
			require.NotPanics(t, func() {
				ss := buildStyleSet(th)
				require.NotNil(t, ss)
			})
			require.NotPanics(t, func() {
				as := buildAssistantStyleSet(th)
				require.NotNil(t, as)
				require.NotEmpty(t, as.Block, "Block glyph should never be empty")
			})
		})
	}
}

// TestApplyTheme exercises the global theme switch and asserts the
// active theme + style set update in lockstep.
func TestApplyTheme(t *testing.T) {
	// Snapshot for cleanup so tests don't pollute each other.
	originalTheme := activeTheme.Name
	originalAsst := currentAssistantMode()
	t.Cleanup(func() {
		_ = ApplyTheme(originalTheme)
		_ = ApplyAssistantTheme(originalAsst)
	})

	require.NoError(t, ApplyTheme("catppuccin-mocha"))
	require.Equal(t, "catppuccin-mocha", activeTheme.Name)
	mochaPrimary := activeTheme.Primary
	require.NotNil(t, styles)

	require.NoError(t, ApplyTheme("catppuccin-latte"))
	require.Equal(t, "catppuccin-latte", activeTheme.Name)
	require.NotEqual(t, mochaPrimary, activeTheme.Primary, "primary should change between Mocha and Latte")
}

// TestApplyTheme_UnknownReturnsError ensures bad input is rejected
// without mutating state.
func TestApplyTheme_UnknownReturnsError(t *testing.T) {
	originalTheme := activeTheme.Name
	t.Cleanup(func() {
		_ = ApplyTheme(originalTheme)
	})
	require.NoError(t, ApplyTheme("catppuccin-mocha"))
	err := ApplyTheme("not-a-real-theme")
	require.Error(t, err)
	require.Equal(t, "catppuccin-mocha", activeTheme.Name, "active theme should be unchanged on error")
}

// TestApplyAssistantTheme covers both modes and confirms that
// follow_global tracks the current global theme.
func TestApplyAssistantTheme(t *testing.T) {
	originalTheme := activeTheme.Name
	originalAsst := currentAssistantMode()
	t.Cleanup(func() {
		_ = ApplyTheme(originalTheme)
		_ = ApplyAssistantTheme(originalAsst)
	})

	require.NoError(t, ApplyTheme("catppuccin-mocha"))
	require.NoError(t, ApplyAssistantTheme("mario"))
	require.Equal(t, "mario", activeAssistantTheme.Name)

	require.NoError(t, ApplyAssistantTheme("follow_global"))
	require.Equal(t, "catppuccin-mocha", activeAssistantTheme.Name,
		"follow_global should mirror the active global theme")

	// And switching the global theme while following should propagate.
	require.NoError(t, ApplyTheme("catppuccin-latte"))
	require.Equal(t, "catppuccin-latte", activeAssistantTheme.Name)
}

// TestListThemes asserts the picker list excludes the assistant-only
// Mario theme but includes every Catppuccin variant and Seam.
func TestListThemes(t *testing.T) {
	got := ListThemes()
	require.NotContains(t, got, "mario", "Mario must not appear in the global picker")
	require.Contains(t, got, "catppuccin-mocha")
	require.Contains(t, got, "catppuccin-macchiato")
	require.Contains(t, got, "catppuccin-frappe")
	require.Contains(t, got, "catppuccin-latte")
	require.Contains(t, got, "seam")
	// Sorted alphabetically for stable picker rendering.
	for i := 1; i < len(got); i++ {
		require.LessOrEqual(t, got[i-1], got[i], "ListThemes should return sorted slugs")
	}
}

// TestResolveTheme_TrimAndCase verifies the resolver tolerates whitespace
// and uppercase, since users may type "  Catppuccin-Mocha  " in a config
// file or env var.
func TestResolveTheme_TrimAndCase(t *testing.T) {
	th, ok := ResolveTheme("  CATPPUCCIN-MOCHA  ")
	require.True(t, ok)
	require.Equal(t, "catppuccin-mocha", th.Name)

	_, ok = ResolveTheme("nope")
	require.False(t, ok)
}
