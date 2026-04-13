package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// ActionID is the stable public identifier for a rebindable action. Users
// write these in ~/.config/seam/tui.yaml under the `keybindings:` key, so
// renaming an ID is a breaking change for any user who has overridden it.
type ActionID string

// Scope groups actions by the screen they belong to. Used for collision
// detection and to decide whether an action's keybindings may contain a
// bare printable character (scopes with text input reject those since
// they would eat typing).
type Scope string

const (
	ScopeGlobal         Scope = "global"
	ScopeEditor         Scope = "editor"
	ScopeCapture        Scope = "capture"
	ScopeURLCapture     Scope = "url_capture"
	ScopeVoice          Scope = "voice"
	ScopeMain           Scope = "main"
	ScopeTimeline       Scope = "timeline"
	ScopeSearch         Scope = "search"
	ScopeAIAssist       Scope = "ai_assist"
	ScopeAsk            Scope = "ask"
	ScopeTemplatePicker Scope = "template_picker"
	ScopeSettings       Scope = "settings"
	ScopeLogin          Scope = "login"
)

// Action identifiers. Adding a new action requires adding its metadata to
// actionRegistry below. ctrl+c is NOT in this list: it is a hard-coded
// quit escape hatch in appModel.Update and can never be rebound.
const (
	ActionGlobalLogout ActionID = "global.logout"

	ActionEditorSave          ActionID = "editor.save"
	ActionEditorBack          ActionID = "editor.back"
	ActionEditorToggleTitle   ActionID = "editor.toggle_title"
	ActionEditorAIAssist      ActionID = "editor.ai_assist"
	ActionEditorTogglePreview ActionID = "editor.toggle_preview"

	ActionCaptureSave        ActionID = "capture.save"
	ActionCaptureSwitchField ActionID = "capture.switch_field"
	ActionCaptureCancel      ActionID = "capture.cancel"

	ActionURLCaptureSubmit ActionID = "url_capture.submit"
	ActionURLCaptureCancel ActionID = "url_capture.cancel"

	ActionVoiceToggleRecord ActionID = "voice.toggle_record"
	ActionVoiceCancel       ActionID = "voice.cancel"

	ActionMainQuit         ActionID = "main.quit"
	ActionMainSwitchPane   ActionID = "main.switch_pane"
	ActionMainNavUp        ActionID = "main.nav_up"
	ActionMainNavDown      ActionID = "main.nav_down"
	ActionMainOpenNote     ActionID = "main.open_note"
	ActionMainCapture      ActionID = "main.capture"
	ActionMainNewFromTpl   ActionID = "main.new_from_tpl"
	ActionMainURLCapture   ActionID = "main.url_capture"
	ActionMainVoiceCapture ActionID = "main.voice_capture"
	ActionMainSearch       ActionID = "main.search"
	ActionMainAsk          ActionID = "main.ask"
	ActionMainTimeline     ActionID = "main.timeline"
	ActionMainSettings     ActionID = "main.settings"
	ActionMainDeleteNote   ActionID = "main.delete_note"
	ActionMainPageForward  ActionID = "main.page_forward"
	ActionMainPageBack     ActionID = "main.page_back"
	ActionMainReload       ActionID = "main.reload"

	ActionTimelineBack       ActionID = "timeline.back"
	ActionTimelineGroupNext  ActionID = "timeline.group_next"
	ActionTimelineGroupPrev  ActionID = "timeline.group_prev"
	ActionTimelineNavUp      ActionID = "timeline.nav_up"
	ActionTimelineNavDown    ActionID = "timeline.nav_down"
	ActionTimelineOpenNote   ActionID = "timeline.open_note"
	ActionTimelineToggleSort ActionID = "timeline.toggle_sort"

	ActionSearchBack       ActionID = "search.back"
	ActionSearchNextResult ActionID = "search.next_result"
	ActionSearchPrevResult ActionID = "search.prev_result"
	ActionSearchOpenResult ActionID = "search.open_result"

	ActionAIAssistPrimary    ActionID = "ai_assist.primary"
	ActionAIAssistInsertAlt  ActionID = "ai_assist.insert_alt"
	ActionAIAssistDismissAlt ActionID = "ai_assist.dismiss_alt"
	ActionAIAssistCancel     ActionID = "ai_assist.cancel"
	ActionAIAssistNavUp      ActionID = "ai_assist.nav_up"
	ActionAIAssistNavDown    ActionID = "ai_assist.nav_down"

	ActionAskBack          ActionID = "ask.back"
	ActionAskSubmit        ActionID = "ask.submit"
	ActionAskNewline       ActionID = "ask.newline"
	ActionAskScrollUp      ActionID = "ask.scroll_up"
	ActionAskScrollDown    ActionID = "ask.scroll_down"
	ActionAskFocusNextTool ActionID = "ask.focus_next_tool"
	ActionAskFocusPrevTool ActionID = "ask.focus_prev_tool"
	ActionAskApprove       ActionID = "ask.approve"
	ActionAskReject        ActionID = "ask.reject"
	ActionAskCopy          ActionID = "ask.copy"

	ActionTplPickerCancel  ActionID = "template_picker.cancel"
	ActionTplPickerNavUp   ActionID = "template_picker.nav_up"
	ActionTplPickerNavDown ActionID = "template_picker.nav_down"
	ActionTplPickerSelect  ActionID = "template_picker.select"

	ActionSettingsCancel         ActionID = "settings.cancel"
	ActionSettingsSave           ActionID = "settings.save"
	ActionSettingsSwitchCategory ActionID = "settings.switch_category"
	ActionSettingsNavUp          ActionID = "settings.nav_up"
	ActionSettingsNavDown        ActionID = "settings.nav_down"

	ActionLoginNextField      ActionID = "login.next_field"
	ActionLoginPrevField      ActionID = "login.prev_field"
	ActionLoginSubmit         ActionID = "login.submit"
	ActionLoginToggleRegister ActionID = "login.toggle_register"
)

// actionMeta holds the static description of an action. The zero value is
// not useful; entries must come from actionRegistry.
type actionMeta struct {
	id       ActionID
	scope    Scope
	defaults []string
	help     string
	// allowBarePrintable exempts this action from the text-input scope
	// validation that otherwise rejects bare printables like "a". Set to
	// true for modal-only actions whose handler gates matching on a
	// specific state (e.g. ask.approve / ask.reject only fire while a
	// confirmation is pending, so they cannot accidentally eat typing).
	allowBarePrintable bool
}

// actionRegistry is the single source of truth for all actions. It drives
// default bindings, validation, and iteration for collision detection.
// Order is semi-stable: keep actions grouped by scope for readability, but
// lookups are done via the index map so callers never depend on slice order.
var actionRegistry = []actionMeta{
	{id: ActionGlobalLogout, scope: ScopeGlobal, defaults: []string{"ctrl+l"}, help: "Logout"},

	{id: ActionEditorSave, scope: ScopeEditor, defaults: []string{"ctrl+s", "super+s"}, help: "Save"},
	{id: ActionEditorBack, scope: ScopeEditor, defaults: []string{"esc"}, help: "Back"},
	{id: ActionEditorToggleTitle, scope: ScopeEditor, defaults: []string{"ctrl+t"}, help: "Title"},
	{id: ActionEditorAIAssist, scope: ScopeEditor, defaults: []string{"ctrl+a"}, help: "AI"},
	{id: ActionEditorTogglePreview, scope: ScopeEditor, defaults: []string{"ctrl+r"}, help: "Preview"},

	{id: ActionCaptureSave, scope: ScopeCapture, defaults: []string{"ctrl+s", "super+s"}, help: "Save"},
	{id: ActionCaptureSwitchField, scope: ScopeCapture, defaults: []string{"tab"}, help: "Next field"},
	{id: ActionCaptureCancel, scope: ScopeCapture, defaults: []string{"esc"}, help: "Cancel"},

	{id: ActionURLCaptureSubmit, scope: ScopeURLCapture, defaults: []string{"enter"}, help: "Submit"},
	{id: ActionURLCaptureCancel, scope: ScopeURLCapture, defaults: []string{"esc"}, help: "Cancel"},

	{id: ActionVoiceToggleRecord, scope: ScopeVoice, defaults: []string{"enter", "space"}, help: "Record"},
	{id: ActionVoiceCancel, scope: ScopeVoice, defaults: []string{"esc"}, help: "Cancel"},

	{id: ActionMainQuit, scope: ScopeMain, defaults: []string{"q"}, help: "Quit"},
	{id: ActionMainSwitchPane, scope: ScopeMain, defaults: []string{"tab"}, help: "Pane"},
	{id: ActionMainNavUp, scope: ScopeMain, defaults: []string{"k", "up"}, help: "Up"},
	{id: ActionMainNavDown, scope: ScopeMain, defaults: []string{"j", "down"}, help: "Down"},
	{id: ActionMainOpenNote, scope: ScopeMain, defaults: []string{"enter"}, help: "Open"},
	{id: ActionMainCapture, scope: ScopeMain, defaults: []string{"c"}, help: "Capture"},
	{id: ActionMainNewFromTpl, scope: ScopeMain, defaults: []string{"n"}, help: "Template"},
	{id: ActionMainURLCapture, scope: ScopeMain, defaults: []string{"u"}, help: "URL"},
	{id: ActionMainVoiceCapture, scope: ScopeMain, defaults: []string{"v"}, help: "Voice"},
	{id: ActionMainSearch, scope: ScopeMain, defaults: []string{"/"}, help: "Search"},
	{id: ActionMainAsk, scope: ScopeMain, defaults: []string{"a"}, help: "Ask"},
	{id: ActionMainTimeline, scope: ScopeMain, defaults: []string{"t"}, help: "Timeline"},
	{id: ActionMainSettings, scope: ScopeMain, defaults: []string{","}, help: "Settings"},
	{id: ActionMainDeleteNote, scope: ScopeMain, defaults: []string{"d"}, help: "Delete"},
	{id: ActionMainPageForward, scope: ScopeMain, defaults: []string{"ctrl+f"}, help: "Next page"},
	{id: ActionMainPageBack, scope: ScopeMain, defaults: []string{"ctrl+b"}, help: "Prev page"},
	{id: ActionMainReload, scope: ScopeMain, defaults: []string{"r"}, help: "Reload"},

	{id: ActionTimelineBack, scope: ScopeTimeline, defaults: []string{"esc", "q"}, help: "Back"},
	{id: ActionTimelineGroupNext, scope: ScopeTimeline, defaults: []string{"]", "l"}, help: "Next day"},
	{id: ActionTimelineGroupPrev, scope: ScopeTimeline, defaults: []string{"[", "h"}, help: "Prev day"},
	{id: ActionTimelineNavUp, scope: ScopeTimeline, defaults: []string{"k", "up"}, help: "Up"},
	{id: ActionTimelineNavDown, scope: ScopeTimeline, defaults: []string{"j", "down"}, help: "Down"},
	{id: ActionTimelineOpenNote, scope: ScopeTimeline, defaults: []string{"enter"}, help: "Open"},
	{id: ActionTimelineToggleSort, scope: ScopeTimeline, defaults: []string{"s"}, help: "Sort"},

	{id: ActionSearchBack, scope: ScopeSearch, defaults: []string{"esc"}, help: "Back"},
	{id: ActionSearchNextResult, scope: ScopeSearch, defaults: []string{"down", "ctrl+n"}, help: "Next"},
	{id: ActionSearchPrevResult, scope: ScopeSearch, defaults: []string{"up", "ctrl+p"}, help: "Prev"},
	{id: ActionSearchOpenResult, scope: ScopeSearch, defaults: []string{"enter"}, help: "Open"},

	{id: ActionAIAssistPrimary, scope: ScopeAIAssist, defaults: []string{"enter"}, help: "Go"},
	{id: ActionAIAssistInsertAlt, scope: ScopeAIAssist, defaults: []string{"y"}, help: "Insert"},
	{id: ActionAIAssistDismissAlt, scope: ScopeAIAssist, defaults: []string{"n"}, help: "Dismiss"},
	{id: ActionAIAssistCancel, scope: ScopeAIAssist, defaults: []string{"esc"}, help: "Cancel"},
	{id: ActionAIAssistNavUp, scope: ScopeAIAssist, defaults: []string{"k", "up"}, help: "Up"},
	{id: ActionAIAssistNavDown, scope: ScopeAIAssist, defaults: []string{"j", "down"}, help: "Down"},

	{id: ActionAskBack, scope: ScopeAsk, defaults: []string{"esc"}, help: "Back"},
	{id: ActionAskSubmit, scope: ScopeAsk, defaults: []string{"enter"}, help: "Submit"},
	{id: ActionAskNewline, scope: ScopeAsk, defaults: []string{"shift+enter"}, help: "Newline"},
	{id: ActionAskScrollUp, scope: ScopeAsk, defaults: []string{"ctrl+up"}, help: "Scroll up"},
	{id: ActionAskScrollDown, scope: ScopeAsk, defaults: []string{"ctrl+down"}, help: "Scroll down"},
	{id: ActionAskFocusNextTool, scope: ScopeAsk, defaults: []string{"tab"}, help: "Next tool"},
	{id: ActionAskFocusPrevTool, scope: ScopeAsk, defaults: []string{"shift+tab"}, help: "Prev tool"},
	{id: ActionAskApprove, scope: ScopeAsk, defaults: []string{"a"}, help: "Approve", allowBarePrintable: true},
	{id: ActionAskReject, scope: ScopeAsk, defaults: []string{"r"}, help: "Reject", allowBarePrintable: true},
	{id: ActionAskCopy, scope: ScopeAsk, defaults: []string{"ctrl+y"}, help: "Copy last reply"},

	{id: ActionTplPickerCancel, scope: ScopeTemplatePicker, defaults: []string{"esc"}, help: "Cancel"},
	{id: ActionTplPickerNavUp, scope: ScopeTemplatePicker, defaults: []string{"k", "up"}, help: "Up"},
	{id: ActionTplPickerNavDown, scope: ScopeTemplatePicker, defaults: []string{"j", "down"}, help: "Down"},
	{id: ActionTplPickerSelect, scope: ScopeTemplatePicker, defaults: []string{"enter"}, help: "Select"},

	{id: ActionSettingsCancel, scope: ScopeSettings, defaults: []string{"esc"}, help: "Cancel"},
	{id: ActionSettingsSave, scope: ScopeSettings, defaults: []string{"enter"}, help: "Save"},
	{id: ActionSettingsSwitchCategory, scope: ScopeSettings, defaults: []string{"tab"}, help: "Category"},
	{id: ActionSettingsNavUp, scope: ScopeSettings, defaults: []string{"k", "up"}, help: "Up"},
	{id: ActionSettingsNavDown, scope: ScopeSettings, defaults: []string{"j", "down"}, help: "Down"},

	{id: ActionLoginNextField, scope: ScopeLogin, defaults: []string{"tab", "down"}, help: "Next"},
	{id: ActionLoginPrevField, scope: ScopeLogin, defaults: []string{"shift+tab", "up"}, help: "Prev"},
	{id: ActionLoginSubmit, scope: ScopeLogin, defaults: []string{"enter"}, help: "Submit"},
	{id: ActionLoginToggleRegister, scope: ScopeLogin, defaults: []string{"ctrl+r"}, help: "Register"},
}

// scopeHasTextInput lists the scopes that contain a focused textarea or
// textinput at some point during their lifecycle. Bindings in these scopes
// may not use bare printable characters (e.g. "a") because those would be
// intercepted before the input component sees them, breaking typing. Modal
// actions can opt out via allowBarePrintable on their actionMeta.
var scopeHasTextInput = map[Scope]bool{
	ScopeEditor:         true,
	ScopeCapture:        true,
	ScopeURLCapture:     true,
	ScopeSearch:         true,
	ScopeAsk:            true,
	ScopeTemplatePicker: true,
	ScopeLogin:          true,
}

// actionIndex is built once from actionRegistry and used for O(1) lookup.
var actionIndex = func() map[ActionID]actionMeta {
	m := make(map[ActionID]actionMeta, len(actionRegistry))
	for _, a := range actionRegistry {
		m[a.id] = a
	}
	return m
}()

// Keymap holds the resolved key bindings for every action after merging
// defaults with user overrides. A nil map entry means the action is
// unbound (empty user list) and will not match anything.
type Keymap struct {
	bindings map[ActionID][]string
}

// Matches returns true if the key event matches any of the given actions.
// It delegates to ultraviolet's Key.MatchString which correctly handles
// modifier order, shift+letter ↔ uppercase equivalence, and printable vs
// special key matching. Callers can pass multiple action IDs to OR them.
func (k *Keymap) Matches(msg tea.KeyPressMsg, actions ...ActionID) bool {
	if k == nil {
		return false
	}
	uvKey := uv.Key(msg.Key())
	for _, id := range actions {
		keys := k.bindings[id]
		if len(keys) == 0 {
			continue
		}
		if uvKey.MatchString(keys...) {
			return true
		}
	}
	return false
}

// Display returns a human-readable label for the first key bound to the
// action, canonicalized for status-bar rendering (e.g. "ctrl+s" becomes
// "Ctrl+S", "super+s" becomes "Cmd+S"). Returns "-" if the action is
// unbound or missing.
func (k *Keymap) Display(action ActionID) string {
	if k == nil {
		return "-"
	}
	keys := k.bindings[action]
	if len(keys) == 0 {
		return "-"
	}
	return prettifyKey(keys[0])
}

// DisplayAll returns a slash-joined list of every bound key for the action,
// each canonicalized. Returns "-" if unbound. Rarely used: status bars
// normally call Display for a single canonical key.
func (k *Keymap) DisplayAll(action ActionID) string {
	if k == nil {
		return "-"
	}
	keys := k.bindings[action]
	if len(keys) == 0 {
		return "-"
	}
	pretty := make([]string, len(keys))
	for i, v := range keys {
		pretty[i] = prettifyKey(v)
	}
	return strings.Join(pretty, "/")
}

// prettifyKey formats a single key string for display. It uppercases
// modifier names and common key names, replaces super with Cmd, and
// title-cases single-letter keys.
func prettifyKey(s string) string {
	parts := strings.Split(s, "+")
	for i, p := range parts {
		lower := strings.ToLower(p)
		switch lower {
		case "ctrl":
			parts[i] = "Ctrl"
		case "alt":
			parts[i] = "Alt"
		case "shift":
			parts[i] = "Shift"
		case "meta":
			parts[i] = "Meta"
		case "hyper":
			parts[i] = "Hyper"
		case "super":
			parts[i] = "Cmd"
		case "esc", "escape":
			parts[i] = "Esc"
		case "enter", "return":
			parts[i] = "Enter"
		case "tab":
			parts[i] = "Tab"
		case "space":
			parts[i] = "Space"
		case "up":
			parts[i] = "Up"
		case "down":
			parts[i] = "Down"
		case "left":
			parts[i] = "Left"
		case "right":
			parts[i] = "Right"
		case "home":
			parts[i] = "Home"
		case "end":
			parts[i] = "End"
		case "pageup":
			parts[i] = "PgUp"
		case "pagedown":
			parts[i] = "PgDn"
		case "backspace":
			parts[i] = "Bksp"
		case "delete":
			parts[i] = "Del"
		case "insert":
			parts[i] = "Ins"
		default:
			if strings.HasPrefix(lower, "f") && len(lower) > 1 {
				allDigits := true
				for _, c := range lower[1:] {
					if !unicode.IsDigit(c) {
						allDigits = false
						break
					}
				}
				if allDigits {
					parts[i] = "F" + lower[1:]
					continue
				}
			}
			if len(p) == 1 {
				parts[i] = strings.ToUpper(p)
			}
		}
	}
	return strings.Join(parts, "+")
}

// activeKeymap is the process-wide keymap, set once at startup in main.go
// after ResolveTUIConfig runs. Mirrors the activeTheme pattern so models do
// not need a keymap injected through every constructor. Tests use
// setKeymapForTest to swap this out with cleanup.
var activeKeymap *Keymap

// currentKeymap returns the active keymap. If the TUI has not been
// initialized (only possible in tests that forgot setKeymapForTest), it
// returns a keymap built from defaults so models never nil-panic.
func currentKeymap() *Keymap {
	if activeKeymap != nil {
		return activeKeymap
	}
	return DefaultKeymap()
}

// DefaultKeymap returns a fresh keymap with every action bound to its
// registered defaults. Useful for tests and as the seed for LoadKeymap.
func DefaultKeymap() *Keymap {
	km := &Keymap{bindings: make(map[ActionID][]string, len(actionRegistry))}
	for _, a := range actionRegistry {
		keys := make([]string, len(a.defaults))
		copy(keys, a.defaults)
		km.bindings[a.id] = keys
	}
	return km
}

// LoadKeymap merges the user's keybinding overrides from cfg on top of the
// built-in defaults. Validation is tolerant: every warning is written to
// stderr with the prefix "seam: keybindings: ..." and the offending entry
// is skipped. The TUI always starts successfully even with a broken
// keybindings block.
func LoadKeymap(cfg TUIConfig) *Keymap {
	km := DefaultKeymap()
	if len(cfg.Keybindings) == 0 {
		return km
	}

	// Apply overrides in a stable order so warnings are reproducible.
	overrides := make([]string, 0, len(cfg.Keybindings))
	for k := range cfg.Keybindings {
		overrides = append(overrides, k)
	}
	sort.Strings(overrides)

	for _, rawAction := range overrides {
		rawKeys := cfg.Keybindings[rawAction]
		meta, ok := actionIndex[ActionID(rawAction)]
		if !ok {
			warnKeybinding("unknown action %q, ignoring (known actions start with global./editor./main./etc.)", rawAction)
			continue
		}

		// Empty user list = unbind the action entirely. Users opt in to
		// this by writing `editor.save: []` in their YAML.
		if len(rawKeys) == 0 {
			km.bindings[meta.id] = nil
			continue
		}

		valid := make([]string, 0, len(rawKeys))
		for _, k := range rawKeys {
			trimmed := strings.TrimSpace(k)
			if trimmed == "" {
				warnKeybinding("%s: empty key string, skipping", rawAction)
				continue
			}
			if scopeHasTextInput[meta.scope] && !meta.allowBarePrintable && isBarePrintable(trimmed) {
				warnKeybinding("%s: %q is a bare printable in a text-input screen and would eat typing; add a modifier like ctrl+%s", rawAction, trimmed, trimmed)
				continue
			}
			if hasAltLetter(trimmed) {
				warnKeybinding("%s: %q uses Alt+<letter>, which does not fire on macOS Terminal.app (it composes to a Latin-1 character). Works in Kitty, Ghostty, or WezTerm with the Kitty keyboard protocol", rawAction, trimmed)
			}
			if hasSuper(trimmed) {
				warnKeybinding("%s: %q uses Super (Cmd on Mac); only terminals implementing the Kitty keyboard protocol forward this modifier (Kitty, Ghostty, WezTerm). Most terminals will ignore it", rawAction, trimmed)
			}
			valid = append(valid, trimmed)
		}
		km.bindings[meta.id] = valid
	}

	// Same-scope collision detection on the final merged keymap. If two
	// different actions in the same scope share a key, the earlier entry
	// in actionRegistry wins at runtime (whichever Matches check runs
	// first in the handler); we flag it here so the user knows.
	detectScopeCollisions(km)

	// Cross-tier shadowing: any screen-scope binding that reuses a global
	// key is silently dead at runtime. Warn loudly.
	detectGlobalShadows(km)

	return km
}

// isBarePrintable reports whether s is a single printable character with
// no modifier, e.g. "a", "1", "/". Special key names like "esc", "enter",
// "f2", "up" are not printables.
func isBarePrintable(s string) bool {
	if strings.Contains(s, "+") {
		return false
	}
	lower := strings.ToLower(s)
	switch lower {
	case "esc", "escape", "enter", "return", "tab", "space",
		"up", "down", "left", "right",
		"home", "end", "pageup", "pagedown",
		"backspace", "delete", "insert":
		return false
	}
	if strings.HasPrefix(lower, "f") && len(lower) > 1 {
		allDigits := true
		for _, c := range lower[1:] {
			if !unicode.IsDigit(c) {
				allDigits = false
				break
			}
		}
		if allDigits {
			return false
		}
	}
	runes := []rune(s)
	return len(runes) == 1 && unicode.IsPrint(runes[0])
}

// hasAltLetter reports whether s is an Alt+<letter> combo (with no other
// modifiers). These are protocol-dependent: macOS Terminal.app composes
// them into dead-keys, so the TUI never sees an alt+s event.
func hasAltLetter(s string) bool {
	parts := strings.Split(strings.ToLower(s), "+")
	if len(parts) != 2 || parts[0] != "alt" {
		return false
	}
	runes := []rune(parts[1])
	return len(runes) == 1 && unicode.IsLetter(runes[0])
}

// hasSuper reports whether s contains a super (Cmd) modifier.
func hasSuper(s string) bool {
	for _, p := range strings.Split(strings.ToLower(s), "+") {
		if p == "super" {
			return true
		}
	}
	return false
}

func detectScopeCollisions(km *Keymap) {
	type scopeKey struct {
		scope Scope
		key   string
	}
	seen := make(map[scopeKey]ActionID)
	for _, meta := range actionRegistry {
		for _, k := range km.bindings[meta.id] {
			sk := scopeKey{scope: meta.scope, key: strings.ToLower(k)}
			if prev, ok := seen[sk]; ok && prev != meta.id {
				warnKeybinding("%s and %s both bind %q in the %s scope; the first-registered action wins", prev, meta.id, k, meta.scope)
				continue
			}
			seen[sk] = meta.id
		}
	}
}

func detectGlobalShadows(km *Keymap) {
	globals := make(map[string]ActionID)
	for _, meta := range actionRegistry {
		if meta.scope != ScopeGlobal {
			continue
		}
		for _, k := range km.bindings[meta.id] {
			globals[strings.ToLower(k)] = meta.id
		}
	}
	if len(globals) == 0 {
		return
	}
	for _, meta := range actionRegistry {
		if meta.scope == ScopeGlobal {
			continue
		}
		for _, k := range km.bindings[meta.id] {
			if g, ok := globals[strings.ToLower(k)]; ok {
				warnKeybinding("%s binds %q but the global action %s also uses it; the screen binding will never fire", meta.id, k, g)
			}
		}
	}
}

func warnKeybinding(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "seam: keybindings: "+format+"\n", args...)
}
