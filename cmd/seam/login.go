package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// loginField identifies which text input is focused.
type loginField int

const (
	fieldUsername loginField = iota
	fieldPassword
	loginFieldCount
)

// loginModel handles the login screen.
type loginModel struct {
	inputs   [loginFieldCount]textinput.Model
	focused  loginField
	err      string
	loading  bool
	width    int
	height   int
	client   *APIClient
	register bool // toggle between login and register mode
}

// loginSuccessMsg is sent when authentication succeeds.
type loginSuccessMsg struct {
	auth *AuthResponse
}

// loginErrorMsg is sent when authentication fails.
type loginErrorMsg struct {
	err error
}

func newLoginModel(client *APIClient) loginModel {
	var inputs [loginFieldCount]textinput.Model

	usernameInput := textinput.New()
	usernameInput.Placeholder = "username"
	usernameInput.CharLimit = 64
	usernameInput.Focus()
	inputs[fieldUsername] = usernameInput

	passwordInput := textinput.New()
	passwordInput.Placeholder = "password"
	passwordInput.EchoMode = textinput.EchoPassword
	passwordInput.EchoCharacter = '*'
	passwordInput.CharLimit = 128
	inputs[fieldPassword] = passwordInput

	return loginModel{
		inputs: inputs,
		client: client,
	}
}

func (m loginModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m loginModel) Update(msg tea.Msg) (loginModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case loginSuccessMsg:
		m.loading = false
		return m, nil

	case loginErrorMsg:
		m.loading = false
		m.err = msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		m.err = ""
		switch msg.String() {
		case "tab", "shift+tab", "down", "up":
			if msg.String() == "tab" || msg.String() == "down" {
				m.focused = (m.focused + 1) % loginFieldCount
			} else {
				m.focused = (m.focused - 1 + loginFieldCount) % loginFieldCount
			}
			var cmds []tea.Cmd
			for i := range m.inputs {
				if loginField(i) == m.focused {
					cmds = append(cmds, m.inputs[i].Focus())
				} else {
					m.inputs[i].Blur()
				}
			}
			return m, tea.Batch(cmds...)

		case "enter":
			if m.loading {
				return m, nil
			}
			username := strings.TrimSpace(m.inputs[fieldUsername].Value())
			password := m.inputs[fieldPassword].Value()
			if username == "" || password == "" {
				m.err = "username and password are required"
				return m, nil
			}
			m.loading = true
			client := m.client
			return m, func() tea.Msg {
				resp, err := client.Login(username, password)
				if err != nil {
					return loginErrorMsg{err: err}
				}
				return loginSuccessMsg{auth: resp}
			}

		case "ctrl+r":
			m.register = !m.register
			return m, nil
		}
	}

	// Update the focused text input.
	var cmd tea.Cmd
	m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
	return m, cmd
}

func (m loginModel) View() string {
	if m.width == 0 {
		return ""
	}

	var b strings.Builder

	modeLabel := "Login"
	if m.register {
		modeLabel = "Register"
	}

	title := styleTitle.Render("Seam - " + modeLabel)
	b.WriteString(title)
	b.WriteString("\n\n")

	labelStyle := lipgloss.NewStyle().
		Width(12).
		Foreground(colorMuted).
		Align(lipgloss.Right)

	b.WriteString(labelStyle.Render("Username: "))
	b.WriteString(m.inputs[fieldUsername].View())
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("Password: "))
	b.WriteString(m.inputs[fieldPassword].View())
	b.WriteString("\n\n")

	if m.err != "" {
		b.WriteString(styleError.Render(m.err))
		b.WriteString("\n\n")
	}

	if m.loading {
		b.WriteString(styleMuted.Render("Authenticating..."))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	help := styleMuted.Render("Enter: submit | Tab: next field | Ctrl+R: toggle login/register | Ctrl+C: quit")
	b.WriteString(help)

	// Center the form in the terminal.
	content := b.String()
	formWidth := 60
	box := lipgloss.NewStyle().
		Width(formWidth).
		Padding(2, 4).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder)

	rendered := box.Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, rendered)
}
