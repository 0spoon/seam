package main

import (
	tea "charm.land/bubbletea/v2"
)

// screen identifies which screen is currently active.
type screen int

const (
	screenLogin screen = iota
	screenMain
	screenEditor
	screenSearch
	screenAsk
	screenTimeline
	screenSettings
)

// appModel is the root Bubble Tea model that delegates to sub-models.
type appModel struct {
	screen        screen
	client        *APIClient
	username      string
	width         int
	height        int
	loginModel    loginModel
	mainModel     mainScreenModel
	editorModel   editorModel
	searchModel   searchModel
	askModel      askModel
	timelineModel timelineModel
	settingsModel settingsModel
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

	// Handle global quit and logout.
	if km, ok := msg.(tea.KeyPressMsg); ok {
		// Ctrl+C is the emergency quit escape hatch and is never
		// rebindable, so it bypasses the keymap entirely.
		if km.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.screen != screenLogin && currentKeymap().Matches(km, ActionGlobalLogout) {
			_ = SaveAuth(&AuthData{})
			m.client.AccessToken = ""
			m.client.RefreshToken = ""
			m.username = ""
			m.screen = screenLogin
			m.loginModel = newLoginModel(m.client)
			return m, m.loginModel.Init()
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
	case screenTimeline:
		return m.updateTimeline(msg)
	case screenSettings:
		return m.updateSettings(msg)
	}

	return m, nil
}

func (m appModel) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.settingsModel, cmd = m.settingsModel.Update(msg)
	if m.settingsModel.saved {
		m.screen = screenMain
	}
	return m, cmd
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

	case openTimelineMsg:
		m.screen = screenTimeline
		m.timelineModel = newTimelineModel(m.client, m.width, m.height)
		return m, m.timelineModel.Init()

	case openSettingsMsg:
		m.screen = screenSettings
		m.settingsModel = newSettingsModel(m.width, m.height)
		return m, m.settingsModel.Init()
	}

	var cmd tea.Cmd
	m.mainModel, cmd = m.mainModel.Update(msg)
	return m, cmd
}

func (m appModel) updateEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m appModel) updateTimeline(msg tea.Msg) (tea.Model, tea.Cmd) {
	// If timeline triggers opening a note, switch to editor.
	if oe, ok := msg.(openEditorMsg); ok {
		m.screen = screenEditor
		m.editorModel = newEditorModel(m.client, oe.noteID, m.width, m.height)
		return m, m.editorModel.Init()
	}

	var cmd tea.Cmd
	m.timelineModel, cmd = m.timelineModel.Update(msg)

	if m.timelineModel.done {
		m.screen = screenMain
		return m, nil
	}

	return m, cmd
}

func (m appModel) View() tea.View {
	var content string
	switch m.screen {
	case screenLogin:
		content = m.loginModel.View()
	case screenMain:
		content = m.mainModel.View()
	case screenEditor:
		content = m.editorModel.View()
	case screenSearch:
		content = m.searchModel.View()
	case screenAsk:
		content = m.askModel.View()
	case screenTimeline:
		content = m.timelineModel.View()
	case screenSettings:
		content = m.settingsModel.View()
	}

	v := tea.NewView(content)
	// Run in the alternate screen buffer (moved from tea.NewProgram options in v2).
	v.AltScreen = true
	// Enable mouse cell motion so mouse wheel events reach Update. The
	// Ask Seam chat uses them to scroll the conversation viewport; all
	// other screens ignore mouse messages and behave as before.
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
