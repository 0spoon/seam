package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// aiAssistAction represents an available AI assist action.
type aiAssistAction struct {
	name        string
	label       string
	description string
}

var aiAssistActions = []aiAssistAction{
	{name: "expand", label: ":expand", description: "Expand selected text into paragraphs"},
	{name: "summarize", label: ":summarize", description: "Summarize the note or selection"},
	{name: "extract-actions", label: ":actions", description: "Extract action items as a checklist"},
}

// aiAssistModel is the command palette overlay for AI writing assist.
type aiAssistModel struct {
	client       *APIClient
	noteID       string
	selection    string
	cursor       int
	err          string
	loading      bool
	done         bool
	result       string
	resultScroll int
	applied      bool
	width        int
	height       int
}

// aiAssistResultMsg is sent when AI assist returns a result.
type aiAssistResultMsg struct {
	result string
}

func newAIAssistModel(client *APIClient, noteID, selection string, width, height int) aiAssistModel {
	return aiAssistModel{
		client:    client,
		noteID:    noteID,
		selection: selection,
		width:     width,
		height:    height,
	}
}

func (m aiAssistModel) Init() tea.Cmd {
	return nil
}

func (m aiAssistModel) Update(msg tea.Msg) (aiAssistModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case aiAssistResultMsg:
		m.loading = false
		m.result = msg.result
		return m, nil

	case apiErrorMsg:
		m.loading = false
		m.err = msg.err.Error()
		return m, nil

	case tea.KeyPressMsg:
		m.err = ""
		km := currentKeymap()

		// If we have a result, handle insert / dismiss / scrolling.
		// ai_assist.primary and ai_assist.insert_alt both insert the
		// result; ai_assist.cancel and ai_assist.dismiss_alt both close it.
		if m.result != "" {
			switch {
			case km.Matches(msg, ActionAIAssistPrimary, ActionAIAssistInsertAlt):
				m.applied = true
				m.done = true
				return m, nil
			case km.Matches(msg, ActionAIAssistCancel, ActionAIAssistDismissAlt):
				m.done = true
				return m, nil
			case km.Matches(msg, ActionAIAssistNavDown):
				maxScroll := m.resultMaxScroll()
				if m.resultScroll < maxScroll {
					m.resultScroll++
				}
				return m, nil
			case km.Matches(msg, ActionAIAssistNavUp):
				if m.resultScroll > 0 {
					m.resultScroll--
				}
				return m, nil
			}
			return m, nil
		}

		switch {
		case km.Matches(msg, ActionAIAssistCancel):
			m.done = true
			return m, nil

		case km.Matches(msg, ActionAIAssistNavDown):
			if m.cursor < len(aiAssistActions)-1 {
				m.cursor++
			}
			return m, nil

		case km.Matches(msg, ActionAIAssistNavUp):
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil

		case km.Matches(msg, ActionAIAssistPrimary):
			if m.loading {
				return m, nil
			}
			m.loading = true
			action := aiAssistActions[m.cursor].name
			client := m.client
			noteID := m.noteID
			selection := m.selection
			return m, func() tea.Msg {
				result, err := client.Assist(noteID, action, selection)
				if err != nil {
					return apiErrorMsg{err: err}
				}
				return aiAssistResultMsg{result: result.Result}
			}
		}
	}

	return m, nil
}

// resultMaxScroll returns the maximum scroll offset for the result view.
func (m aiAssistModel) resultMaxScroll() int {
	if m.result == "" {
		return 0
	}
	lines := strings.Split(m.result, "\n")
	// Allow roughly half the screen for the result.
	viewportHeight := m.height/2 - 4
	if viewportHeight < 5 {
		viewportHeight = 5
	}
	max := len(lines) - viewportHeight
	if max < 0 {
		max = 0
	}
	return max
}

func (m aiAssistModel) View() string {
	if m.width == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString(styles.Title.Render("AI Writing Assist"))
	b.WriteString("\n\n")

	if m.result != "" {
		// Show scrollable result preview.
		b.WriteString(styles.Muted.Render("Result:"))
		b.WriteString("\n\n")

		lines := strings.Split(m.result, "\n")
		viewportHeight := m.height/2 - 4
		if viewportHeight < 5 {
			viewportHeight = 5
		}
		start := m.resultScroll
		if start > len(lines) {
			start = len(lines)
		}
		end := start + viewportHeight
		if end > len(lines) {
			end = len(lines)
		}
		visible := lines[start:end]
		b.WriteString(styles.Normal.Render(strings.Join(visible, "\n")))
		km := currentKeymap()
		if end < len(lines) {
			b.WriteString("\n" + styles.Muted.Render(fmt.Sprintf("... (+%d more lines, %s/%s to scroll)",
				len(lines)-end,
				km.Display(ActionAIAssistNavDown),
				km.Display(ActionAIAssistNavUp))))
		}
		b.WriteString("\n\n")

		help := styles.Muted.Render(fmt.Sprintf("%s/%s: insert | %s/%s: dismiss | %s/%s: scroll",
			km.Display(ActionAIAssistPrimary),
			km.Display(ActionAIAssistInsertAlt),
			km.Display(ActionAIAssistCancel),
			km.Display(ActionAIAssistDismissAlt),
			km.Display(ActionAIAssistNavDown),
			km.Display(ActionAIAssistNavUp)))
		b.WriteString(help)
	} else if m.loading {
		action := aiAssistActions[m.cursor].label
		b.WriteString(styles.Muted.Render(fmt.Sprintf("Running %s...", action)))
	} else {
		if m.selection != "" {
			preview := m.selection
			if runes := []rune(preview); len(runes) > 80 {
				preview = string(runes[:77]) + "..."
			}
			b.WriteString(styles.Muted.Render(fmt.Sprintf("Selection: %q", preview)))
			b.WriteString("\n\n")
		} else {
			b.WriteString(styles.Muted.Render("No selection - will use full note"))
			b.WriteString("\n\n")
		}

		for i, a := range aiAssistActions {
			label := fmt.Sprintf("  %s", a.label)
			desc := styles.Muted.Render(" - " + a.description)

			if i == m.cursor {
				b.WriteString(styles.Selected.Render(fmt.Sprintf("> %s", a.label)))
				b.WriteString(desc)
			} else {
				b.WriteString(styles.Normal.Render(label))
				b.WriteString(desc)
			}
			b.WriteString("\n")
		}

		b.WriteString("\n")
		km := currentKeymap()
		help := styles.Muted.Render(fmt.Sprintf("%s/%s: navigate | %s: run | %s: cancel",
			km.Display(ActionAIAssistNavDown),
			km.Display(ActionAIAssistNavUp),
			km.Display(ActionAIAssistPrimary),
			km.Display(ActionAIAssistCancel)))
		b.WriteString(help)
	}

	if m.err != "" {
		b.WriteString("\n\n")
		b.WriteString(styles.Error.Render(m.err))
	}

	content := b.String()
	formWidth := 64
	box := lipgloss.NewStyle().
		Width(formWidth).
		Padding(2, 4).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(activeTheme.Secondary)

	rendered := box.Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, rendered)
}
