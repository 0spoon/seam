package main

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
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
	preview        viewport.Model
	previewMode    bool
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

// editorChromeHeight is the number of vertical rows reserved by the editor
// for its own chrome (header line, divider, status bar). Everything else
// is handed to the body textarea or preview viewport.
const editorChromeHeight = 3

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
	ta.Prompt = " "
	ta.EndOfBufferCharacter = ' '
	ta.SetStyles(editorTextareaStyles())

	ti := textinput.New()
	ti.Placeholder = "Note title"
	ti.CharLimit = 256

	vp := viewport.New()
	vp.SoftWrap = true

	bodyWidth, bodyHeight := editorBodyDimensions(width, height)
	ta.SetWidth(bodyWidth)
	ta.SetHeight(bodyHeight)
	ta.Focus()
	vp.SetWidth(bodyWidth)
	vp.SetHeight(bodyHeight)

	if width > 4 {
		ti.SetWidth(width - 4)
	}

	return editorModel{
		client:     client,
		noteID:     noteID,
		titleInput: ti,
		body:       ta,
		preview:    vp,
		loading:    true,
		width:      width,
		height:     height,
	}
}

// editorBodyDimensions returns the width and height the body pane should
// occupy given the overall terminal size. The body fills everything except
// the editor chrome (header + divider + status bar), so no vertical rows
// are wasted.
func editorBodyDimensions(width, height int) (int, int) {
	bodyWidth := max(width-2, 10)
	bodyHeight := max(height-editorChromeHeight, 3)
	return bodyWidth, bodyHeight
}

// editorTextareaStyles produces a textarea StyleSet themed to the active
// palette. We replace the library's bright-grey defaults with softer theme
// colors so the editor blends with the rest of the TUI.
func editorTextareaStyles() textarea.Styles {
	base := textarea.DefaultDarkStyles()
	cursorLine := lipgloss.NewStyle().Background(activeTheme.Selected).Foreground(activeTheme.Fg)
	eob := lipgloss.NewStyle().Foreground(activeTheme.Dim)
	text := lipgloss.NewStyle().Foreground(activeTheme.Fg)
	prompt := lipgloss.NewStyle().Foreground(activeTheme.Border)
	placeholder := lipgloss.NewStyle().Foreground(activeTheme.Dim).Italic(true)

	base.Focused.Base = lipgloss.NewStyle()
	base.Focused.Text = text
	base.Focused.CursorLine = cursorLine
	base.Focused.EndOfBuffer = eob
	base.Focused.Prompt = prompt
	base.Focused.Placeholder = placeholder

	base.Blurred.Base = lipgloss.NewStyle()
	base.Blurred.Text = text
	base.Blurred.CursorLine = lipgloss.NewStyle().Foreground(activeTheme.Fg)
	base.Blurred.EndOfBuffer = eob
	base.Blurred.Prompt = prompt
	base.Blurred.Placeholder = placeholder

	base.Cursor.Color = activeTheme.Primary
	return base
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
		bodyWidth, bodyHeight := editorBodyDimensions(m.width, m.height)
		m.body.SetWidth(bodyWidth)
		m.body.SetHeight(bodyHeight)
		m.preview.SetWidth(bodyWidth)
		m.preview.SetHeight(bodyHeight)
		if m.previewMode {
			m.preview.SetContent(renderMarkdown(m.body.Value(), bodyWidth))
		}
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

		case km.Matches(msg, ActionEditorTogglePreview):
			m.previewMode = !m.previewMode
			if m.previewMode {
				bodyWidth, _ := editorBodyDimensions(m.width, m.height)
				m.preview.SetContent(renderMarkdown(m.body.Value(), bodyWidth))
				m.preview.GotoTop()
				m.body.Blur()
			} else {
				m.body.Focus()
			}
			return m, nil
		}

		// Any key not matching ActionEditorBack resets the discard
		// confirmation so two unrelated keypresses cannot accidentally
		// trigger "discard on second esc".
		if !km.Matches(msg, ActionEditorBack) {
			m.confirmDiscard = false
		}
	}

	// Update the active input (title, body, or preview viewport).
	var cmd tea.Cmd
	switch {
	case m.editingTitle:
		prev := m.titleInput.Value()
		m.titleInput, cmd = m.titleInput.Update(msg)
		if m.titleInput.Value() != prev {
			m.modified = true
		}
	case m.previewMode:
		m.preview, cmd = m.preview.Update(msg)
	default:
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
			if m.previewMode {
				bodyWidth, _ := editorBodyDimensions(m.width, m.height)
				m.preview.SetContent(renderMarkdown(m.body.Value(), bodyWidth))
			}
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

	header := m.renderHeader()
	divider := styles.EditorDivider.Render(strings.Repeat("─", m.width))
	body := m.renderBody()
	statusBar := m.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left, header, divider, body, statusBar)
}

// renderHeader lays out the single-line editor title strip. In read mode it
// shows the note title in the primary color with an optional [modified]
// badge and a right-aligned mode indicator; in title-edit mode it swaps in
// the text input.
func (m editorModel) renderHeader() string {
	if m.editingTitle {
		label := styles.Muted.Render("Title ")
		inner := label + m.titleInput.View()
		return lipgloss.NewStyle().Width(m.width).Padding(0, 1).Render(inner)
	}

	title := strings.TrimSpace(m.titleInput.Value())
	if title == "" {
		title = "Untitled"
	}
	left := styles.EditorTitle.Render(title)
	if m.modified {
		left += " " + styles.EditorCrumb.Render("• modified")
	}

	mode := "EDIT"
	if m.previewMode {
		mode = "PREVIEW"
	}
	right := styles.EditorBadge.Render(mode)

	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

// renderBody returns either the textarea view or, when preview mode is on,
// the rendered-markdown viewport. A loading message takes precedence before
// the note has been fetched.
func (m editorModel) renderBody() string {
	if m.loading {
		return styles.Muted.Render(" Loading note...")
	}
	if m.previewMode {
		return m.preview.View()
	}
	return m.body.View()
}

// renderStatusBar builds the bottom strip: errors or transient status on
// the left, cursor position and key hints on the right, separated so the
// hint row stays anchored to the edge of the terminal.
func (m editorModel) renderStatusBar() string {
	km := currentKeymap()

	var leftParts []string
	switch {
	case m.err != "":
		leftParts = append(leftParts, styles.Error.Render(m.err))
	case m.status != "":
		leftParts = append(leftParts, styles.Success.Render(m.status))
	default:
		leftParts = append(leftParts, styles.Muted.Render(m.cursorStats()))
	}
	left := strings.Join(leftParts, "  ")

	hints := fmt.Sprintf("%s save  %s preview  %s title  %s AI  %s back",
		km.Display(ActionEditorSave),
		km.Display(ActionEditorTogglePreview),
		km.Display(ActionEditorToggleTitle),
		km.Display(ActionEditorAIAssist),
		km.Display(ActionEditorBack))
	right := styles.Muted.Render(hints)

	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right)-2, 1)
	inner := " " + left + strings.Repeat(" ", gap) + right + " "
	return styles.StatusBar.Width(m.width).Render(inner)
}

// cursorStats returns a short "ln:col / lines · words" string used when no
// transient error or status is showing. Mirrors the old markdown-count
// hint but with data a user actually checks while writing.
func (m editorModel) cursorStats() string {
	body := m.body.Value()
	lines := strings.Count(body, "\n") + 1
	if body == "" {
		lines = 0
	}
	words := len(strings.Fields(body))
	if m.previewMode {
		return fmt.Sprintf("%d lines · %d words", lines, words)
	}
	return fmt.Sprintf("ln %d:%d · %d lines · %d words",
		m.body.Line()+1, m.body.Column()+1, lines, words)
}
