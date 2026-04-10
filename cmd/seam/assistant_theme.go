package main

import "charm.land/lipgloss/v2"

// marioPipeBorder draws a thick double-line frame reminiscent of a green
// warp pipe. Used by themeMario as its BorderShape so the assistant
// confirmation pane keeps its signature look. The shape is decorative
// (not a color), so it lives here even though all color vars have moved
// into the Theme registry.
var marioPipeBorder = lipgloss.Border{
	Top:         "═",
	Bottom:      "═",
	Left:        "║",
	Right:       "║",
	TopLeft:     "╔",
	TopRight:    "╗",
	BottomLeft:  "╚",
	BottomRight: "╝",
}

// marioBlock is the gold question-block glyph used to prefix tool cards
// in the assistant screen when the Mario theme is active. The
// assistantStyleSet exposes this via its Block field; ApplyAssistantTheme
// switches between this and a smaller dot when the assistant follows the
// global Catppuccin theme.
const marioBlock = "▣"

// marioStatusGlyph returns the unicode glyph for a tool's status. The
// glyph is intentionally theme-independent so success/error/running stay
// recognizable across the Mario and Catppuccin assistant looks.
func marioStatusGlyph(status string) string {
	switch status {
	case "ok":
		return "✔"
	case "error":
		return "✖"
	default:
		return "●"
	}
}

// assistantStatusStyle returns the style paired with a status glyph,
// reading from the active assistantStyleSet so it tracks the current
// theme.
func assistantStatusStyle(status string) lipgloss.Style {
	switch status {
	case "ok":
		return assistantStyles.ToolStatusOk
	case "error":
		return assistantStyles.ToolStatusErr
	default:
		return assistantStyles.ToolStatusRun
	}
}
