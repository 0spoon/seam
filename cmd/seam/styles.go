package main

import "charm.land/lipgloss/v2"

// styleSet holds all shared styles used across the main TUI screens. It
// is rebuilt by buildStyleSet whenever the active theme changes; call
// sites read from the package-level `styles` pointer at View() time.
type styleSet struct {
	Header     lipgloss.Style
	StatusBar  lipgloss.Style
	Error      lipgloss.Style
	Success    lipgloss.Style
	Title      lipgloss.Style
	Muted      lipgloss.Style
	Selected   lipgloss.Style
	Normal     lipgloss.Style
	Pane       lipgloss.Style
	PaneActive lipgloss.Style
}

// assistantStyleSet holds styles used only by the assistant screen
// (cmd/seam/ask.go). It is built from activeAssistantTheme, which may
// be themeMario or a copy of the active global theme depending on the
// `assistant_theme` config value.
type assistantStyleSet struct {
	Header        lipgloss.Style
	ConfirmPane   lipgloss.Style
	ToolBlock     lipgloss.Style
	MessageUser   lipgloss.Style
	MessageAssist lipgloss.Style
	ToolStatusOk  lipgloss.Style
	ToolStatusErr lipgloss.Style
	ToolStatusRun lipgloss.Style
	Muted         lipgloss.Style
	Error         lipgloss.Style
	StatusBar     lipgloss.Style
	// Block is the accent glyph used to prefix tool cards. Mario uses
	// the question-block; non-Mario themes use a smaller dot.
	Block string
}

// styles is the active main-screen style set. Replaced by ApplyTheme.
var styles *styleSet

// assistantStyles is the active assistant-screen style set. Replaced by
// ApplyTheme (when the assistant follows the global theme) or by
// ApplyAssistantTheme.
var assistantStyles *assistantStyleSet

// buildStyleSet constructs the main style set from a Theme. Lipgloss v2
// styles are values that bake colors at construction, so we rebuild the
// whole set on every theme change rather than mutating individual fields.
func buildStyleSet(t Theme) *styleSet {
	return &styleSet{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Fg).
			Background(t.HeaderBg).
			Padding(0, 1),

		StatusBar: lipgloss.NewStyle().
			Foreground(t.Muted).
			Background(t.StatusBarBg).
			Padding(0, 1),

		Error: lipgloss.NewStyle().
			Foreground(t.Error).
			Bold(true),

		Success: lipgloss.NewStyle().
			Foreground(t.Success),

		Title: lipgloss.NewStyle().
			Foreground(t.Primary).
			Bold(true),

		Muted: lipgloss.NewStyle().
			Foreground(t.Muted),

		Selected: lipgloss.NewStyle().
			Background(t.Selected).
			Foreground(t.Fg).
			Padding(0, 1),

		Normal: lipgloss.NewStyle().
			Foreground(t.Fg).
			Padding(0, 1),

		Pane: lipgloss.NewStyle().
			Border(t.BorderShape).
			BorderForeground(t.Border),

		PaneActive: lipgloss.NewStyle().
			Border(t.BorderShape).
			BorderForeground(t.Primary),
	}
}

// buildAssistantStyleSet constructs the assistant-screen style set from a
// Theme. The same builder works for Mario and Catppuccin because the
// Theme abstracts both palettes behind the same semantic slots.
func buildAssistantStyleSet(t Theme) *assistantStyleSet {
	block := t.AccentBlock
	if block == "" {
		block = "•"
	}
	return &assistantStyleSet{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Fg).
			Background(t.HeaderBg).
			Padding(0, 2),

		ConfirmPane: lipgloss.NewStyle().
			Border(t.BorderShape).
			BorderForeground(t.Primary),

		ToolBlock: lipgloss.NewStyle().
			Foreground(t.Primary).
			Bold(true),

		MessageUser: lipgloss.NewStyle().
			Foreground(t.Secondary).
			Bold(true),

		MessageAssist: lipgloss.NewStyle().
			Foreground(t.Fg),

		ToolStatusOk: lipgloss.NewStyle().
			Foreground(t.Success).
			Bold(true),

		ToolStatusErr: lipgloss.NewStyle().
			Foreground(t.Error).
			Bold(true),

		ToolStatusRun: lipgloss.NewStyle().
			Foreground(t.Primary).
			Bold(true),

		Muted: lipgloss.NewStyle().
			Foreground(t.Muted),

		Error: lipgloss.NewStyle().
			Foreground(t.Error).
			Bold(true),

		StatusBar: lipgloss.NewStyle().
			Foreground(t.Fg).
			Background(t.StatusBarBg).
			Padding(0, 1),

		Block: block,
	}
}

func init() {
	styles = buildStyleSet(activeTheme)
	assistantStyles = buildAssistantStyleSet(activeAssistantTheme)
}
