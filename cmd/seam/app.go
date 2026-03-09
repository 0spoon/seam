package main

import (
	tea "github.com/charmbracelet/bubbletea"
)

// screen identifies which screen is currently active.
type screen int

const (
	screenLogin screen = iota
	screenMain
	screenEditor
	screenSearch
	screenAsk
)

// appModel is the root Bubble Tea model that delegates to sub-models.
type appModel struct {
	screen      screen
	client      *APIClient
	username    string
	width       int
	height      int
	loginModel  loginModel
	mainModel   mainScreenModel
	editorModel editorModel
	searchModel searchModel
	askModel    askModel
}

func newAppModel(client *APIClient, authenticated bool, username string) appModel {
	m := appModel{
		client:   client,
		username: username,
	}

	if authenticated {
		m.screen = screenMain
		m.mainModel = newMainScreenModel(client, username)
	} else {
		m.screen = screenLogin
		m.loginModel = newLoginModel(client)
	}

	return m
}

func (m appModel) Init() tea.Cmd {
	switch m.screen {
	case screenLogin:
		return m.loginModel.Init()
	case screenMain:
		return m.mainModel.Init()
	default:
		return nil
	}
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle window size globally.
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = wsm.Width
		m.height = wsm.Height
	}

	// Handle global quit.
	if km, ok := msg.(tea.KeyMsg); ok {
		if km.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	switch m.screen {
	case screenLogin:
		return m.updateLogin(msg)
	case screenMain:
		return m.updateMain(msg)
	case screenEditor:
		return m.updateEditor(msg)
	case screenSearch:
		return m.updateSearch(msg)
	case screenAsk:
		return m.updateAsk(msg)
	}

	return m, nil
}

func (m appModel) updateLogin(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.loginModel, cmd = m.loginModel.Update(msg)

	// Check if login succeeded.
	if resp, ok := msg.(loginSuccessMsg); ok {
		// Save tokens to disk.
		_ = SaveAuth(&AuthData{
			ServerURL:    m.client.BaseURL,
			AccessToken:  resp.auth.Tokens.AccessToken,
			RefreshToken: resp.auth.Tokens.RefreshToken,
			Username:     resp.auth.User.Username,
		})

		m.username = resp.auth.User.Username
		m.screen = screenMain
		m.mainModel = newMainScreenModel(m.client, m.username)
		m.mainModel.width = m.width
		m.mainModel.height = m.height
		return m, m.mainModel.Init()
	}

	return m, cmd
}

func (m appModel) updateMain(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Check for screen-switching messages.
	switch msg := msg.(type) {
	case openEditorMsg:
		m.screen = screenEditor
		m.editorModel = newEditorModel(m.client, msg.noteID, m.width, m.height)
		return m, m.editorModel.Init()

	case openSearchMsg:
		m.screen = screenSearch
		m.searchModel = newSearchModel(m.client, m.width, m.height)
		return m, m.searchModel.Init()

	case openAskMsg:
		m.screen = screenAsk
		m.askModel = newAskModel(m.client, m.width, m.height)
		return m, m.askModel.Init()
	}

	var cmd tea.Cmd
	m.mainModel, cmd = m.mainModel.Update(msg)
	return m, cmd
}

func (m appModel) updateEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Check for editor-triggered screen switches first.
	if _, ok := msg.(openEditorMsg); ok {
		// Re-entering editor from search result: handle in caller.
	}

	var cmd tea.Cmd
	m.editorModel, cmd = m.editorModel.Update(msg)

	if m.editorModel.done {
		m.screen = screenMain
		// Reload notes since the note may have been edited.
		return m, m.mainModel.loadNotes()
	}

	return m, cmd
}

func (m appModel) updateSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
	// If search triggers opening a note, switch to editor.
	if oe, ok := msg.(openEditorMsg); ok {
		m.screen = screenEditor
		m.editorModel = newEditorModel(m.client, oe.noteID, m.width, m.height)
		return m, m.editorModel.Init()
	}

	var cmd tea.Cmd
	m.searchModel, cmd = m.searchModel.Update(msg)

	if m.searchModel.done {
		m.screen = screenMain
		return m, nil
	}

	return m, cmd
}

func (m appModel) updateAsk(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.askModel, cmd = m.askModel.Update(msg)

	if m.askModel.done {
		m.screen = screenMain
		return m, nil
	}

	return m, cmd
}

func (m appModel) View() string {
	switch m.screen {
	case screenLogin:
		return m.loginModel.View()
	case screenMain:
		return m.mainModel.View()
	case screenEditor:
		return m.editorModel.View()
	case screenSearch:
		return m.searchModel.View()
	case screenAsk:
		return m.askModel.View()
	default:
		return ""
	}
}
