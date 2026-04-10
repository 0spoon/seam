package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// openAskMsg triggers switching to the Ask Seam screen.
type openAskMsg struct{}

// -- Stream message types ----------------------------------------------------

// assistantStreamStartMsg carries the first streamed event and the
// channel the remaining events will arrive on.
type assistantStreamStartMsg struct {
	first tea.Msg
	ch    <-chan tea.Msg
}

// assistantStreamEventMsg wraps a single SSE event from the assistant.
type assistantStreamEventMsg struct {
	event AssistantStreamEvent
}

// assistantStreamErrMsg reports a stream-level error.
type assistantStreamErrMsg struct {
	err error
}

// assistantStreamDoneMsg signals a graceful stream close.
type assistantStreamDoneMsg struct{}

// assistantConvCreatedMsg delivers the conversation id that was created
// lazily on first submit. It also carries the pending message to submit
// immediately after creation.
type assistantConvCreatedMsg struct {
	id      string
	pending string
}

// assistantConvErrMsg reports a conversation creation error.
type assistantConvErrMsg struct {
	err error
}

// assistantRejectedMsg signals the reject endpoint completed.
type assistantRejectedMsg struct {
	err error
}

// assistantReloadMsg delivers a fresh canonical conversation snapshot
// after a resume completes. The TUI replaces its local turns with the
// persisted history so the visible state matches the server.
type assistantReloadMsg struct {
	turns []chatTurn
	err   error
}

// -- Model state -------------------------------------------------------------

// chatTurn is one rendered row in the chat view. It can be a user
// message, an assistant text reply, a tool card, or a system note.
type chatTurn struct {
	kind     string // "user" | "assistant" | "tool" | "system" | "stream-tool"
	content  string
	toolName string
	status   string // "running" | "ok" | "error" -- for tool kinds
	raw      json.RawMessage
}

// confirmationPrompt holds the tool action awaiting user approval.
type confirmationPrompt struct {
	actionID string
	toolName string
}

// askModel handles the Ask Seam assistant chat screen.
type askModel struct {
	client *APIClient
	input  textarea.Model

	conversationID string
	turns          []chatTurn
	streamingText  string
	streaming      bool
	streamCh       <-chan tea.Msg
	streamCancel   context.CancelFunc

	pendingConfirm *confirmationPrompt
	err            string

	width, height int
	scrollY       int
	focusToolIdx  int          // index into turns of the focused tool card, -1 for none
	expanded      map[int]bool // turn index -> expanded state

	done bool
}

// newAskModel constructs a fresh assistant screen. The conversation row
// is created lazily on first submit.
func newAskModel(client *APIClient, width, height int) askModel {
	ta := textarea.New()
	ta.Placeholder = "Ask Seam anything..."
	ta.CharLimit = 4000
	ta.MaxHeight = 4
	ta.ShowLineNumbers = false
	ta.SetHeight(4)
	ta.Focus()

	if width > 10 {
		ta.SetWidth(width - 6)
	}

	return askModel{
		client:       client,
		input:        ta,
		width:        width,
		height:       height,
		focusToolIdx: -1,
		expanded:     make(map[int]bool),
	}
}

func (m askModel) Init() tea.Cmd {
	return textarea.Blink
}

// -- Update ------------------------------------------------------------------

func (m askModel) Update(msg tea.Msg) (askModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(msg.Width - 6)
		return m, nil

	case assistantConvCreatedMsg:
		m.conversationID = msg.id
		return m.startStream(msg.pending)

	case assistantConvErrMsg:
		m.err = msg.err.Error()
		return m, nil

	case assistantStreamStartMsg:
		// The background goroutine is feeding events into msg.ch. We
		// store the channel and dispatch the first message recursively.
		// The recursive handler will re-arm the waiter; we must NOT spawn
		// a second reader here or events race and arrive out of order.
		m.streamCh = msg.ch
		return m.Update(msg.first)

	case assistantStreamEventMsg:
		m = m.handleStreamEvent(msg.event)
		if m.streamCh != nil {
			return m, waitForAssistantStreamMsg(m.streamCh)
		}
		return m, nil

	case assistantStreamErrMsg:
		m.streaming = false
		m.streamCh = nil
		m.streamCancel = nil
		if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
			m.err = msg.err.Error()
		}
		return m, nil

	case assistantStreamDoneMsg:
		m.streaming = false
		m.streamCh = nil
		m.streamCancel = nil
		return m, nil

	case assistantReloadMsg:
		if msg.err != nil {
			// Non-fatal: streaming scratch state already shows the
			// result. Keep the local turn list as-is.
			m.err = msg.err.Error()
			return m, nil
		}
		if msg.turns != nil {
			m.turns = msg.turns
			m.scrollY = m.maxScroll()
		}
		return m, nil

	case assistantRejectedMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.turns = append(m.turns, chatTurn{
			kind:    "system",
			content: "Action rejected",
		})
		return m, nil

	case apiErrorMsg:
		m.err = msg.err.Error()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	// Let the textarea handle everything else (cursor, paste, etc.).
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleKey dispatches key presses.
func (m askModel) handleKey(msg tea.KeyPressMsg) (askModel, tea.Cmd) {
	m.err = ""
	key := msg.String()

	switch key {
	case "esc":
		// Dismiss a pending confirmation first.
		if m.pendingConfirm != nil {
			m.pendingConfirm = nil
			return m, nil
		}
		// Cancel an in-flight stream before backing out.
		if m.streamCancel != nil {
			m.streamCancel()
			m.streamCancel = nil
			m.streaming = false
			return m, nil
		}
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

	case "tab":
		m.focusToolIdx = m.nextToolIdx(m.focusToolIdx, +1)
		return m, nil

	case "shift+tab":
		m.focusToolIdx = m.nextToolIdx(m.focusToolIdx, -1)
		return m, nil
	}

	// Pending confirmation shortcuts.
	if m.pendingConfirm != nil {
		switch key {
		case "a":
			actionID := m.pendingConfirm.actionID
			m.pendingConfirm = nil
			return m.startResumeStream(actionID)
		case "r":
			actionID := m.pendingConfirm.actionID
			m.pendingConfirm = nil
			return m, m.rejectAction(actionID)
		}
		return m, nil
	}

	// Enter -- submit or expand.
	if key == "enter" {
		// If a tool card is focused, toggle expansion instead of submitting.
		if m.focusToolIdx >= 0 && m.focusToolIdx < len(m.turns) {
			t := m.turns[m.focusToolIdx]
			if t.kind == "tool" || t.kind == "stream-tool" {
				m.expanded[m.focusToolIdx] = !m.expanded[m.focusToolIdx]
				return m, nil
			}
		}
		if m.streaming {
			return m, nil
		}
		query := strings.TrimSpace(m.input.Value())
		if query == "" {
			return m, nil
		}
		m.input.Reset()

		if m.conversationID == "" {
			return m, m.createConversationThenSend(query)
		}
		return m.startStream(query)
	}

	if key == "shift+enter" {
		// Shift+Enter inserts a newline. Requires Kitty keyboard protocol
		// for disambiguation from plain Enter; on terminals without it
		// this falls through to submit.
		m.input.InsertRune('\n')
		return m, nil
	}

	// Pass through to textarea.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// nextToolIdx advances focus to the next tool turn in the given direction.
// Returns -1 if no tool turn is present.
func (m askModel) nextToolIdx(current, step int) int {
	// Find all tool turn indices.
	var toolIdxs []int
	for i, t := range m.turns {
		if t.kind == "tool" || t.kind == "stream-tool" {
			toolIdxs = append(toolIdxs, i)
		}
	}
	if len(toolIdxs) == 0 {
		return -1
	}
	// Find the position of current in toolIdxs.
	pos := -1
	for i, idx := range toolIdxs {
		if idx == current {
			pos = i
			break
		}
	}
	if pos == -1 {
		if step > 0 {
			return toolIdxs[0]
		}
		return toolIdxs[len(toolIdxs)-1]
	}
	pos = (pos + step + len(toolIdxs)) % len(toolIdxs)
	return toolIdxs[pos]
}

// handleStreamEvent mutates the model in response to one SSE event.
func (m askModel) handleStreamEvent(e AssistantStreamEvent) askModel {
	switch e.Type {
	case "text":
		// Text arrives as one final blob (the inner LLM call is
		// non-streaming). We convert it to a turn on "done".
		m.streamingText = e.Content

	case "tool_use":
		status := "ok"
		if e.Error != "" {
			status = "error"
		}
		raw := json.RawMessage(e.Content)
		if status == "error" {
			raw = json.RawMessage(fmt.Sprintf("%q", e.Error))
		}
		m.turns = append(m.turns, chatTurn{
			kind:     "stream-tool",
			toolName: e.ToolName,
			status:   status,
			raw:      raw,
		})

	case "confirmation":
		m.pendingConfirm = &confirmationPrompt{
			actionID: e.Content,
			toolName: e.ToolName,
		}

	case "done":
		if m.streamingText != "" {
			m.turns = append(m.turns, chatTurn{
				kind:    "assistant",
				content: m.streamingText,
			})
			m.streamingText = ""
		}
		m.streaming = false
		m.streamCancel = nil

	case "error":
		m.err = e.Error
		m.streaming = false
		m.streamingText = ""
	}

	m.scrollY = m.maxScroll()
	return m
}

// -- Commands ----------------------------------------------------------------

// createConversationThenSend lazily creates a conversation row and then
// kicks off the first stream.
func (m askModel) createConversationThenSend(query string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		conv, err := client.CreateAssistantConversation(context.Background())
		if err != nil {
			return assistantConvErrMsg{err: err}
		}
		return assistantConvCreatedMsg{id: conv.ID, pending: query}
	}
}

// startStream appends the user turn, spawns the stream goroutine, and
// returns a Cmd that reads the first event from the channel.
func (m askModel) startStream(query string) (askModel, tea.Cmd) {
	m.turns = append(m.turns, chatTurn{kind: "user", content: query})
	m.streamingText = ""
	m.streaming = true
	m.scrollY = m.maxScroll()

	history := buildHistory(m.turns[:len(m.turns)-1])

	client := m.client
	convID := m.conversationID

	ctx, cancel := context.WithCancel(context.Background())
	m.streamCancel = cancel

	ch := make(chan tea.Msg, 64)
	go func() {
		defer close(ch)
		err := client.AssistantChatStream(ctx, convID, query, history, func(ev AssistantStreamEvent) {
			// Non-blocking dispatch is fine because ch is buffered and
			// the Update loop drains it. If the buffer fills, the SSE
			// reader will block briefly -- acceptable backpressure.
			select {
			case ch <- assistantStreamEventMsg{event: ev}:
			case <-ctx.Done():
			}
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			ch <- assistantStreamErrMsg{err: err}
			return
		}
		ch <- assistantStreamDoneMsg{}
	}()

	cmd := func() tea.Msg {
		first, ok := <-ch
		if !ok {
			return assistantStreamDoneMsg{}
		}
		return assistantStreamStartMsg{first: first, ch: ch}
	}
	return m, cmd
}

// startResumeStream spawns the resume SSE stream after the user
// approves a pending tool action. It uses the same channel pattern as
// startStream so the existing assistantStreamEventMsg / Done / Err
// handling drives the UI state machine. After the stream closes
// cleanly, the model schedules a reload to reconcile local turns with
// the canonical persisted history.
func (m askModel) startResumeStream(actionID string) (askModel, tea.Cmd) {
	m.streamingText = ""
	m.streaming = true
	m.scrollY = m.maxScroll()

	client := m.client
	convID := m.conversationID

	ctx, cancel := context.WithCancel(context.Background())
	m.streamCancel = cancel

	ch := make(chan tea.Msg, 64)
	go func() {
		defer close(ch)
		err := client.AssistantResumeStream(ctx, actionID, func(ev AssistantStreamEvent) {
			select {
			case ch <- assistantStreamEventMsg{event: ev}:
			case <-ctx.Done():
			}
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			ch <- assistantStreamErrMsg{err: err}
			return
		}
		// Stream finished cleanly. Reload the canonical conversation
		// from the server so the visible turns match what was
		// persisted (including the assistant envelope, the tool
		// result, and the final assistant text). The reload runs
		// inside the goroutine so it shares the same cancel context.
		conv, msgs, reloadErr := client.GetAssistantConversation(ctx, convID)
		_ = conv
		ch <- assistantReloadMsg{
			turns: persistedToTurns(msgs),
			err:   reloadErr,
		}
		ch <- assistantStreamDoneMsg{}
	}()

	cmd := func() tea.Msg {
		first, ok := <-ch
		if !ok {
			return assistantStreamDoneMsg{}
		}
		return assistantStreamStartMsg{first: first, ch: ch}
	}
	return m, cmd
}

// rejectAction returns a Cmd that calls the reject endpoint and wraps
// the outcome in an assistantRejectedMsg.
func (m askModel) rejectAction(actionID string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		err := client.RejectAssistantAction(context.Background(), actionID)
		return assistantRejectedMsg{err: err}
	}
}

// waitForAssistantStreamMsg reads the next message from the channel.
func waitForAssistantStreamMsg(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return assistantStreamDoneMsg{}
		}
		return msg
	}
}

// buildHistory converts internal turns into the API history format.
// Only user and assistant text turns are included -- the server reads
// the canonical history (assistant tool_call envelopes, tool results,
// system markers) directly from chat.Store via the persisted conversation
// row, so the client only needs to send the bits the server can't
// reconstruct from its own state (the in-flight user message context).
func buildHistory(turns []chatTurn) []AssistantHistoryMessage {
	var out []AssistantHistoryMessage
	for _, t := range turns {
		switch t.kind {
		case "user":
			out = append(out, AssistantHistoryMessage{Role: "user", Content: t.content})
		case "assistant":
			out = append(out, AssistantHistoryMessage{Role: "assistant", Content: t.content})
		}
		// tool / stream-tool / system turns are display-only.
	}
	return out
}

// persistedToTurns converts canonical persisted chat messages from the
// server into the local chatTurn slice the TUI renders. system rows
// are dropped (audit markers, not real conversation), assistant rows
// with tool_calls fall back to a "(used N tools)" placeholder so the
// chronology stays intact, and tool rows render as tool cards.
func persistedToTurns(msgs []AssistantPersistedMessage) []chatTurn {
	var out []chatTurn
	for _, m := range msgs {
		switch m.Role {
		case "user":
			out = append(out, chatTurn{kind: "user", content: m.Content})
		case "assistant":
			if len(m.ToolCalls) > 0 && m.Content == "" {
				// The envelope row carries no body text; the
				// individual tool result rows that follow will
				// render the visible cards.
				continue
			}
			out = append(out, chatTurn{kind: "assistant", content: m.Content})
		case "tool":
			status := "ok"
			if strings.HasPrefix(m.Content, "Error:") {
				status = "error"
			}
			out = append(out, chatTurn{
				kind:     "tool",
				toolName: m.ToolName,
				status:   status,
				raw:      json.RawMessage(m.Content),
			})
		case "system":
			// Audit marker -- skip.
		}
	}
	return out
}

// -- View --------------------------------------------------------------------

func (m askModel) View() string {
	if m.width == 0 {
		return ""
	}

	header := assistantStyles.Header.Width(m.width).Render(" " + marioBlock + "  ASK SEAM")

	lines := m.buildConversationLines()

	viewportHeight := m.height - 10
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

	visible := lines[start:end]
	for len(visible) < viewportHeight {
		visible = append(visible, "")
	}
	conversation := strings.Join(visible, "\n")

	if m.err != "" {
		conversation += "\n" + assistantStyles.Error.Render("  "+m.err)
	}

	// Confirmation prompt replaces the input when pending.
	var bottom string
	if m.pendingConfirm != nil {
		bottom = m.renderConfirmPrompt()
	} else {
		bottom = "\n " + m.input.View() + "\n"
	}

	statusBar := assistantStyles.StatusBar.
		Width(m.width).
		Render("Enter: send | Shift+Enter: newline | Tab/Shift+Tab: focus tool | Enter (on tool): expand | Ctrl+Up/Down: scroll | Esc: stop/back")

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		conversation,
		bottom,
		statusBar,
	)
}

// buildConversationLines flattens turns into display lines with styling.
func (m askModel) buildConversationLines() []string {
	var lines []string
	wrapWidth := m.width - 8
	if wrapWidth < 20 {
		wrapWidth = 20
	}

	if len(m.turns) == 0 && !m.streaming {
		lines = append(lines, "")
		lines = append(lines, assistantStyles.Muted.Render("  "+marioBlock+"  Ask the assistant anything -- it can search, read, write, and remember."))
		lines = append(lines, "")
	}

	for i, t := range m.turns {
		focused := i == m.focusToolIdx
		switch t.kind {
		case "user":
			lines = append(lines, assistantStyles.MessageUser.Render("  You: "))
			for _, line := range strings.Split(t.content, "\n") {
				wrapped := wrapText(line, wrapWidth)
				for _, wl := range strings.Split(wrapped, "\n") {
					lines = append(lines, "    "+assistantStyles.MessageAssist.Render(wl))
				}
			}
			lines = append(lines, "")

		case "assistant":
			lines = append(lines, assistantStyles.ToolBlock.Render("  Seam: "))
			for _, line := range strings.Split(t.content, "\n") {
				wrapped := wrapText(line, wrapWidth)
				for _, wl := range strings.Split(wrapped, "\n") {
					lines = append(lines, "    "+assistantStyles.MessageAssist.Render(wl))
				}
			}
			lines = append(lines, "")

		case "system":
			lines = append(lines, assistantStyles.Muted.Render("  · "+t.content))
			lines = append(lines, "")

		case "tool", "stream-tool":
			lines = append(lines, m.renderToolCard(i, t, focused, wrapWidth)...)
			lines = append(lines, "")
		}
	}

	// Streaming text bubble.
	if m.streaming && m.streamingText != "" {
		lines = append(lines, assistantStyles.ToolBlock.Render("  Seam: "))
		for _, line := range strings.Split(m.streamingText, "\n") {
			wrapped := wrapText(line, wrapWidth)
			for _, wl := range strings.Split(wrapped, "\n") {
				lines = append(lines, "    "+assistantStyles.MessageAssist.Render(wl))
			}
		}
		lines = append(lines, "")
	} else if m.streaming && m.streamingText == "" {
		lines = append(lines, assistantStyles.Muted.Render("  "+marioBlock+" thinking..."))
		lines = append(lines, "")
	}

	return lines
}

// renderToolCard formats a single tool turn as one or more lines.
func (m askModel) renderToolCard(idx int, t chatTurn, focused bool, wrapWidth int) []string {
	cursor := "  "
	nameStyle := assistantStyles.ToolBlock
	if focused {
		cursor = assistantStyles.ToolBlock.Render("> ")
		nameStyle = assistantStyles.ToolBlock.Bold(true).Underline(true)
	}
	glyph := assistantStatusStyle(t.status).Render(marioStatusGlyph(t.status))
	head := cursor + assistantStyles.ToolBlock.Render(marioBlock+" ") + nameStyle.Render(t.toolName) + "  " + glyph

	lines := []string{head}
	if m.expanded[idx] {
		body := renderToolResult(t.toolName, t.raw)
		for _, bl := range strings.Split(body, "\n") {
			// Indent with three spaces to line up under the block glyph.
			lines = append(lines, "   "+bl)
		}
	}
	return lines
}

// renderConfirmPrompt builds the gold-framed approval block.
func (m askModel) renderConfirmPrompt() string {
	innerWidth := m.width - 6
	if innerWidth < 40 {
		innerWidth = 40
	}
	body := []string{
		assistantStyles.MessageAssist.Render("The assistant wants to use ") + assistantStyles.ToolBlock.Render(m.pendingConfirm.toolName) + assistantStyles.MessageAssist.Render("."),
		assistantStyles.Muted.Render("Action ID: " + m.pendingConfirm.actionID),
		"",
		assistantStyles.ToolStatusOk.Render("[a] Approve") + "     " +
			assistantStyles.ToolStatusErr.Render("[r] Reject") + "     " +
			assistantStyles.Muted.Render("[Esc] Cancel"),
	}
	title := assistantStyles.ToolBlock.Render(" " + marioBlock + "  Action required ")
	joined := lipgloss.JoinVertical(lipgloss.Left, title, "") + "\n" + strings.Join(body, "\n")
	return "\n" + assistantStyles.ConfirmPane.Width(innerWidth).Render(joined) + "\n"
}

// -- Scroll helpers ----------------------------------------------------------

func (m askModel) maxScroll() int {
	contentLines := len(m.buildConversationLines())
	viewportHeight := m.height - 10
	if viewportHeight < 3 {
		viewportHeight = 3
	}
	max := contentLines - viewportHeight
	if max < 0 {
		max = 0
	}
	return max
}

// wrapText wraps a string at the given width (in runes), breaking on spaces.
func wrapText(s string, width int) string {
	runes := []rune(s)
	if width <= 0 || len(runes) <= width {
		return s
	}

	var lines []string
	for len(runes) > width {
		segment := string(runes[:width])
		breakAt := strings.LastIndex(segment, " ")
		if breakAt <= 0 {
			breakAt = width
		}
		lines = append(lines, string(runes[:breakAt]))
		runes = []rune(strings.TrimLeft(string(runes[breakAt:]), " "))
	}
	if len(runes) > 0 {
		lines = append(lines, string(runes))
	}
	return strings.Join(lines, "\n")
}
