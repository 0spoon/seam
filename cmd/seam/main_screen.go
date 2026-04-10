package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// pane identifies which pane is active.
type pane int

const (
	paneProjects pane = iota
	paneNotes
)

// projectItem is either "Inbox" or a real project.
type projectItem struct {
	id   string // empty for inbox
	name string
}

const notesPerPage = 20

// mainScreenModel is the main two-pane view showing projects and notes.
type mainScreenModel struct {
	client              *APIClient
	width               int
	height              int
	activePane          pane
	projects            []projectItem
	projectIdx          int
	notes               []*Note
	noteIdx             int
	totalNotes          int
	page                int
	err                 string
	loading             bool
	confirmDelete       bool
	username            string
	showCapture         bool
	captureModel        captureModel
	showURLCapture      bool
	urlCaptureModel     urlCaptureModel
	showVoiceCapture    bool
	voiceCaptureModel   voiceCaptureModel
	showTemplatePicker  bool
	templatePickerModel templatePickerModel
}

// -- Messages ----------------------------------------------------------------

type projectsLoadedMsg struct {
	projects []*Project
}

type notesLoadedMsg struct {
	notes []*Note
	total int
}

type apiErrorMsg struct {
	err error
}

type noteDeletedMsg struct{}

func newMainScreenModel(client *APIClient, username string) mainScreenModel {
	return mainScreenModel{
		client:   client,
		username: username,
	}
}

func (m mainScreenModel) Init() tea.Cmd {
	return m.loadProjects()
}

func (m mainScreenModel) loadProjects() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		projects, err := client.ListProjects()
		if err != nil {
			return apiErrorMsg{err: err}
		}
		return projectsLoadedMsg{projects: projects}
	}
}

func (m mainScreenModel) loadNotes() tea.Cmd {
	if len(m.projects) == 0 {
		return nil
	}
	client := m.client
	item := m.projects[m.projectIdx]
	offset := m.page * notesPerPage
	limit := notesPerPage

	return func() tea.Msg {
		projectFilter := item.id
		if item.id == "" {
			projectFilter = "inbox"
		}
		notes, total, err := client.ListNotesPaged(projectFilter, offset, limit)
		if err != nil {
			return apiErrorMsg{err: err}
		}
		return notesLoadedMsg{notes: notes, total: total}
	}
}

func (m mainScreenModel) Update(msg tea.Msg) (mainScreenModel, tea.Cmd) {
	// If any overlay is open, delegate to it.
	if m.showCapture {
		return m.updateCapture(msg)
	}
	if m.showURLCapture {
		return m.updateURLCapture(msg)
	}
	if m.showVoiceCapture {
		return m.updateVoiceCapture(msg)
	}
	if m.showTemplatePicker {
		return m.updateTemplatePicker(msg)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case projectsLoadedMsg:
		m.loading = false
		m.projects = []projectItem{{id: "", name: "Inbox"}}
		for _, p := range msg.projects {
			m.projects = append(m.projects, projectItem{id: p.ID, name: p.Name})
		}
		m.projectIdx = 0
		m.page = 0
		return m, m.loadNotes()

	case notesLoadedMsg:
		m.loading = false
		m.notes = msg.notes
		m.totalNotes = msg.total
		if m.noteIdx >= len(m.notes) {
			m.noteIdx = 0
		}
		return m, nil

	case apiErrorMsg:
		m.loading = false
		m.err = msg.err.Error()
		return m, nil

	case noteDeletedMsg:
		m.err = ""
		return m, m.loadNotes()

	case tea.KeyPressMsg:
		m.err = ""
		// Any key other than "d" resets the delete confirmation.
		if msg.String() != "d" {
			m.confirmDelete = false
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "tab":
			if m.activePane == paneProjects {
				m.activePane = paneNotes
			} else {
				m.activePane = paneProjects
			}
			return m, nil

		case "j", "down":
			if m.activePane == paneProjects {
				if m.projectIdx < len(m.projects)-1 {
					m.projectIdx++
					m.page = 0
					m.loading = true
					return m, m.loadNotes()
				}
			} else {
				if m.noteIdx < len(m.notes)-1 {
					m.noteIdx++
				}
			}
			return m, nil

		case "k", "up":
			if m.activePane == paneProjects {
				if m.projectIdx > 0 {
					m.projectIdx--
					m.page = 0
					m.loading = true
					return m, m.loadNotes()
				}
			} else {
				if m.noteIdx > 0 {
					m.noteIdx--
				}
			}
			return m, nil

		case "enter":
			if m.activePane == paneNotes && len(m.notes) > 0 {
				note := m.notes[m.noteIdx]
				return m, func() tea.Msg {
					return openEditorMsg{noteID: note.ID}
				}
			}
			return m, nil

		case "c":
			// Quick capture: open capture modal.
			projectID := ""
			if m.projectIdx < len(m.projects) {
				projectID = m.projects[m.projectIdx].id
			}
			m.showCapture = true
			m.captureModel = newCaptureModel(m.client, projectID, m.width, m.height)
			return m, m.captureModel.Init()

		case "n":
			// Create note from template: open template picker.
			projectID := ""
			if m.projectIdx < len(m.projects) {
				projectID = m.projects[m.projectIdx].id
			}
			m.showTemplatePicker = true
			m.templatePickerModel = newTemplatePickerModel(m.client, projectID, m.width, m.height)
			return m, m.templatePickerModel.Init()

		case "u":
			// URL capture: open URL input modal.
			m.showURLCapture = true
			m.urlCaptureModel = newURLCaptureModel(m.client, m.width, m.height)
			return m, m.urlCaptureModel.Init()

		case "v":
			// Voice capture: open voice recording modal.
			m.showVoiceCapture = true
			m.voiceCaptureModel = newVoiceCaptureModel(m.client, m.width, m.height)
			return m, m.voiceCaptureModel.Init()

		case "/":
			return m, func() tea.Msg {
				return openSearchMsg{}
			}

		case "a":
			return m, func() tea.Msg {
				return openAskMsg{}
			}

		case "t":
			return m, func() tea.Msg {
				return openTimelineMsg{}
			}

		case ",":
			return m, func() tea.Msg {
				return openSettingsMsg{}
			}

		case "d":
			if m.activePane == paneNotes && len(m.notes) > 0 {
				if !m.confirmDelete {
					m.confirmDelete = true
					m.err = "Press d again to confirm delete"
					return m, nil
				}
				m.confirmDelete = false
				note := m.notes[m.noteIdx]
				client := m.client
				return m, func() tea.Msg {
					if err := client.DeleteNote(note.ID); err != nil {
						return apiErrorMsg{err: err}
					}
					return noteDeletedMsg{}
				}
			}
			return m, nil

		case "ctrl+f":
			// Next page.
			totalPages := m.totalPages()
			if m.page < totalPages-1 {
				m.page++
				m.noteIdx = 0
				m.loading = true
				return m, m.loadNotes()
			}
			return m, nil

		case "ctrl+b":
			// Previous page.
			if m.page > 0 {
				m.page--
				m.noteIdx = 0
				m.loading = true
				return m, m.loadNotes()
			}
			return m, nil

		case "r":
			m.loading = true
			return m, m.loadProjects()
		}
	}

	return m, nil
}

func (m mainScreenModel) totalPages() int {
	if m.totalNotes <= 0 {
		return 1
	}
	pages := m.totalNotes / notesPerPage
	if m.totalNotes%notesPerPage != 0 {
		pages++
	}
	return pages
}

func (m mainScreenModel) updateCapture(msg tea.Msg) (mainScreenModel, tea.Cmd) {
	var cmd tea.Cmd
	m.captureModel, cmd = m.captureModel.Update(msg)

	if m.captureModel.done {
		m.showCapture = false
		if m.captureModel.created {
			return m, m.loadNotes()
		}
		return m, nil
	}

	return m, cmd
}

func (m mainScreenModel) updateURLCapture(msg tea.Msg) (mainScreenModel, tea.Cmd) {
	var cmd tea.Cmd
	m.urlCaptureModel, cmd = m.urlCaptureModel.Update(msg)

	if m.urlCaptureModel.done {
		m.showURLCapture = false
		if m.urlCaptureModel.created {
			return m, m.loadNotes()
		}
		return m, nil
	}

	return m, cmd
}

func (m mainScreenModel) updateVoiceCapture(msg tea.Msg) (mainScreenModel, tea.Cmd) {
	var cmd tea.Cmd
	m.voiceCaptureModel, cmd = m.voiceCaptureModel.Update(msg)

	if m.voiceCaptureModel.done {
		m.showVoiceCapture = false
		if m.voiceCaptureModel.created {
			return m, m.loadNotes()
		}
		return m, nil
	}

	return m, cmd
}

func (m mainScreenModel) updateTemplatePicker(msg tea.Msg) (mainScreenModel, tea.Cmd) {
	var cmd tea.Cmd
	m.templatePickerModel, cmd = m.templatePickerModel.Update(msg)

	if m.templatePickerModel.done {
		m.showTemplatePicker = false
		if m.templatePickerModel.created {
			return m, m.loadNotes()
		}
		return m, nil
	}

	return m, cmd
}

func (m mainScreenModel) View() string {
	if m.width == 0 {
		return ""
	}

	if m.showCapture {
		return m.captureModel.View()
	}
	if m.showURLCapture {
		return m.urlCaptureModel.View()
	}
	if m.showVoiceCapture {
		return m.voiceCaptureModel.View()
	}
	if m.showTemplatePicker {
		return m.templatePickerModel.View()
	}

	// Header.
	header := styles.Header.Width(m.width).Render(
		fmt.Sprintf(" Seam  |  %s  |  %d notes", m.username, m.totalNotes),
	)

	// Status bar.
	pageInfo := ""
	totalPages := m.totalPages()
	if totalPages > 1 {
		pageInfo = fmt.Sprintf(" | Page %d/%d (Ctrl+F/B)", m.page+1, totalPages)
	}
	statusText := "j/k: nav | Tab: pane | Enter: open | c: capture | n: template | u: URL | v: voice | /: search | a: ask | ,: settings | d: del | q: quit" + pageInfo
	if m.err != "" {
		statusText = styles.Error.Render(m.err)
	}
	statusBar := styles.StatusBar.Width(m.width).Render(statusText)

	// Calculate pane dimensions.
	contentHeight := m.height - 3 // header + status + borders
	if contentHeight < 1 {
		contentHeight = 1
	}
	leftWidth := m.width/4 - 2
	if leftWidth < 15 {
		leftWidth = 15
	}
	rightWidth := m.width - leftWidth - 6 // account for borders
	if rightWidth < 20 {
		rightWidth = 20
	}

	// Project list pane.
	leftPaneStyle := styles.Pane
	if m.activePane == paneProjects {
		leftPaneStyle = styles.PaneActive
	}
	projectList := m.renderProjectList(leftWidth, contentHeight-2)
	leftPane := leftPaneStyle.Width(leftWidth).Height(contentHeight - 2).Render(projectList)

	// Note list pane.
	rightPaneStyle := styles.Pane
	if m.activePane == paneNotes {
		rightPaneStyle = styles.PaneActive
	}
	noteList := m.renderNoteList(rightWidth, contentHeight-2)
	rightPane := rightPaneStyle.Width(rightWidth).Height(contentHeight - 2).Render(noteList)

	content := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)

	return lipgloss.JoinVertical(lipgloss.Left, header, content, statusBar)
}

func (m mainScreenModel) renderProjectList(width, height int) string {
	var b strings.Builder
	title := styles.Title.Render("Projects")
	b.WriteString(title)
	b.WriteString("\n\n")

	if len(m.projects) == 0 {
		b.WriteString(styles.Muted.Render("  No projects"))
		return b.String()
	}

	for i, p := range m.projects {
		if i >= height-2 {
			b.WriteString(styles.Muted.Render(fmt.Sprintf("  ... +%d more", len(m.projects)-i)))
			break
		}
		label := p.name
		if i == m.projectIdx {
			b.WriteString(styles.Selected.Width(width - 2).Render("> " + label))
		} else {
			b.WriteString(styles.Normal.Width(width - 2).Render("  " + label))
		}
		if i < len(m.projects)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m mainScreenModel) renderNoteList(width, height int) string {
	var b strings.Builder
	title := styles.Title.Render("Notes")
	if m.loading {
		title += styles.Muted.Render(" (loading...)")
	}
	b.WriteString(title)
	b.WriteString("\n\n")

	if len(m.notes) == 0 {
		b.WriteString(styles.Muted.Render("  No notes"))
		return b.String()
	}

	for i, n := range m.notes {
		if i >= height-2 {
			b.WriteString(styles.Muted.Render(fmt.Sprintf("  ... +%d more", len(m.notes)-i)))
			break
		}

		noteTitle := n.Title
		if noteTitle == "" {
			noteTitle = "(untitled)"
		}

		// Truncate title if needed.
		maxTitleLen := width - 6
		if maxTitleLen < 10 {
			maxTitleLen = 10
		}
		if runes := []rune(noteTitle); len(runes) > maxTitleLen {
			noteTitle = string(runes[:maxTitleLen-3]) + "..."
		}

		line := noteTitle
		if len(n.Tags) > 0 {
			tags := styles.Muted.Render(" [" + strings.Join(n.Tags, ", ") + "]")
			line += tags
		}

		if i == m.noteIdx {
			b.WriteString(styles.Selected.Width(width - 2).Render("> " + line))
		} else {
			b.WriteString(styles.Normal.Width(width - 2).Render("  " + line))
		}
		if i < len(m.notes)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}
