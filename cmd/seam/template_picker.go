package main

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// templatePickerModel lets users pick a template before creating a note.
type templatePickerModel struct {
	client     *APIClient
	projectID  string
	templates  []TemplateMeta
	cursor     int
	titleInput textinput.Model
	err        string
	loading    bool
	applying   bool
	done       bool
	created    bool
	phase      templatePhase
	width      int
	height     int
}

// templatePhase tracks what stage of the template flow we are in.
type templatePhase int

const (
	tplPhaseLoading templatePhase = iota
	tplPhaseList
	tplPhaseTitle
)

// templatesLoadedMsg is sent when templates are loaded from the server.
type templatesLoadedMsg struct {
	templates []TemplateMeta
}

// templateAppliedMsg is sent when a template has been applied.
type templateAppliedMsg struct {
	body string
}

func newTemplatePickerModel(client *APIClient, projectID string, width, height int) templatePickerModel {
	ti := textinput.New()
	ti.Placeholder = "Note title"
	ti.CharLimit = 256
	ti.SetWidth(50)

	return templatePickerModel{
		client:     client,
		projectID:  projectID,
		titleInput: ti,
		phase:      tplPhaseLoading,
		loading:    true,
		width:      width,
		height:     height,
	}
}

func (m templatePickerModel) Init() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		templates, err := client.ListTemplates()
		if err != nil {
			return apiErrorMsg{err: err}
		}
		return templatesLoadedMsg{templates: templates}
	}
}

func (m templatePickerModel) Update(msg tea.Msg) (templatePickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case templatesLoadedMsg:
		m.loading = false
		m.templates = msg.templates
		if len(m.templates) == 0 {
			// No templates: go to title phase for a blank note.
			m.phase = tplPhaseTitle
			return m, m.titleInput.Focus()
		}
		m.phase = tplPhaseList
		return m, nil

	case templateAppliedMsg:
		m.applying = false
		// Create a note with the applied body.
		title := strings.TrimSpace(m.titleInput.Value())
		if title == "" && m.cursor < len(m.templates) {
			title = m.templates[m.cursor].Name
		}
		if title == "" {
			title = "Untitled"
		}
		client := m.client
		projectID := m.projectID
		body := msg.body
		return m, func() tea.Msg {
			note, err := client.CreateNote(title, body, projectID)
			if err != nil {
				return apiErrorMsg{err: err}
			}
			return noteCreatedMsg{note: note}
		}

	case noteCreatedMsg:
		m.done = true
		m.created = true
		return m, nil

	case apiErrorMsg:
		m.loading = false
		m.applying = false
		m.err = msg.err.Error()
		return m, nil

	case tea.KeyPressMsg:
		m.err = ""
		switch msg.String() {
		case "esc":
			if m.phase == tplPhaseTitle {
				// Go back to template list if we have templates.
				if len(m.templates) > 0 {
					m.phase = tplPhaseList
					m.titleInput.Blur()
					return m, nil
				}
			}
			m.done = true
			return m, nil
		}

		// Phase-specific key handling.
		switch m.phase {
		case tplPhaseList:
			return m.updateList(msg)
		case tplPhaseTitle:
			return m.updateTitle(msg)
		}
	}

	// Update title input if in title phase.
	if m.phase == tplPhaseTitle {
		var cmd tea.Cmd
		m.titleInput, cmd = m.titleInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m templatePickerModel) updateList(msg tea.KeyPressMsg) (templatePickerModel, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.cursor < len(m.templates)-1 {
			m.cursor++
		}
		return m, nil

	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case "enter":
		// Select this template and move to title input.
		m.phase = tplPhaseTitle
		return m, m.titleInput.Focus()
	}
	return m, nil
}

func (m templatePickerModel) updateTitle(msg tea.KeyPressMsg) (templatePickerModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.applying {
			return m, nil
		}
		// If no templates exist, create a blank note directly.
		if len(m.templates) == 0 {
			m.applying = true
			title := strings.TrimSpace(m.titleInput.Value())
			if title == "" {
				title = "Untitled"
			}
			client := m.client
			projectID := m.projectID
			return m, func() tea.Msg {
				note, err := client.CreateNote(title, "", projectID)
				if err != nil {
					return apiErrorMsg{err: err}
				}
				return noteCreatedMsg{note: note}
			}
		}
		if m.cursor >= len(m.templates) {
			m.err = "no template selected"
			return m, nil
		}
		m.applying = true
		client := m.client
		name := m.templates[m.cursor].Name
		vars := map[string]string{}
		title := strings.TrimSpace(m.titleInput.Value())
		if title != "" {
			vars["title"] = title
		}
		return m, func() tea.Msg {
			result, err := client.ApplyTemplate(name, vars)
			if err != nil {
				return apiErrorMsg{err: err}
			}
			return templateAppliedMsg{body: result.Body}
		}
	}

	var cmd tea.Cmd
	m.titleInput, cmd = m.titleInput.Update(msg)
	return m, cmd
}

func (m templatePickerModel) View() string {
	if m.width == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString(styles.Title.Render("New Note from Template"))
	b.WriteString("\n\n")

	switch m.phase {
	case tplPhaseLoading:
		b.WriteString(styles.Muted.Render("Loading templates..."))

	case tplPhaseList:
		for i, t := range m.templates {
			name := t.Name
			desc := t.Description
			if desc != "" {
				desc = styles.Muted.Render(" - " + desc)
			}

			if i == m.cursor {
				b.WriteString(styles.Selected.Render(fmt.Sprintf("> %s", name)))
				b.WriteString(desc)
			} else {
				b.WriteString(styles.Normal.Render(fmt.Sprintf("  %s", name)))
				b.WriteString(desc)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
		help := styles.Muted.Render("j/k: navigate | Enter: select | Esc: cancel")
		b.WriteString(help)

	case tplPhaseTitle:
		selected := ""
		if m.cursor < len(m.templates) {
			selected = m.templates[m.cursor].Name
		}
		b.WriteString(styles.Muted.Render(fmt.Sprintf("Template: %s", selected)))
		b.WriteString("\n\n")

		labelStyle := lipgloss.NewStyle().
			Foreground(activeTheme.Muted).
			Width(8).
			Align(lipgloss.Right)

		b.WriteString(labelStyle.Render("Title: "))
		b.WriteString(m.titleInput.View())
		b.WriteString("\n\n")

		if m.applying {
			b.WriteString(styles.Muted.Render("Creating note..."))
			b.WriteString("\n")
		}

		b.WriteString("\n")
		help := styles.Muted.Render("Enter: create | Esc: back")
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
		BorderForeground(activeTheme.Primary)

	rendered := box.Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, rendered)
}
