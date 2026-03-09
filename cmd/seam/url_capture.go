package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// urlCaptureModel handles URL capture via a simple modal.
type urlCaptureModel struct {
	urlInput textinput.Model
	client   *APIClient
	err      string
	loading  bool
	done     bool
	created  bool
	width    int
	height   int
}

// urlCapturedMsg is sent when a URL is successfully captured.
type urlCapturedMsg struct {
	note *Note
}

func newURLCaptureModel(client *APIClient, width, height int) urlCaptureModel {
	ti := textinput.New()
	ti.Placeholder = "https://example.com/article"
	ti.CharLimit = 2048
	ti.Width = 50
	ti.Focus()

	return urlCaptureModel{
		urlInput: ti,
		client:   client,
		width:    width,
		height:   height,
	}
}

func (m urlCaptureModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m urlCaptureModel) Update(msg tea.Msg) (urlCaptureModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case urlCapturedMsg:
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

		case "enter":
			if m.loading {
				return m, nil
			}
			rawURL := strings.TrimSpace(m.urlInput.Value())
			if rawURL == "" {
				m.err = "URL is required"
				return m, nil
			}
			if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
				rawURL = "https://" + rawURL
			}
			m.loading = true
			client := m.client
			return m, func() tea.Msg {
				note, err := client.CaptureURL(rawURL)
				if err != nil {
					return apiErrorMsg{err: err}
				}
				return urlCapturedMsg{note: note}
			}
		}
	}

	var cmd tea.Cmd
	m.urlInput, cmd = m.urlInput.Update(msg)
	return m, cmd
}

func (m urlCaptureModel) View() string {
	if m.width == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString(styleTitle.Render("Capture URL"))
	b.WriteString("\n\n")

	labelStyle := lipgloss.NewStyle().
		Foreground(colorMuted).
		Width(8).
		Align(lipgloss.Right)

	b.WriteString(labelStyle.Render("URL: "))
	b.WriteString(m.urlInput.View())
	b.WriteString("\n\n")

	if m.err != "" {
		b.WriteString(styleError.Render(m.err))
		b.WriteString("\n\n")
	}

	if m.loading {
		b.WriteString(styleMuted.Render("Fetching page..."))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	help := styleMuted.Render("Enter: capture | Esc: cancel")
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
