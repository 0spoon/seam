package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// openSettingsMsg opens the settings screen from any other screen.
type openSettingsMsg struct{}

// settingsCategory identifies which list the cursor is currently in.
type settingsCategory int

const (
	catTheme settingsCategory = iota
	catAssistant
)

// settingsModel renders a small settings/picker overlay-style screen.
// The screen is reachable via the comma key from the main screen and
// supports live preview: highlighting a theme calls ApplyTheme so the
// underlying chrome re-renders immediately, and Esc reverts to whatever
// was active when the screen opened.
type settingsModel struct {
	width, height int

	// themes is the list of selectable global theme slugs (excluding
	// the assistant-only Mario theme).
	themes []string
	// assistantOptions is the list of assistant-theme modes.
	assistantOptions []string

	category    settingsCategory
	themeIdx    int
	assistIdx   int
	originTheme string
	originAsst  string
	status      string
	saved       bool
}

func newSettingsModel(width, height int) settingsModel {
	themes := ListThemes()
	assistantOptions := []string{themeMario.Name, AssistantInheritName}

	m := settingsModel{
		width:            width,
		height:           height,
		themes:           themes,
		assistantOptions: assistantOptions,
		originTheme:      activeTheme.Name,
		originAsst:       currentAssistantMode(),
	}
	// Position cursors on the active values.
	for i, name := range themes {
		if name == activeTheme.Name {
			m.themeIdx = i
			break
		}
	}
	for i, name := range assistantOptions {
		if name == m.originAsst {
			m.assistIdx = i
			break
		}
	}
	return m
}

// currentAssistantMode returns the canonical mode string for the
// currently active assistant theme.
func currentAssistantMode() string {
	if activeAssistantTheme.Name == themeMario.Name {
		return themeMario.Name
	}
	return AssistantInheritName
}

func (m settingsModel) Init() tea.Cmd {
	return nil
}

func (m settingsModel) Update(msg tea.Msg) (settingsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		km := currentKeymap()
		switch {
		case km.Matches(msg, ActionSettingsCancel):
			// Revert preview to whatever was active on entry.
			_ = ApplyTheme(m.originTheme)
			_ = ApplyAssistantTheme(m.originAsst)
			m.saved = true
			return m, nil

		case km.Matches(msg, ActionSettingsSave):
			// Load the existing config first so we do not clobber user
			// keybindings when saving the theme/assistant choice.
			existing, _ := LoadTUIConfig()
			existing.Theme = m.themes[m.themeIdx]
			existing.AssistantTheme = m.assistantOptions[m.assistIdx]
			if err := SaveTUIConfig(existing); err != nil {
				m.status = "Save failed: " + err.Error()
				return m, nil
			}
			m.saved = true
			return m, nil

		case km.Matches(msg, ActionSettingsSwitchCategory):
			if m.category == catTheme {
				m.category = catAssistant
			} else {
				m.category = catTheme
			}
			return m, nil

		case km.Matches(msg, ActionSettingsNavDown):
			return m.cursorMove(+1), nil

		case km.Matches(msg, ActionSettingsNavUp):
			return m.cursorMove(-1), nil
		}
	}
	return m, nil
}

// cursorMove advances the cursor in the active category and immediately
// applies the highlighted choice for live preview.
func (m settingsModel) cursorMove(step int) settingsModel {
	switch m.category {
	case catTheme:
		next := m.themeIdx + step
		if next < 0 {
			next = 0
		}
		if next >= len(m.themes) {
			next = len(m.themes) - 1
		}
		m.themeIdx = next
		// Live preview.
		if err := ApplyTheme(m.themes[m.themeIdx]); err == nil {
			m.status = "Preview: " + m.themes[m.themeIdx]
		}
	case catAssistant:
		next := m.assistIdx + step
		if next < 0 {
			next = 0
		}
		if next >= len(m.assistantOptions) {
			next = len(m.assistantOptions) - 1
		}
		m.assistIdx = next
		if err := ApplyAssistantTheme(m.assistantOptions[m.assistIdx]); err == nil {
			m.status = "Preview: assistant " + m.assistantOptions[m.assistIdx]
		}
	}
	return m
}

func (m settingsModel) View() string {
	if m.width == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(styles.Title.Render("Settings"))
	b.WriteString("\n\n")

	// Theme list.
	themeHeader := "Theme"
	if m.category == catTheme {
		themeHeader = "> " + themeHeader
	} else {
		themeHeader = "  " + themeHeader
	}
	b.WriteString(styles.Title.Render(themeHeader))
	b.WriteString("\n")
	for i, name := range m.themes {
		marker := "  "
		render := styles.Normal.Render
		if i == m.themeIdx {
			marker = "> "
			render = styles.Selected.Render
		}
		b.WriteString(render(marker + name))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Assistant theme list.
	asstHeader := "Assistant theme"
	if m.category == catAssistant {
		asstHeader = "> " + asstHeader
	} else {
		asstHeader = "  " + asstHeader
	}
	b.WriteString(styles.Title.Render(asstHeader))
	b.WriteString("\n")
	for i, name := range m.assistantOptions {
		marker := "  "
		render := styles.Normal.Render
		if i == m.assistIdx {
			marker = "> "
			render = styles.Selected.Render
		}
		b.WriteString(render(marker + name))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.status != "" {
		b.WriteString(styles.Muted.Render(m.status))
		b.WriteString("\n\n")
	}
	km := currentKeymap()
	help := styles.Muted.Render(fmt.Sprintf("%s/%s: navigate | %s: switch list | %s: save | %s: cancel",
		km.Display(ActionSettingsNavDown),
		km.Display(ActionSettingsNavUp),
		km.Display(ActionSettingsSwitchCategory),
		km.Display(ActionSettingsSave),
		km.Display(ActionSettingsCancel)))
	b.WriteString(help)

	content := b.String()
	formWidth := 60
	box := lipgloss.NewStyle().
		Width(formWidth).
		Padding(2, 4).
		Border(activeTheme.BorderShape).
		BorderForeground(activeTheme.Primary)

	rendered := box.Render(content)
	// Center the form in the terminal so the underlying chrome shows
	// through the live preview as the user navigates.
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, rendered)
}
