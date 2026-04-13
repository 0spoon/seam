package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"
)

// openAskMsg triggers switching to the Ask Seam screen.
type openAskMsg struct{}

// marioTickMsg advances the Mario chat-icon animation by one frame.
// The tick only runs while the Ask screen is open AND the Mario
// assistant theme is active; both conditions are rechecked inside the
// Update handler so a theme toggle mid-conversation cleanly stops the
// loop without leaking a stale goroutine.
type marioTickMsg struct{}

// marioTickInterval is the delay between Mario walk-cycle frames in
// the "thinking..." indicator. Two frames at 320ms give a natural
// walking cadence -- fast enough to read as "moving", slow enough not
// to distract from the surrounding chat copy.
const marioTickInterval = 320 * time.Millisecond

// marioTick returns a bubbletea Cmd that fires one marioTickMsg after
// marioTickInterval. The Update handler enqueues a fresh tick after
// each frame, so the animation is a self-sustaining ping-pong loop
// that terminates when askModel exits or the theme changes.
func marioTick() tea.Cmd {
	return tea.Tick(marioTickInterval, func(time.Time) tea.Msg {
		return marioTickMsg{}
	})
}

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
	kind      string // "user" | "assistant" | "tool" | "system" | "stream-tool"
	content   string
	toolName  string
	status    string // "running" | "ok" | "error" -- for tool kinds
	raw       json.RawMessage
	createdAt time.Time
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

	// animFrame drives the Mario-theme chat icon animation. Non-Mario
	// themes leave it at zero; the icon renderer simply ignores it.
	animFrame int

	done bool
}

// newAskModel constructs a fresh assistant screen. The conversation row
// is created lazily on first submit.
func newAskModel(client *APIClient, width, height int) askModel {
	ta := textarea.New()
	ta.Placeholder = "Ask Seam anything..."
	ta.CharLimit = 4000
	ta.MaxHeight = askInputHeight
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.SetHeight(askInputHeight)
	styleAskTextarea(&ta)
	ta.Focus()

	if width > askInputChromeWidth {
		ta.SetWidth(width - askInputChromeWidth)
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

// askInputHeight is the fixed row count of the Ask Seam textarea. Four
// lines is enough for most prompts and still leaves room for the status
// bar and conversation viewport.
const askInputHeight = 4

// askInputChromeWidth is the total horizontal chrome around the textarea
// that is subtracted from the terminal width when sizing the textarea
// body. It accounts for the outer margin (4 cols), the bordered input
// box (2 cols), and the inner padding (2 cols).
const askInputChromeWidth = 8

// askInputRows is the fixed vertical budget of the Ask Seam footer
// when the textarea is active. It accounts for the textarea itself
// (askInputHeight), the bordered input box (2 rows), and one blank
// row above and below the box (2 rows) so the border does not butt
// directly against the conversation viewport or the status bar.
const askInputRows = askInputHeight + 4

// styleAskTextarea applies theme-aware colors to the Ask Seam input so
// it blends with the assistant pane. The default bubbles v2 textarea
// uses a thick border prompt and a hard-coded cursor-line background
// that clash with the Mario / Catppuccin palettes, so we rebuild the
// style state from scratch using the active assistant colors.
func styleAskTextarea(ta *textarea.Model) {
	s := ta.Styles()
	fg := assistantStyles.MessageAssist.GetForeground()
	placeholder := assistantStyles.Muted.GetForeground()
	base := lipgloss.NewStyle()
	text := lipgloss.NewStyle().Foreground(fg)

	s.Focused.Base = base
	s.Focused.Text = text
	s.Focused.CursorLine = text
	s.Focused.CursorLineNumber = base
	s.Focused.Placeholder = lipgloss.NewStyle().Foreground(placeholder)
	s.Focused.Prompt = base
	s.Focused.EndOfBuffer = base

	s.Blurred.Base = base
	s.Blurred.Text = text
	s.Blurred.CursorLine = text
	s.Blurred.CursorLineNumber = base
	s.Blurred.Placeholder = lipgloss.NewStyle().Foreground(placeholder)
	s.Blurred.Prompt = base
	s.Blurred.EndOfBuffer = base

	ta.SetStyles(s)
}

func (m askModel) Init() tea.Cmd {
	if assistantStyles.Mario {
		return tea.Batch(textarea.Blink, marioTick())
	}
	return textarea.Blink
}

// -- Update ------------------------------------------------------------------

func (m askModel) Update(msg tea.Msg) (askModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if msg.Width > askInputChromeWidth {
			m.input.SetWidth(msg.Width - askInputChromeWidth)
		}
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
			kind:      "system",
			content:   "Action rejected",
			createdAt: time.Now(),
		})
		return m, nil

	case apiErrorMsg:
		m.err = msg.err.Error()
		return m, nil

	case marioTickMsg:
		// The tick self-terminates if the screen is closing or the
		// user switched away from the Mario theme between frames.
		if m.done || !assistantStyles.Mario {
			return m, nil
		}
		m.animFrame = (m.animFrame + 1) % marioSpriteFrameCount
		return m, marioTick()

	case tea.MouseWheelMsg:
		return m.handleWheel(msg), nil

	case tea.MouseClickMsg, tea.MouseReleaseMsg, tea.MouseMotionMsg:
		// Swallow non-wheel mouse events so they don't reach the
		// textarea (which would try to move the cursor based on
		// pixel coordinates that don't match our layout).
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	// Let the textarea handle everything else (cursor, paste, etc.).
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleWheel maps a vertical mouse wheel event to a scroll offset
// change. Three lines per notch feels closer to a native chat window
// than one-line-at-a-time on a tall conversation.
func (m askModel) handleWheel(msg tea.MouseWheelMsg) askModel {
	const step = 3
	switch msg.Button {
	case tea.MouseWheelUp:
		m.scrollY -= step
		if m.scrollY < 0 {
			m.scrollY = 0
		}
	case tea.MouseWheelDown:
		max := m.maxScroll()
		m.scrollY += step
		if m.scrollY > max {
			m.scrollY = max
		}
	}
	return m
}

// handleKey dispatches key presses.
func (m askModel) handleKey(msg tea.KeyPressMsg) (askModel, tea.Cmd) {
	m.err = ""
	km := currentKeymap()

	// Actions that always run, regardless of modal state. These are
	// intentionally checked before the pendingConfirm gate so the user
	// can always back out of a confirmation, cancel a stream, or scroll.
	switch {
	case km.Matches(msg, ActionAskBack):
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

	case km.Matches(msg, ActionAskScrollUp):
		if m.scrollY > 0 {
			m.scrollY--
		}
		return m, nil

	case km.Matches(msg, ActionAskScrollDown):
		max := m.maxScroll()
		if m.scrollY < max {
			m.scrollY++
		}
		return m, nil

	case km.Matches(msg, ActionAskFocusNextTool):
		m.focusToolIdx = m.nextToolIdx(m.focusToolIdx, +1)
		return m, nil

	case km.Matches(msg, ActionAskFocusPrevTool):
		m.focusToolIdx = m.nextToolIdx(m.focusToolIdx, -1)
		return m, nil

	case km.Matches(msg, ActionAskCopy):
		if text := m.copyableText(); text != "" {
			m.err = "copied"
			return m, tea.SetClipboard(text)
		}
		return m, nil
	}

	// While a confirmation is pending, only the approve/reject shortcuts
	// are allowed. Everything else is swallowed so a stray keystroke
	// does not leak into the textarea and let the user keep typing as
	// if nothing were pending.
	if m.pendingConfirm != nil {
		switch {
		case km.Matches(msg, ActionAskApprove):
			actionID := m.pendingConfirm.actionID
			m.pendingConfirm = nil
			return m.startResumeStream(actionID)
		case km.Matches(msg, ActionAskReject):
			actionID := m.pendingConfirm.actionID
			m.pendingConfirm = nil
			return m, m.rejectAction(actionID)
		}
		return m, nil
	}

	// Shift+Enter inserts a newline. Checked before ask.submit so
	// terminals that disambiguate the two don't accidentally submit.
	// Requires Kitty keyboard protocol; on terminals without it the
	// event is indistinguishable from plain Enter and will submit.
	if km.Matches(msg, ActionAskNewline) {
		m.input.InsertRune('\n')
		return m, nil
	}

	// Submit or expand a tool card.
	if km.Matches(msg, ActionAskSubmit) {
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
	case "text_delta":
		// Incremental text chunks from streaming providers. Appended
		// to the scratch buffer as they arrive; the terminal "text"
		// event later overwrites with the authoritative content.
		m.streamingText += e.Content

	case "text":
		// Authoritative final text. For streaming providers this is
		// the concatenation of every prior text_delta; for
		// non-streaming providers it is the single blob we get back.
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
			kind:      "stream-tool",
			toolName:  e.ToolName,
			status:    status,
			raw:       raw,
			createdAt: time.Now(),
		})

	case "confirmation":
		m.pendingConfirm = &confirmationPrompt{
			actionID: e.Content,
			toolName: e.ToolName,
		}

	case "done":
		if m.streamingText != "" {
			m.turns = append(m.turns, chatTurn{
				kind:      "assistant",
				content:   m.streamingText,
				createdAt: time.Now(),
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
	m.turns = append(m.turns, chatTurn{kind: "user", content: query, createdAt: time.Now()})
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
		ts := parsePersistedTime(m.CreatedAt)
		switch m.Role {
		case "user":
			out = append(out, chatTurn{kind: "user", content: m.Content, createdAt: ts})
		case "assistant":
			if len(m.ToolCalls) > 0 && m.Content == "" {
				// The envelope row carries no body text; the
				// individual tool result rows that follow will
				// render the visible cards.
				continue
			}
			out = append(out, chatTurn{kind: "assistant", content: m.Content, createdAt: ts})
		case "tool":
			status := "ok"
			if strings.HasPrefix(m.Content, "Error:") {
				status = "error"
			}
			out = append(out, chatTurn{
				kind:      "tool",
				toolName:  m.ToolName,
				status:    status,
				raw:       json.RawMessage(m.Content),
				createdAt: ts,
			})
		case "system":
			// Audit marker -- skip.
		}
	}
	return out
}

// parsePersistedTime parses the ISO-8601 timestamp the server attaches
// to a persisted chat message. Failure returns the zero time so the
// renderer simply omits the timestamp rather than showing a bogus one.
func parsePersistedTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}
		}
	}
	return t.Local()
}

// -- View --------------------------------------------------------------------

// askLayout captures the vertical budget of the Ask Seam screen. It is
// shared by View (which renders into each region) and maxScroll (which
// uses the same viewport height to clamp the scroll offset).
type askLayout struct {
	viewportHeight int
	inputRows      int
}

// computeLayout returns the per-region row counts used by View. The
// header and status bar are one row each. When a confirmation prompt
// is pending the bordered approval box takes askConfirmRows rows; the
// rest of the time the bordered textarea footer takes askInputRows
// rows. Everything remaining goes to the conversation viewport, with
// a floor of three rows for very short terminals.
func (m askModel) computeLayout() askLayout {
	inputRows := askInputRows
	if m.pendingConfirm != nil {
		inputRows = askConfirmRows
	}
	headerRows := 1
	statusRows := 1
	viewport := m.height - headerRows - statusRows - inputRows
	if viewport < 3 {
		viewport = 3
	}
	return askLayout{viewportHeight: viewport, inputRows: inputRows}
}

// askConfirmRows is the fixed height of the approval prompt footer.
// The ConfirmPane border adds two rows, plus four rows of body and
// one leading/trailing blank gives eight.
const askConfirmRows = 8

func (m askModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	layout := m.computeLayout()

	header := assistantStyles.Header.Width(m.width).Render(" " + marioBlock + "  ASK SEAM")

	lines := m.buildConversationLines()

	start := m.scrollY
	if start > len(lines) {
		start = len(lines)
	}
	end := start + layout.viewportHeight
	if end > len(lines) {
		end = len(lines)
	}
	visible := lines[start:end]

	if m.err != "" && len(visible) < layout.viewportHeight {
		visible = append(visible, assistantStyles.Error.Render("  "+m.err))
	}

	// Pad the conversation viewport to the full terminal width and the
	// full vertical budget. Without this, blank rows are empty strings
	// that do not overwrite the previous frame's cells, which lets the
	// main screen bleed through when we switch into Ask mode.
	conversation := lipgloss.NewStyle().
		Width(m.width).
		Height(layout.viewportHeight).
		Render(strings.Join(visible, "\n"))

	// Confirmation prompt replaces the input area when pending. Both
	// branches force a fixed-size box so the surrounding regions stay
	// pinned and previous-frame content cannot bleed through the
	// padding.
	var bottom string
	if m.pendingConfirm != nil {
		bottom = lipgloss.NewStyle().
			Width(m.width).
			Height(layout.inputRows).
			Padding(0, 2).
			Render(m.renderConfirmPrompt())
	} else {
		// Trim the textarea's trailing newline so the lipgloss height
		// constraint sees exactly askInputHeight rows of content. We
		// size the input box by its OUTER width (content + border +
		// padding) because lipgloss Width() counts border/padding as
		// part of the given width budget.
		inputView := strings.TrimRight(m.input.View(), "\n")
		inputOuter := m.width - 4 // outer Padding(1, 2) = 4 cols
		if inputOuter < 20 {
			inputOuter = 20
		}
		boxed := assistantStyles.InputBox.Width(inputOuter).Render(inputView)
		bottom = lipgloss.NewStyle().
			Width(m.width).
			Height(layout.inputRows).
			Padding(1, 2).
			Render(boxed)
	}

	statusBar := m.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		conversation,
		bottom,
		statusBar,
	)
}

// bubbleChromeWidth is the width of the border+padding chrome applied
// to a chat bubble (1-col border + 1-col padding on each side).
const bubbleChromeWidth = 4

// bubbleLeftMargin is the space prepended to every bubble line so
// that the bordered box sits slightly inset from the viewport edge.
const bubbleLeftMargin = "  "

// askEmptyHint is the placeholder shown in an empty conversation.
const askEmptyHint = "Ask the assistant anything -- it can search, read, write, and remember."

// bubbleContentWidth returns the inner (content) width used when
// rendering a chat bubble, clamped so very wide terminals still get
// readable line lengths and very narrow ones still get a usable box.
func (m askModel) bubbleContentWidth() int {
	const cap = 72
	const floor = 20
	// 2 cols left margin + 4 cols chrome + 4 cols right breathing room.
	w := m.width - len(bubbleLeftMargin) - bubbleChromeWidth - 4
	if w > cap {
		w = cap
	}
	if w < floor {
		w = floor
	}
	return w
}

// buildConversationLines flattens turns into display lines with styling.
func (m askModel) buildConversationLines() []string {
	var lines []string
	innerWidth := m.bubbleContentWidth()

	if len(m.turns) == 0 && !m.streaming {
		if assistantStyles.Mario {
			sprite := renderMarioSprite(m.animFrame)
			captionRow := marioSpriteTermHeight / 2
			lines = append(lines, "")
			for i, row := range sprite {
				line := bubbleLeftMargin + row
				if i == captionRow {
					line += "  " + assistantStyles.Muted.Render(askEmptyHint)
				}
				lines = append(lines, line)
			}
			lines = append(lines, "")
		} else {
			lines = append(lines, "")
			lines = append(lines, bubbleLeftMargin+assistantStyles.Muted.Render(marioBlock+"  "+askEmptyHint))
			lines = append(lines, "")
		}
	}

	for i, t := range m.turns {
		focused := i == m.focusToolIdx
		switch t.kind {
		case "user":
			label := m.userLabel()
			ts := formatTurnTime(t.createdAt)
			fw := contentFitWidth(t.content, label, ts, innerWidth)
			lines = append(lines, m.renderMessageBubble(
				assistantStyles.BubbleUser,
				assistantStyles.BannerUser,
				label,
				t.content,
				t.createdAt,
				fw,
				false,
				true, // right-align
			)...)

		case "assistant":
			label := m.assistantLabel()
			ts := formatTurnTime(t.createdAt)
			fw := contentFitWidth(t.content, label, ts, innerWidth)
			lines = append(lines, m.renderMessageBubble(
				assistantStyles.BubbleAssist,
				assistantStyles.BannerAssist,
				label,
				t.content,
				t.createdAt,
				fw,
				false,
				false,
			)...)

		case "system":
			lines = append(lines, m.renderSystemLine(t)...)

		case "tool", "stream-tool":
			lines = append(lines, m.renderToolBubble(i, t, focused, innerWidth)...)
		}
	}

	// Streaming text bubble.
	if m.streaming && m.streamingText != "" {
		label := m.assistantLabel()
		fw := contentFitWidth(m.streamingText, label, "...", innerWidth)
		lines = append(lines, m.renderMessageBubble(
			assistantStyles.BubbleAssist,
			assistantStyles.BannerAssist,
			label,
			m.streamingText,
			time.Time{},
			fw,
			true,
			false,
		)...)
	} else if m.streaming && m.streamingText == "" {
		lines = append(lines, m.renderThinkingIndicator()...)
	}

	return lines
}

// copyableText returns the content the Ctrl+Y copy shortcut should
// put on the clipboard. If a tool card is focused it returns the tool
// result; otherwise it scans backwards for the most recent assistant
// reply and returns its text. Empty string means nothing to copy.
func (m askModel) copyableText() string {
	if m.focusToolIdx >= 0 && m.focusToolIdx < len(m.turns) {
		t := m.turns[m.focusToolIdx]
		if t.kind == "tool" || t.kind == "stream-tool" {
			return string(t.raw)
		}
	}
	for i := len(m.turns) - 1; i >= 0; i-- {
		if m.turns[i].kind == "assistant" {
			return m.turns[i].content
		}
	}
	return ""
}

// contentFitWidth returns the narrowest bubble width that contains
// both the banner (label + timestamp) and the widest content line
// without forcing a wrap, clamped between a floor of 20 cells and
// maxWidth. Short messages get a compact bubble; long messages expand
// up to the viewport cap, identical to the previous fixed-width
// behaviour.
func contentFitWidth(content, label, ts string, maxWidth int) int {
	const floor = 20
	widest := 0
	for _, line := range strings.Split(content, "\n") {
		if w := lipgloss.Width(line); w > widest {
			widest = w
		}
	}
	bannerMin := lipgloss.Width(" "+label+" ") + 1
	if ts != "" {
		bannerMin += lipgloss.Width(ts + " ")
	}
	if bannerMin > widest {
		widest = bannerMin
	}
	if widest > maxWidth {
		return maxWidth
	}
	if widest < floor {
		return floor
	}
	return widest
}

// userLabel returns the sender label for user messages. Mario gets a
// star flourish; other themes get a plain uppercase label.
func (m askModel) userLabel() string {
	if assistantStyles.Mario {
		return "★ YOU ★"
	}
	return "YOU"
}

// renderThinkingIndicator returns the lines that represent "the
// assistant is thinking" in the chat view. Mario renders an animated
// pixel-art Mario sprite walking in place with a muted caption to the
// right; other themes fall back to a single-line glyph + text. The
// middle row of the sprite carries the caption so the combined block
// reads as one balanced unit rather than a sprite floating above a
// text label.
func (m askModel) renderThinkingIndicator() []string {
	if !assistantStyles.Mario {
		return []string{
			bubbleLeftMargin + assistantStyles.Muted.Render(marioBlock+" thinking..."),
			"",
		}
	}

	sprite := renderMarioSprite(m.animFrame)
	caption := assistantStyles.Muted.Render("thinking...")
	captionRow := marioSpriteTermHeight / 2

	out := make([]string, 0, len(sprite)+1)
	for i, row := range sprite {
		line := bubbleLeftMargin + row
		if i == captionRow {
			line += "  " + caption
		}
		out = append(out, line)
	}
	out = append(out, "")
	return out
}

// assistantLabel returns the sender label for assistant messages.
// Mario gets the question-block flourish on both sides of the label;
// other themes use a single leading dot. The actual Mario animation
// lives in the "thinking..." indicator via renderMarioSprite -- the
// label itself stays static so every message bubble renders with a
// stable width.
func (m askModel) assistantLabel() string {
	if assistantStyles.Mario {
		return "▣ SEAM ▣"
	}
	return assistantStyles.Block + " SEAM"
}

// renderMessageBubble builds a bordered chat bubble with a cartoonish
// full-width banner header (inverse fg/bg) and wrapped content body.
// When alignRight is true the bubble is pushed against the right edge
// of the viewport (used for user messages). innerWidth is the number
// of visible cells inside the bubble (excluding border and padding),
// and is used to size both the banner and the wrapped content. The
// trailing blank separator is included so every call site appends a
// single block of lines.
func (m askModel) renderMessageBubble(
	box lipgloss.Style,
	bannerStyle lipgloss.Style,
	label string,
	content string,
	createdAt time.Time,
	innerWidth int,
	streaming bool,
	alignRight bool,
) []string {
	ts := formatTurnTime(createdAt)
	if streaming {
		ts = "..."
	}
	banner := renderBanner(bannerStyle, label, ts, innerWidth)

	// Thin separator in the banner's accent color between header and
	// body. Gives a retro terminal panel divider without a heavy
	// full-width colored bar.
	sep := lipgloss.NewStyle().
		Foreground(bannerStyle.GetForeground()).
		Faint(true).
		Render(strings.Repeat("─", innerWidth))

	body := banner + "\n" + sep
	wrapped := wrapContent(content, innerWidth)
	if wrapped != "" {
		body += "\n" + assistantStyles.MessageAssist.Render(wrapped)
	}

	// lipgloss Width() is the total rendered width including border
	// and padding, so we add the 4 cells of chrome to the content
	// budget before handing the body to the box.
	rendered := box.Width(innerWidth + bubbleChromeWidth).Render(body)
	if alignRight {
		return m.rightAlignAndTerminate(rendered, innerWidth)
	}
	return prefixAndTerminate(rendered, bubbleLeftMargin)
}

// renderToolBubble builds a bordered card for a tool call turn. The
// border color tracks the tool status (success = green, error = red),
// the banner mirrors the border tint, and the card body is only
// rendered when the user has expanded it.
func (m askModel) renderToolBubble(idx int, t chatTurn, focused bool, innerWidth int) []string {
	box := assistantStyles.BubbleTool
	bannerSt := assistantStyles.BannerTool
	if t.status == "error" {
		box = assistantStyles.BubbleToolWarn
		bannerSt = assistantStyles.BannerToolWarn
	}

	label := assistantStyles.Block + " " + strings.ToUpper(t.toolName)
	if focused {
		label = "> " + label
	}
	glyph := marioStatusGlyph(t.status)
	ts := formatTurnTime(t.createdAt)

	// Adaptive: size the tool card to its content when expanded.
	toolContent := ""
	if m.expanded[idx] {
		toolContent = renderToolResult(t.toolName, t.raw)
	}
	fw := contentFitWidth(toolContent, label+"  "+glyph, ts, innerWidth)

	header := renderBannerWithGlyph(bannerSt, label, glyph, ts, fw)

	body := header
	if toolContent != "" {
		wrapped := wrapContent(toolContent, fw)
		if wrapped != "" {
			body += "\n" + wrapped
		}
	}

	rendered := box.Width(fw + bubbleChromeWidth).Render(body)
	return prefixAndTerminate(rendered, bubbleLeftMargin)
}

// renderBanner builds a single-line banner with label pinned to the
// left, optional timestamp pinned to the right, and the bannerStyle's
// background filling the full innerWidth between them. Widths are
// measured with lipgloss.Width so they match the same cell-counting
// heuristic lipgloss uses when laying out the final box -- mixing
// uniseg here caused ambiguous-width glyphs (★, ▣) to wrap the
// banner to two lines.
func renderBanner(style lipgloss.Style, label, ts string, innerWidth int) string {
	left := " " + label + " "
	right := ""
	if ts != "" {
		right = ts + " "
	}
	return style.Width(innerWidth).Render(joinSpread(left, right, innerWidth))
}

// renderBannerWithGlyph is a tool-bubble variant that places a status
// glyph between the label and the timestamp. The glyph inherits the
// banner foreground so it matches the inverse color scheme.
func renderBannerWithGlyph(style lipgloss.Style, label, glyph, ts string, innerWidth int) string {
	left := " " + label + "  " + glyph + " "
	right := ""
	if ts != "" {
		right = ts + " "
	}
	return style.Width(innerWidth).Render(joinSpread(left, right, innerWidth))
}

// joinSpread concatenates left and right with enough blanks between
// them to fill exactly totalWidth visible cells. If the natural width
// already exceeds totalWidth, the result is truncated gracefully by
// falling back to a single-space separator -- lipgloss will then wrap
// as a last resort, which is still preferable to panicking.
func joinSpread(left, right string, totalWidth int) string {
	if right == "" {
		return left
	}
	gap := totalWidth - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// rightAlignAndTerminate pads each rendered line of a bordered bubble
// with leading spaces so the bubble sits against the right edge of
// the viewport, leaving two cols of breathing room on the right.
// Appends a trailing blank line to separate consecutive bubbles.
func (m askModel) rightAlignAndTerminate(rendered string, innerWidth int) []string {
	const rightMargin = 2
	bubbleOuterWidth := innerWidth + bubbleChromeWidth
	padLeft := m.width - bubbleOuterWidth - rightMargin
	if padLeft < 0 {
		padLeft = 0
	}
	pad := strings.Repeat(" ", padLeft)
	raw := strings.Split(rendered, "\n")
	out := make([]string, 0, len(raw)+1)
	for _, line := range raw {
		out = append(out, pad+line)
	}
	out = append(out, "")
	return out
}

// renderSystemLine renders a centered, italic audit marker with a
// timestamp -- cheaper than a full bubble and visually quieter.
func (m askModel) renderSystemLine(t chatTurn) []string {
	text := "· " + t.content
	if ts := formatTurnTime(t.createdAt); ts != "" {
		text += "  " + ts
	}
	return []string{bubbleLeftMargin + assistantStyles.Muted.Italic(true).Render(text), ""}
}

// wrapContent applies wrapText to every \n-delimited line of s and
// rejoins the result. Blank-line separators are preserved. Returns
// an empty string when s is empty so callers can skip the content
// region of a bubble entirely.
func wrapContent(s string, width int) string {
	if s == "" {
		return ""
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line == "" {
			out = append(out, "")
			continue
		}
		wrapped := wrapText(line, width)
		out = append(out, strings.Split(wrapped, "\n")...)
	}
	return strings.Join(out, "\n")
}

// prefixAndTerminate prepends margin to every line of rendered and
// appends a trailing blank line to separate the bubble from whatever
// comes next in the conversation stream.
func prefixAndTerminate(rendered string, margin string) []string {
	raw := strings.Split(rendered, "\n")
	out := make([]string, 0, len(raw)+1)
	for _, line := range raw {
		out = append(out, margin+line)
	}
	out = append(out, "")
	return out
}

// formatTurnTime renders a timestamp as HH:MM. A zero time yields an
// empty string so the renderer can omit the slot entirely.
func formatTurnTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("15:04")
}

// renderStatusBar picks the longest hint string that still fits on
// one line at the current width. Without this, the v1 hint exceeds
// 100 columns and lipgloss wraps the status bar to two rows, which
// throws off the fixed layout maths in computeLayout.
func (m askModel) renderStatusBar() string {
	km := currentKeymap()
	full := fmt.Sprintf("%s: send | %s: newline | %s/%s: tool | %s: expand | %s/%s: scroll | %s: copy | %s: back",
		km.Display(ActionAskSubmit),
		km.Display(ActionAskNewline),
		km.Display(ActionAskFocusNextTool),
		km.Display(ActionAskFocusPrevTool),
		km.Display(ActionAskSubmit),
		km.Display(ActionAskScrollUp),
		km.Display(ActionAskScrollDown),
		km.Display(ActionAskCopy),
		km.Display(ActionAskBack))
	medium := fmt.Sprintf("%s send · %s/%s tool · %s copy · %s/%s scroll · %s back",
		km.Display(ActionAskSubmit),
		km.Display(ActionAskFocusNextTool),
		km.Display(ActionAskFocusPrevTool),
		km.Display(ActionAskCopy),
		km.Display(ActionAskScrollUp),
		km.Display(ActionAskScrollDown),
		km.Display(ActionAskBack))
	short := fmt.Sprintf("%s send · %s copy · %s back",
		km.Display(ActionAskSubmit),
		km.Display(ActionAskCopy),
		km.Display(ActionAskBack))

	// Pick the widest variant that still fits inside the bar (the
	// StatusBar style adds two columns of horizontal padding).
	budget := m.width - 2
	text := short
	for _, candidate := range []string{full, medium} {
		if uniseg.StringWidth(candidate) <= budget {
			text = candidate
			break
		}
	}
	return assistantStyles.StatusBar.
		Width(m.width).
		Inline(true).
		Render(text)
}

// renderConfirmPrompt builds the gold-framed approval block. The
// leading/trailing spacing is handled by the parent footer region, so
// this returns just the bordered box aligned to a sane inner width.
func (m askModel) renderConfirmPrompt() string {
	innerWidth := m.width - 6
	if innerWidth < 40 {
		innerWidth = 40
	}
	body := []string{
		assistantStyles.ToolBlock.Render(" " + marioBlock + "  Action required "),
		"",
		assistantStyles.MessageAssist.Render("The assistant wants to use ") + assistantStyles.ToolBlock.Render(m.pendingConfirm.toolName) + assistantStyles.MessageAssist.Render("."),
		assistantStyles.Muted.Render("Action ID: " + m.pendingConfirm.actionID),
		"",
		assistantStyles.ToolStatusOk.Render("[a] Approve") + "     " +
			assistantStyles.ToolStatusErr.Render("[r] Reject") + "     " +
			assistantStyles.Muted.Render("[Esc] Cancel"),
	}
	return assistantStyles.ConfirmPane.Width(innerWidth).Render(strings.Join(body, "\n"))
}

// -- Scroll helpers ----------------------------------------------------------

func (m askModel) maxScroll() int {
	contentLines := len(m.buildConversationLines())
	viewportHeight := m.computeLayout().viewportHeight
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
