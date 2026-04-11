package main

import (
	"fmt"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// editorModel handles the full-screen note editor.
type editorModel struct {
	client         *APIClient
	noteID         string
	title          string
	titleInput     textinput.Model
	editingTitle   bool
	body           textarea.Model
	err            string
	status         string
	loading        bool
	saving         bool
	done           bool
	modified       bool
	confirmDiscard bool
	width          int
	height         int
	showAIAssist   bool
	aiAssistModel  aiAssistModel
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

	ti := textinput.New()
	ti.Placeholder = "Note title"
	ti.CharLimit = 256

	// Reserve space for header (title input), separator, and status bar.
	ta.SetWidth(width - 2)
	editorHeight := height - 6
	if editorHeight < 5 {
		editorHeight = 5
	}
	ta.SetHeight(editorHeight)
	ta.Focus()

	if width > 4 {
		ti.SetWidth(width - 4)
	}

	return editorModel{
		client:     client,
		noteID:     noteID,
		titleInput: ti,
		body:       ta,
		loading:    true,
		width:      width,
		height:     height,
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
		editorHeight := m.height - 6
		if editorHeight < 5 {
			editorHeight = 5
		}
		m.body.SetHeight(editorHeight)
		if m.width > 4 {
			m.titleInput.SetWidth(m.width - 4)
		}
		return m, nil

	case noteLoadedMsg:
		m.loading = false
		m.title = msg.note.Title
		m.titleInput.SetValue(msg.note.Title)
		m.body.SetValue(msg.note.Body)
		m.modified = false
		return m, nil

	case noteSavedMsg:
		m.saving = false
		m.modified = false
		m.title = strings.TrimSpace(m.titleInput.Value())
		m.status = "Saved"
		return m, nil

	case apiErrorMsg:
		m.loading = false
		m.saving = false
		m.err = msg.err.Error()
		return m, nil

	case tea.KeyPressMsg:
		m.err = ""
		m.status = ""
		km := currentKeymap()

		switch {
		case km.Matches(msg, ActionEditorSave):
			if m.saving {
				return m, nil
			}
			m.saving = true
			m.status = "Saving..."
			client := m.client
			noteID := m.noteID
			title := strings.TrimSpace(m.titleInput.Value())
			body := m.body.Value()
			var titlePtr *string
			if title != m.title {
				titlePtr = &title
			}
			return m, func() tea.Msg {
				_, err := client.UpdateNote(noteID, titlePtr, &body)
				if err != nil {
					return apiErrorMsg{err: err}
				}
				return noteSavedMsg{}
			}

		case km.Matches(msg, ActionEditorBack):
			if m.editingTitle {
				// Exit title editing, return focus to body.
				m.editingTitle = false
				m.titleInput.Blur()
				m.body.Focus()
				m.title = strings.TrimSpace(m.titleInput.Value())
				return m, nil
			}
			if m.modified && !m.confirmDiscard {
				m.confirmDiscard = true
				m.status = "Unsaved changes. Press Esc again to discard."
				return m, nil
			}
			m.done = true
			return m, nil

		case km.Matches(msg, ActionEditorToggleTitle):
			m.editingTitle = !m.editingTitle
			if m.editingTitle {
				m.body.Blur()
				return m, m.titleInput.Focus()
			}
			m.titleInput.Blur()
			m.title = strings.TrimSpace(m.titleInput.Value())
			m.body.Focus()
			return m, nil

		case km.Matches(msg, ActionEditorAIAssist):
			selection := m.getSelection()
			m.showAIAssist = true
			m.aiAssistModel = newAIAssistModel(m.client, m.noteID, selection, m.width, m.height)
			return m, m.aiAssistModel.Init()
		}

		// Any key not matching ActionEditorBack resets the discard
		// confirmation so two unrelated keypresses cannot accidentally
		// trigger "discard on second esc".
		if !km.Matches(msg, ActionEditorBack) {
			m.confirmDiscard = false
		}
	}

	// Update the active input (title or body).
	var cmd tea.Cmd
	if m.editingTitle {
		prev := m.titleInput.Value()
		m.titleInput, cmd = m.titleInput.Update(msg)
		if m.titleInput.Value() != prev {
			m.modified = true
		}
	} else {
		prev := m.body.Value()
		m.body, cmd = m.body.Update(msg)
		if m.body.Value() != prev {
			m.modified = true
		}
	}
	return m, cmd
}

func (m editorModel) getSelection() string {
	// LIMITATION: The Bubbles textarea component (charmbracelet/bubbles)
	// does not expose text selection state or selected text content.
	// There is no API to retrieve the user's selection programmatically.
	// As a workaround, we pass an empty string, which signals to the AI
	// assist endpoint to operate on the full note body instead.
	// See: https://github.com/charmbracelet/bubbles/issues/textarea
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

	// Title section: editable input or static display.
	var titleView string
	if m.editingTitle {
		titleLabel := styles.Muted.Render("Title: ")
		titleView = styles.Header.Width(m.width).Render(titleLabel + m.titleInput.View())
	} else {
		headerText := fmt.Sprintf(" %s", m.titleInput.Value())
		if m.modified {
			headerText += " [modified]"
		}
		titleView = styles.Header.Width(m.width).Render(headerText)
	}

	// Editor body.
	var bodyView string
	if m.loading {
		bodyView = styles.Muted.Render("Loading note...")
	} else {
		bodyView = m.body.View()
	}

	// Markdown preview bar: show heading highlights for the current line.
	mdHint := renderMarkdownHint(m.body.Value(), m.width)

	// Status bar.
	var statusParts []string
	if m.err != "" {
		statusParts = append(statusParts, styles.Error.Render(m.err))
	}
	if m.status != "" {
		statusParts = append(statusParts, styles.Success.Render(m.status))
	}
	km := currentKeymap()
	statusParts = append(statusParts, styles.Muted.Render(fmt.Sprintf("%s: save | %s: title | %s: AI | %s: back",
		km.Display(ActionEditorSave),
		km.Display(ActionEditorToggleTitle),
		km.Display(ActionEditorAIAssist),
		km.Display(ActionEditorBack))))
	statusBar := styles.StatusBar.Width(m.width).Render(strings.Join(statusParts, "  |  "))

	parts := []string{titleView, bodyView}
	if mdHint != "" {
		parts = append(parts, mdHint)
	}
	parts = append(parts, statusBar)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// Markdown syntax highlighting regex patterns.
var (
	reH1     = regexp.MustCompile(`^# .+`)
	reH2     = regexp.MustCompile(`^## .+`)
	reH3     = regexp.MustCompile(`^### .+`)
	reBold   = regexp.MustCompile(`\*\*[^*]+\*\*`)
	reItalic = regexp.MustCompile(`\*[^*]+\*`)
	reCode   = regexp.MustCompile("`[^`]+`")
	reLink   = regexp.MustCompile(`\[[^\]]+\]\([^)]+\)`)
)

// renderMarkdownHint produces a single-line summary of markdown elements
// detected in the note body. Because the Bubbles textarea does not support
// inline styled rendering, full syntax highlighting within the editor is
// not feasible. Instead, we show a status line indicating the structure
// of the document: headings, bold, italic, code, and links found.
func renderMarkdownHint(body string, width int) string {
	if body == "" {
		return ""
	}

	headingStyle := lipgloss.NewStyle().Foreground(activeTheme.Primary).Bold(true)
	codeStyle := lipgloss.NewStyle().Foreground(activeTheme.Secondary)
	linkStyle := lipgloss.NewStyle().Foreground(activeTheme.Primary).Underline(true)

	lines := strings.Split(body, "\n")
	var hints []string
	headingCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if reH1.MatchString(trimmed) || reH2.MatchString(trimmed) || reH3.MatchString(trimmed) {
			headingCount++
		}
	}
	if headingCount > 0 {
		hints = append(hints, headingStyle.Render(fmt.Sprintf("%d heading(s)", headingCount)))
	}
	boldCount := len(reBold.FindAllString(body, -1))
	if boldCount > 0 {
		hints = append(hints, lipgloss.NewStyle().Bold(true).Foreground(activeTheme.Fg).Render(fmt.Sprintf("%d bold", boldCount)))
	}
	italicCount := len(reItalic.FindAllString(body, -1)) - boldCount
	if italicCount > 0 {
		hints = append(hints, lipgloss.NewStyle().Italic(true).Foreground(activeTheme.Fg).Render(fmt.Sprintf("%d italic", italicCount)))
	}
	codeCount := len(reCode.FindAllString(body, -1))
	if codeCount > 0 {
		hints = append(hints, codeStyle.Render(fmt.Sprintf("%d code", codeCount)))
	}
	linkCount := len(reLink.FindAllString(body, -1))
	if linkCount > 0 {
		hints = append(hints, linkStyle.Render(fmt.Sprintf("%d link(s)", linkCount)))
	}

	if len(hints) == 0 {
		return ""
	}
	return styles.Muted.Render(" md: ") + strings.Join(hints, styles.Muted.Render(" | "))
}
