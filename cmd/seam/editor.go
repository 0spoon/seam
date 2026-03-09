package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// editorModel handles the full-screen note editor.
type editorModel struct {
	client        *APIClient
	noteID        string
	title         string
	body          textarea.Model
	err           string
	status        string
	loading       bool
	saving        bool
	done          bool
	modified      bool
	width         int
	height        int
	showAIAssist  bool
	aiAssistModel aiAssistModel
}

// openEditorMsg triggers opening a note in the editor.
type openEditorMsg struct {
	noteID string
}

// noteLoadedMsg is sent when a note is loaded for editing.
type noteLoadedMsg struct {
	note *Note
}

// noteSavedMsg is sent when a note is saved.
type noteSavedMsg struct{}

func newEditorModel(client *APIClient, noteID string, width, height int) editorModel {
	ta := textarea.New()
	ta.Placeholder = "Start writing..."
	ta.CharLimit = 0
	ta.ShowLineNumbers = false

	// Reserve space for header and status bar.
	ta.SetWidth(width - 2)
	editorHeight := height - 4
	if editorHeight < 5 {
		editorHeight = 5
	}
	ta.SetHeight(editorHeight)
	ta.Focus()

	return editorModel{
		client:  client,
		noteID:  noteID,
		body:    ta,
		loading: true,
		width:   width,
		height:  height,
	}
}

func (m editorModel) Init() tea.Cmd {
	client := m.client
	noteID := m.noteID
	return func() tea.Msg {
		note, err := client.GetNote(noteID)
		if err != nil {
			return apiErrorMsg{err: err}
		}
		return noteLoadedMsg{note: note}
	}
}

func (m editorModel) Update(msg tea.Msg) (editorModel, tea.Cmd) {
	// If AI assist overlay is open, delegate to it.
	if m.showAIAssist {
		return m.updateAIAssist(msg)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.body.SetWidth(m.width - 2)
		editorHeight := m.height - 4
		if editorHeight < 5 {
			editorHeight = 5
		}
		m.body.SetHeight(editorHeight)
		return m, nil

	case noteLoadedMsg:
		m.loading = false
		m.title = msg.note.Title
		m.body.SetValue(msg.note.Body)
		m.modified = false
		return m, nil

	case noteSavedMsg:
		m.saving = false
		m.modified = false
		m.status = "Saved"
		return m, nil

	case apiErrorMsg:
		m.loading = false
		m.saving = false
		m.err = msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		m.err = ""
		m.status = ""
		switch msg.String() {
		case "esc":
			m.done = true
			return m, nil

		case "ctrl+s":
			if m.saving {
				return m, nil
			}
			m.saving = true
			m.status = "Saving..."
			client := m.client
			noteID := m.noteID
			body := m.body.Value()
			return m, func() tea.Msg {
				_, err := client.UpdateNote(noteID, nil, &body)
				if err != nil {
					return apiErrorMsg{err: err}
				}
				return noteSavedMsg{}
			}

		case "ctrl+a":
			// Open AI assist command palette.
			// Get the current selection from the textarea (if any).
			selection := m.getSelection()
			m.showAIAssist = true
			m.aiAssistModel = newAIAssistModel(m.client, m.noteID, selection, m.width, m.height)
			return m, m.aiAssistModel.Init()
		}
	}

	// Update textarea.
	var cmd tea.Cmd
	m.body, cmd = m.body.Update(msg)
	// Mark modified on any key that is not a control sequence.
	if _, ok := msg.(tea.KeyMsg); ok {
		m.modified = true
	}
	return m, cmd
}

func (m editorModel) getSelection() string {
	// The bubbles textarea does not expose selection state,
	// so we pass empty string (the API will use the full note body).
	return ""
}

func (m editorModel) updateAIAssist(msg tea.Msg) (editorModel, tea.Cmd) {
	var cmd tea.Cmd
	m.aiAssistModel, cmd = m.aiAssistModel.Update(msg)

	if m.aiAssistModel.done {
		m.showAIAssist = false
		if m.aiAssistModel.applied && m.aiAssistModel.result != "" {
			// Insert the AI result at the end of the note body.
			current := m.body.Value()
			updated := current + "\n\n" + m.aiAssistModel.result
			m.body.SetValue(updated)
			m.modified = true
			m.status = "AI result inserted"
		}
		return m, nil
	}

	return m, cmd
}

func (m editorModel) View() string {
	if m.width == 0 {
		return ""
	}

	if m.showAIAssist {
		return m.aiAssistModel.View()
	}

	// Header with title.
	headerText := fmt.Sprintf(" %s", m.title)
	if m.modified {
		headerText += " [modified]"
	}
	header := styleHeader.Width(m.width).Render(headerText)

	// Editor body.
	var bodyView string
	if m.loading {
		bodyView = styleMuted.Render("Loading note...")
	} else {
		bodyView = m.body.View()
	}

	// Status bar.
	var statusParts []string
	if m.err != "" {
		statusParts = append(statusParts, styleError.Render(m.err))
	}
	if m.status != "" {
		statusParts = append(statusParts, styleSuccess.Render(m.status))
	}
	statusParts = append(statusParts, styleMuted.Render("Ctrl+S: save | Ctrl+A: AI assist | Esc: back"))
	statusBar := styleStatusBar.Width(m.width).Render(strings.Join(statusParts, "  |  "))

	return lipgloss.JoinVertical(lipgloss.Left, header, bodyView, statusBar)
}
