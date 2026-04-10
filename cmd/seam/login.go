package main

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// loginField identifies which text input is focused.
type loginField int

const (
	fieldServerURL loginField = iota
	fieldUsername
	fieldEmail
	fieldPassword
)

// loginModel handles the login screen.
type loginModel struct {
	serverURL textinput.Model
	username  textinput.Model
	email     textinput.Model
	password  textinput.Model
	focused   loginField
	err       string
	loading   bool
	width     int
	height    int
	client    *APIClient
	register  bool // toggle between login and register mode
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
	serverURLInput := textinput.New()
	serverURLInput.Placeholder = "http://localhost:8080"
	serverURLInput.CharLimit = 256
	serverURLInput.SetValue(client.BaseURL)

	usernameInput := textinput.New()
	usernameInput.Placeholder = "username"
	usernameInput.CharLimit = 64
	usernameInput.Focus()

	emailInput := textinput.New()
	emailInput.Placeholder = "email"
	emailInput.CharLimit = 256

	passwordInput := textinput.New()
	passwordInput.Placeholder = "password"
	passwordInput.EchoMode = textinput.EchoPassword
	passwordInput.EchoCharacter = '*'
	passwordInput.CharLimit = 128

	return loginModel{
		serverURL: serverURLInput,
		username:  usernameInput,
		email:     emailInput,
		password:  passwordInput,
		focused:   fieldUsername,
		client:    client,
	}
}

func (m loginModel) Init() tea.Cmd {
	return textinput.Blink
}

// focusField sets focus to the given login field.
func (m *loginModel) focusField(field loginField) tea.Cmd {
	m.focused = field
	m.serverURL.Blur()
	m.username.Blur()
	m.email.Blur()
	m.password.Blur()
	switch field {
	case fieldServerURL:
		return m.serverURL.Focus()
	case fieldUsername:
		return m.username.Focus()
	case fieldEmail:
		return m.email.Focus()
	case fieldPassword:
		return m.password.Focus()
	}
	return nil
}

// visibleFields returns the ordered list of fields for the current mode.
func (m loginModel) visibleFields() []loginField {
	if m.register {
		return []loginField{fieldServerURL, fieldUsername, fieldEmail, fieldPassword}
	}
	return []loginField{fieldServerURL, fieldUsername, fieldPassword}
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

	case tea.KeyPressMsg:
		m.err = ""
		switch msg.String() {
		case "tab", "shift+tab", "down", "up":
			fields := m.visibleFields()
			// Find current index in the visible fields list.
			curIdx := 0
			for i, f := range fields {
				if f == m.focused {
					curIdx = i
					break
				}
			}
			if msg.String() == "tab" || msg.String() == "down" {
				curIdx = (curIdx + 1) % len(fields)
			} else {
				curIdx = (curIdx - 1 + len(fields)) % len(fields)
			}
			cmd := m.focusField(fields[curIdx])
			return m, cmd

		case "enter":
			if m.loading {
				return m, nil
			}
			// Apply server URL if provided.
			serverURL := strings.TrimSpace(m.serverURL.Value())
			if serverURL != "" {
				m.client.BaseURL = serverURL
			}
			username := strings.TrimSpace(m.username.Value())
			password := m.password.Value()
			if username == "" || password == "" {
				m.err = "username and password are required"
				return m, nil
			}
			if m.register {
				email := strings.TrimSpace(m.email.Value())
				if email == "" {
					m.err = "email is required for registration"
					return m, nil
				}
				m.loading = true
				client := m.client
				return m, func() tea.Msg {
					resp, err := client.Register(username, email, password)
					if err != nil {
						return loginErrorMsg{err: err}
					}
					return loginSuccessMsg{auth: resp}
				}
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
			// If the focused field is email and we switched to login mode,
			// move focus to password.
			if !m.register && m.focused == fieldEmail {
				cmd := m.focusField(fieldPassword)
				return m, cmd
			}
			return m, nil
		}
	}

	// Update the focused text input.
	var cmd tea.Cmd
	switch m.focused {
	case fieldServerURL:
		m.serverURL, cmd = m.serverURL.Update(msg)
	case fieldUsername:
		m.username, cmd = m.username.Update(msg)
	case fieldEmail:
		m.email, cmd = m.email.Update(msg)
	case fieldPassword:
		m.password, cmd = m.password.Update(msg)
	}
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

	title := styles.Title.Render("Seam - " + modeLabel)
	b.WriteString(title)
	b.WriteString("\n\n")

	labelStyle := lipgloss.NewStyle().
		Width(12).
		Foreground(activeTheme.Muted).
		Align(lipgloss.Right)

	b.WriteString(labelStyle.Render("Server: "))
	b.WriteString(m.serverURL.View())
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("Username: "))
	b.WriteString(m.username.View())
	b.WriteString("\n\n")

	if m.register {
		b.WriteString(labelStyle.Render("Email: "))
		b.WriteString(m.email.View())
		b.WriteString("\n\n")
	}

	b.WriteString(labelStyle.Render("Password: "))
	b.WriteString(m.password.View())
	b.WriteString("\n\n")

	if m.err != "" {
		b.WriteString(styles.Error.Render(m.err))
		b.WriteString("\n\n")
	}

	if m.loading {
		b.WriteString(styles.Muted.Render("Authenticating..."))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	help := styles.Muted.Render("Enter: submit | Tab: next field | Ctrl+R: toggle login/register | Ctrl+C: quit")
	b.WriteString(help)

	// Center the form in the terminal.
	content := b.String()
	formWidth := 60
	box := lipgloss.NewStyle().
		Width(formWidth).
		Padding(2, 4).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(activeTheme.Border)

	rendered := box.Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, rendered)
}
