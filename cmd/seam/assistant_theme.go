package main

import "charm.land/lipgloss/v2"

// Mario palette -- used ONLY by the assistant screen so the rest of the
// TUI keeps the warm amber theme defined in styles.go.
var (
	marioRed       = lipgloss.Color("#E52521")
	marioPipeGreen = lipgloss.Color("#43B047")
	marioCoinGold  = lipgloss.Color("#FBD000")
	marioSky       = lipgloss.Color("#5C94FC")
	marioBrickBrn  = lipgloss.Color("#7B3F00")
	marioWhite     = lipgloss.Color("#FCE5C8")
	marioMutedFg   = lipgloss.Color("#8B7355")
)

// marioPipeBorder draws a thick double-line frame reminiscent of a green
// warp pipe. We use it for the assistant pane and the confirmation prompt.
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

var (
	marioHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(marioWhite).
				Background(marioRed).
				Padding(0, 2)

	marioConfirmPaneStyle = lipgloss.NewStyle().
				Border(marioPipeBorder).
				BorderForeground(marioCoinGold)

	marioToolBlockStyle = lipgloss.NewStyle().
				Foreground(marioCoinGold).
				Bold(true)

	marioMessageUserStyle = lipgloss.NewStyle().
				Foreground(marioSky).
				Bold(true)

	marioMessageAssistStyle = lipgloss.NewStyle().
				Foreground(marioWhite)

	marioToolStatusOk  = lipgloss.NewStyle().Foreground(marioPipeGreen).Bold(true)
	marioToolStatusErr = lipgloss.NewStyle().Foreground(marioRed).Bold(true)
	marioToolStatusRun = lipgloss.NewStyle().Foreground(marioCoinGold).Bold(true)

	marioMutedStyle = lipgloss.NewStyle().Foreground(marioMutedFg)
	marioErrorStyle = lipgloss.NewStyle().Foreground(marioRed).Bold(true)
)

// marioBlock is the gold question-block glyph used to prefix tool cards.
const marioBlock = "▣"

// marioStatusGlyph returns the unicode dot for a tool's status.
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

// marioStatusStyle returns the style paired with a status glyph.
func marioStatusStyle(status string) lipgloss.Style {
	switch status {
	case "ok":
		return marioToolStatusOk
	case "error":
		return marioToolStatusErr
	default:
		return marioToolStatusRun
	}
}
