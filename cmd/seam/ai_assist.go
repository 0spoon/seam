package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

	case tea.KeyMsg:
		m.err = ""

		// If we have a result, handle insert/dismiss/scrolling.
		if m.result != "" {
			switch msg.String() {
			case "enter", "y":
				m.applied = true
				m.done = true
				return m, nil
			case "esc", "n":
				m.done = true
				return m, nil
			case "j", "down":
				maxScroll := m.resultMaxScroll()
				if m.resultScroll < maxScroll {
					m.resultScroll++
				}
				return m, nil
			case "k", "up":
				if m.resultScroll > 0 {
					m.resultScroll--
				}
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "esc":
			m.done = true
			return m, nil

		case "j", "down":
			if m.cursor < len(aiAssistActions)-1 {
				m.cursor++
			}
			return m, nil

		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil

		case "enter":
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

	b.WriteString(styleTitle.Render("AI Writing Assist"))
	b.WriteString("\n\n")

	if m.result != "" {
		// Show scrollable result preview.
		b.WriteString(styleMuted.Render("Result:"))
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
		b.WriteString(styleNormal.Render(strings.Join(visible, "\n")))
		if end < len(lines) {
			b.WriteString("\n" + styleMuted.Render(fmt.Sprintf("... (+%d more lines, j/k to scroll)", len(lines)-end)))
		}
		b.WriteString("\n\n")

		help := styleMuted.Render("Enter/y: insert | Esc/n: dismiss | j/k: scroll")
		b.WriteString(help)
	} else if m.loading {
		action := aiAssistActions[m.cursor].label
		b.WriteString(styleMuted.Render(fmt.Sprintf("Running %s...", action)))
	} else {
		if m.selection != "" {
			preview := m.selection
			if runes := []rune(preview); len(runes) > 80 {
				preview = string(runes[:77]) + "..."
			}
			b.WriteString(styleMuted.Render(fmt.Sprintf("Selection: %q", preview)))
			b.WriteString("\n\n")
		} else {
			b.WriteString(styleMuted.Render("No selection - will use full note"))
			b.WriteString("\n\n")
		}

		for i, a := range aiAssistActions {
			label := fmt.Sprintf("  %s", a.label)
			desc := styleMuted.Render(" - " + a.description)

			if i == m.cursor {
				b.WriteString(styleSelected.Render(fmt.Sprintf("> %s", a.label)))
				b.WriteString(desc)
			} else {
				b.WriteString(styleNormal.Render(label))
				b.WriteString(desc)
			}
			b.WriteString("\n")
		}

		b.WriteString("\n")
		help := styleMuted.Render("j/k: navigate | Enter: run | Esc: cancel")
		b.WriteString(help)
	}

	if m.err != "" {
		b.WriteString("\n\n")
		b.WriteString(styleError.Render(m.err))
	}

	content := b.String()
	formWidth := 64
	box := lipgloss.NewStyle().
		Width(formWidth).
		Padding(2, 4).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorSecondary)

	rendered := box.Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, rendered)
}
