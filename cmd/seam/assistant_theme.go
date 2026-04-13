package main

import "charm.land/lipgloss/v2"

// marioPipeBorder uses thin single-line box-drawing chars for a clean
// retro terminal look. The thinner lines let the accent colors and
// content breathe instead of competing with a heavy frame.
var marioPipeBorder = lipgloss.Border{
	Top:         "─",
	Bottom:      "─",
	Left:        "│",
	Right:       "│",
	TopLeft:     "┌",
	TopRight:    "┐",
	BottomLeft:  "└",
	BottomRight: "┘",
}

// marioBlock is the gold question-block glyph used to prefix tool cards
// in the assistant screen when the Mario theme is active. The
// assistantStyleSet exposes this via its Block field; ApplyAssistantTheme
// switches between this and a smaller dot when the assistant follows the
// global Catppuccin theme.
const marioBlock = "▣"

// marioStatusGlyph returns the unicode glyph for a tool's status. The
// glyph is intentionally theme-independent so success/error/running stay
// recognizable across the Mario and Catppuccin assistant looks. It is
// rendered inside the tool bubble banner, which carries the semantic
// color via its inverse fg/bg.
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

