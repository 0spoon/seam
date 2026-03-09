package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// captureField identifies which field is focused in the capture modal.
type captureField int

const (
	capFieldTitle captureField = iota
	capFieldBody
)

// captureModel handles the quick capture modal.
type captureModel struct {
	titleInput textinput.Model
	bodyInput  textarea.Model
	focused    captureField
	client     *APIClient
	projectID  string
	err        string
	loading    bool
	done       bool
	created    bool
	width      int
	height     int
}

// noteCreatedMsg is sent when a note is successfully created.
type noteCreatedMsg struct {
	note *Note
}

func newCaptureModel(client *APIClient, projectID string, width, height int) captureModel {
	ti := textinput.New()
	ti.Placeholder = "Note title"
	ti.CharLimit = 256
	ti.Width = 50
	ti.Focus()

	ta := textarea.New()
	ta.Placeholder = "Note body (markdown)"
	ta.SetWidth(50)
	ta.SetHeight(8)
	ta.CharLimit = 0

	return captureModel{
		titleInput: ti,
		bodyInput:  ta,
		client:     client,
		projectID:  projectID,
		width:      width,
		height:     height,
	}
}

func (m captureModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m captureModel) Update(msg tea.Msg) (captureModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case noteCreatedMsg:
		m.loading = false
		m.done = true
		m.created = true
		return m, nil

	case apiErrorMsg:
		m.loading = false
		m.err = msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		m.err = ""
		switch msg.String() {
		case "esc":
			m.done = true
			return m, nil

		case "tab":
			if m.focused == capFieldTitle {
				m.focused = capFieldBody
				m.titleInput.Blur()
				m.bodyInput.Focus()
			} else {
				m.focused = capFieldTitle
				m.bodyInput.Blur()
				return m, m.titleInput.Focus()
			}
			return m, nil

		case "ctrl+s":
			if m.loading {
				return m, nil
			}
			title := strings.TrimSpace(m.titleInput.Value())
			if title == "" {
				m.err = "title is required"
				return m, nil
			}
			body := m.bodyInput.Value()
			m.loading = true
			client := m.client
			projectID := m.projectID
			return m, func() tea.Msg {
				note, err := client.CreateNote(title, body, projectID)
				if err != nil {
					return apiErrorMsg{err: err}
				}
				return noteCreatedMsg{note: note}
			}
		}
	}

	// Update the focused input.
	var cmd tea.Cmd
	if m.focused == capFieldTitle {
		m.titleInput, cmd = m.titleInput.Update(msg)
	} else {
		m.bodyInput, cmd = m.bodyInput.Update(msg)
	}
	return m, cmd
}

func (m captureModel) View() string {
	if m.width == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString(styleTitle.Render("New Note"))
	b.WriteString("\n\n")

	labelStyle := lipgloss.NewStyle().
		Foreground(colorMuted).
		Width(8).
		Align(lipgloss.Right)

	b.WriteString(labelStyle.Render("Title: "))
	b.WriteString(m.titleInput.View())
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("Body: "))
	b.WriteString("\n")
	b.WriteString(m.bodyInput.View())
	b.WriteString("\n\n")

	if m.err != "" {
		b.WriteString(styleError.Render(m.err))
		b.WriteString("\n\n")
	}

	if m.loading {
		b.WriteString(styleMuted.Render("Saving..."))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	help := styleMuted.Render("Ctrl+S: save | Tab: switch field | Esc: cancel")
	b.WriteString(help)

	content := b.String()
	formWidth := 64
	box := lipgloss.NewStyle().
		Width(formWidth).
		Padding(2, 4).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary)

	rendered := box.Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, rendered)
}
