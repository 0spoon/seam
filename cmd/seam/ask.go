package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/coder/websocket"
)

// openAskMsg triggers switching to the Ask Seam screen.
type openAskMsg struct{}

// askResultMsg delivers a complete (non-streaming) chat response.
type askResultMsg struct {
	response  string
	citations []string
}

// askStreamTokenMsg delivers a single streaming token.
type askStreamTokenMsg struct {
	token string
}

// askStreamDoneMsg signals that streaming is complete.
type askStreamDoneMsg struct {
	citations []string
}

// askStreamErrMsg signals a streaming error.
type askStreamErrMsg struct {
	err error
}

// chatEntry is a single message in the conversation.
type chatEntry struct {
	role    string // "user" or "assistant"
	content string
}

// askModel handles the Ask Seam chat screen.
type askModel struct {
	client  *APIClient
	input   textarea.Model
	history []chatEntry
	apiHist []ChatMessage
	loading bool
	done    bool
	err     string
	width   int
	height  int
	scrollY int // scroll offset for the conversation view

	// Streaming state.
	streaming        bool
	streamingContent string
}

func newAskModel(client *APIClient, width, height int) askModel {
	ta := textarea.New()
	ta.Placeholder = "Ask about your notes..."
	ta.CharLimit = 1000
	ta.MaxHeight = 4
	ta.ShowLineNumbers = false
	ta.Focus()

	if width > 10 {
		ta.SetWidth(width - 4)
	}

	return askModel{
		client: client,
		input:  ta,
		width:  width,
		height: height,
	}
}

func (m askModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m askModel) Update(msg tea.Msg) (askModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(msg.Width - 4)
		return m, nil

	case askStreamTokenMsg:
		m.streamingContent += msg.token
		m.scrollY = m.maxScroll()
		return m, nil

	case askStreamDoneMsg:
		m.loading = false
		m.streaming = false
		content := m.streamingContent
		m.streamingContent = ""
		// Add citation info if present.
		if len(msg.citations) > 0 {
			citeStr := "Sources: " + strings.Join(msg.citations, ", ")
			content += "\n\n" + citeStr
		}
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: content,
		})
		m.apiHist = append(m.apiHist, ChatMessage{
			Role:    "assistant",
			Content: content,
		})
		m.scrollY = m.maxScroll()
		return m, nil

	case askStreamErrMsg:
		m.loading = false
		m.streaming = false
		m.streamingContent = ""
		m.err = msg.err.Error()
		return m, nil

	case askResultMsg:
		// Fallback for non-streaming path.
		m.loading = false
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: msg.response,
		})
		m.apiHist = append(m.apiHist, ChatMessage{
			Role:    "assistant",
			Content: msg.response,
		})
		if len(msg.citations) > 0 {
			citeStr := "Sources: " + strings.Join(msg.citations, ", ")
			m.history[len(m.history)-1].content += "\n\n" + citeStr
		}
		m.scrollY = m.maxScroll()
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

		case "ctrl+up":
			if m.scrollY > 0 {
				m.scrollY--
			}
			return m, nil

		case "ctrl+down":
			max := m.maxScroll()
			if m.scrollY < max {
				m.scrollY++
			}
			return m, nil

		case "enter":
			if m.loading {
				return m, nil
			}
			query := strings.TrimSpace(m.input.Value())
			if query == "" {
				return m, nil
			}

			// Add user message to history.
			m.history = append(m.history, chatEntry{
				role:    "user",
				content: query,
			})
			m.apiHist = append(m.apiHist, ChatMessage{
				Role:    "user",
				Content: query,
			})
			m.input.Reset()
			m.loading = true
			m.streaming = true
			m.streamingContent = ""
			m.scrollY = m.maxScroll()

			client := m.client
			apiHist := make([]ChatMessage, len(m.apiHist))
			copy(apiHist, m.apiHist)
			return m, askViaWebSocket(client, query, apiHist[:len(apiHist)-1])
		}
	}

	// Update textarea input.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// askViaWebSocket connects to the WS endpoint and streams the response
// back as Bubble Tea messages.
func askViaWebSocket(client *APIClient, query string, history []ChatMessage) tea.Cmd {
	return func() tea.Msg {
		// Build WS URL from the REST base URL.
		u, err := url.Parse(client.BaseURL)
		if err != nil {
			// Fall back to sync HTTP on URL parse failure.
			return askFallbackHTTP(client, query, history)
		}

		scheme := "ws"
		if u.Scheme == "https" {
			scheme = "wss"
		}
		wsURL := fmt.Sprintf("%s://%s/api/ws", scheme, u.Host)

		ctx := context.Background()
		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			// Fall back to sync HTTP if WS connection fails.
			return askFallbackHTTP(client, query, history)
		}
		defer conn.CloseNow()

		// Authenticate.
		authMsg, _ := json.Marshal(map[string]interface{}{
			"type":    "auth",
			"payload": map[string]string{"token": client.AccessToken},
		})
		if err := conn.Write(ctx, websocket.MessageText, authMsg); err != nil {
			return askFallbackHTTP(client, query, history)
		}

		// Send chat.ask message.
		askPayload := map[string]interface{}{
			"query": query,
		}
		if len(history) > 0 {
			askPayload["history"] = history
		}
		chatMsg, _ := json.Marshal(map[string]interface{}{
			"type":    "chat.ask",
			"payload": askPayload,
		})
		if err := conn.Write(ctx, websocket.MessageText, chatMsg); err != nil {
			return askFallbackHTTP(client, query, history)
		}

		// Read streaming responses. We accumulate the response here since
		// we cannot send multiple tea.Msg from a single Cmd. Instead, we
		// collect the full response and send it once (same latency as before
		// for initial display, but uses the streaming infrastructure).
		var fullContent strings.Builder
		var citations []string

		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				if fullContent.Len() > 0 {
					// We got some content before error; return what we have.
					break
				}
				return askFallbackHTTP(client, query, history)
			}

			var msg struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			switch msg.Type {
			case "chat.stream":
				var streamPayload struct {
					Token string `json:"token"`
				}
				json.Unmarshal(msg.Payload, &streamPayload)
				fullContent.WriteString(streamPayload.Token)

			case "chat.done":
				var donePayload struct {
					Citations []string `json:"citations"`
				}
				json.Unmarshal(msg.Payload, &donePayload)
				citations = donePayload.Citations
				conn.Close(websocket.StatusNormalClosure, "done")
				return askResultMsg{
					response:  fullContent.String(),
					citations: citations,
				}
			}
		}

		return askResultMsg{
			response:  fullContent.String(),
			citations: citations,
		}
	}
}

// askFallbackHTTP falls back to synchronous HTTP when WS is unavailable.
func askFallbackHTTP(client *APIClient, query string, history []ChatMessage) tea.Msg {
	result, err := client.AskSeam(query, history)
	if err != nil {
		return apiErrorMsg{err: err}
	}
	return askResultMsg{
		response:  result.Response,
		citations: result.Citations,
	}
}

func (m askModel) maxScroll() int {
	contentLines := m.renderConversationLines()
	viewportHeight := m.height - 8 // header + input + status
	if viewportHeight < 3 {
		viewportHeight = 3
	}
	max := contentLines - viewportHeight
	if max < 0 {
		max = 0
	}
	return max
}

func (m askModel) renderConversationLines() int {
	count := 0
	for _, entry := range m.history {
		// Role label line + empty line.
		count++
		lines := strings.Split(entry.content, "\n")
		count += len(lines)
		count++ // spacing
	}
	if m.loading {
		count += 2
	}
	if m.streaming && m.streamingContent != "" {
		count++ // "Seam:" label
		lines := strings.Split(m.streamingContent, "\n")
		count += len(lines)
		count++ // spacing
	}
	return count
}

func (m askModel) View() string {
	if m.width == 0 {
		return ""
	}

	// Header.
	header := styleHeader.Width(m.width).Render(" Ask Seam")

	// Conversation area.
	var lines []string
	wrapWidth := m.width - 6
	if wrapWidth < 20 {
		wrapWidth = 20
	}

	if len(m.history) == 0 && !m.loading {
		lines = append(lines, "")
		lines = append(lines, styleMuted.Render("  Ask a question about your notes. Answers are grounded in your knowledge base."))
		lines = append(lines, "")
	}

	for _, entry := range m.history {
		if entry.role == "user" {
			lines = append(lines, styleTitle.Render("  You:"))
		} else {
			lines = append(lines, styleSuccess.Render("  Seam:"))
		}

		// Wrap long lines.
		for _, line := range strings.Split(entry.content, "\n") {
			wrapped := wrapText(line, wrapWidth)
			for _, wl := range strings.Split(wrapped, "\n") {
				lines = append(lines, "    "+wl)
			}
		}
		lines = append(lines, "")
	}

	// Show streaming content as it arrives.
	if m.streaming && m.streamingContent != "" {
		lines = append(lines, styleSuccess.Render("  Seam:"))
		for _, line := range strings.Split(m.streamingContent, "\n") {
			wrapped := wrapText(line, wrapWidth)
			for _, wl := range strings.Split(wrapped, "\n") {
				lines = append(lines, "    "+wl)
			}
		}
		lines = append(lines, "")
	}

	if m.loading && !m.streaming {
		lines = append(lines, styleMuted.Render("  Thinking..."))
		lines = append(lines, "")
	} else if m.loading && m.streaming && m.streamingContent == "" {
		lines = append(lines, styleMuted.Render("  Thinking..."))
		lines = append(lines, "")
	}

	// Apply scrolling.
	viewportHeight := m.height - 8
	if viewportHeight < 3 {
		viewportHeight = 3
	}

	start := m.scrollY
	if start > len(lines) {
		start = len(lines)
	}
	end := start + viewportHeight
	if end > len(lines) {
		end = len(lines)
	}

	visibleLines := lines[start:end]

	// Pad to fill viewport.
	for len(visibleLines) < viewportHeight {
		visibleLines = append(visibleLines, "")
	}

	conversation := strings.Join(visibleLines, "\n")

	// Error display.
	if m.err != "" {
		conversation += "\n" + styleError.Render("  "+m.err)
	}

	// Input area.
	inputSection := "\n " + m.input.View() + "\n"

	// Status bar.
	statusBar := styleStatusBar.Width(m.width).Render(
		"Enter: send | Ctrl+Up/Down: scroll | Esc: back",
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		conversation,
		inputSection,
		statusBar,
	)
}

// wrapText wraps a string at the given width, breaking on spaces.
func wrapText(s string, width int) string {
	if width <= 0 || len(s) <= width {
		return s
	}

	var lines []string
	for len(s) > width {
		// Find last space before width.
		breakAt := strings.LastIndex(s[:width], " ")
		if breakAt <= 0 {
			breakAt = width
		}
		lines = append(lines, s[:breakAt])
		s = strings.TrimLeft(s[breakAt:], " ")
	}
	if s != "" {
		lines = append(lines, s)
	}
	return strings.Join(lines, "\n")
}
