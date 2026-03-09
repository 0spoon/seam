package main

import "github.com/charmbracelet/lipgloss"

// Color palette for the dark theme.
var (
	colorPrimary   = lipgloss.Color("#7aa2f7")
	colorSecondary = lipgloss.Color("#bb9af7")
	colorMuted     = lipgloss.Color("#565f89")
	colorFg        = lipgloss.Color("#c0caf5")
	colorBg        = lipgloss.Color("#1a1b26")
	colorHeaderBg  = lipgloss.Color("#24283b")
	colorSelected  = lipgloss.Color("#3d59a1")
	colorError     = lipgloss.Color("#f7768e")
	colorSuccess   = lipgloss.Color("#9ece6a")
	colorBorder    = lipgloss.Color("#3b4261")
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
