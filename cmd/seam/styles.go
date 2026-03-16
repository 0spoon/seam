package main

import "github.com/charmbracelet/lipgloss"

// Color palette for the dark theme, aligned with FE_DESIGN.md.
// Primary accent: amber/copper. Text: warm off-white. Background: dark.
var (
	colorPrimary   = lipgloss.Color("#c4915c") // amber/copper accent
	colorSecondary = lipgloss.Color("#a68a6e") // muted amber
	colorMuted     = lipgloss.Color("#9992a6") // muted lavender
	colorFg        = lipgloss.Color("#e8e2d9") // warm off-white
	colorBg        = lipgloss.Color("#1a1816") // dark background
	colorHeaderBg  = lipgloss.Color("#242120") // slightly lighter bg
	colorSelected  = lipgloss.Color("#3a3330") // warm dark selection
	colorError     = lipgloss.Color("#c46b6b") // muted red
	colorSuccess   = lipgloss.Color("#6b9b7a") // sage green (aligned with web --status-success)
	colorBorder    = lipgloss.Color("#3a3330") // warm border
	colorDim       = lipgloss.Color("#5e5a6e") // dim text for less emphasis
)

// Shared styles used across screens.
var (
	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorFg).
			Background(colorHeaderBg).
			Padding(0, 1)

	styleStatusBar = lipgloss.NewStyle().
			Foreground(colorMuted).
			Background(colorHeaderBg).
			Padding(0, 1)

	styleError = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true)

	styleSuccess = lipgloss.NewStyle().
			Foreground(colorSuccess)

	styleTitle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleMuted = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleSelected = lipgloss.NewStyle().
			Background(colorSelected).
			Foreground(colorFg).
			Padding(0, 1)

	styleNormal = lipgloss.NewStyle().
			Foreground(colorFg).
			Padding(0, 1)

	styleBorder = lipgloss.NewStyle().
			BorderForeground(colorBorder)

	stylePane = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder)

	stylePaneActive = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary)
)
