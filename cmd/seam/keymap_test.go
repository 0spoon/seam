package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

// mkKey builds a tea.KeyPressMsg from the ultraviolet key that would
// result from typing `s` in a terminal. It mirrors what the decoder
// produces so Matches exercises the real path. Pass mods and code
// explicitly; text is computed from the shifted rule bubbletea uses.
func mkKey(code rune, mod uv.KeyMod, text string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code, Mod: mod, Text: text})
}

func TestDefaultKeymap_AllActionsBound(t *testing.T) {
	km := DefaultKeymap()
	for _, meta := range actionRegistry {
		got, ok := km.bindings[meta.id]
		require.True(t, ok, "action %s missing from default keymap", meta.id)
		require.NotEmpty(t, got, "action %s has no default keys", meta.id)
		require.Equal(t, meta.defaults, got, "action %s defaults mismatch", meta.id)
	}
}

func TestKeymap_Matches_CtrlS(t *testing.T) {
	km := DefaultKeymap()
	msg := mkKey('s', uv.ModCtrl, "")

	require.True(t, km.Matches(msg, ActionEditorSave))
	require.False(t, km.Matches(msg, ActionEditorBack))
	require.False(t, km.Matches(msg, ActionEditorToggleTitle))
}

func TestKeymap_Matches_MultipleActions(t *testing.T) {
	km := DefaultKeymap()
	msg := mkKey('s', uv.ModCtrl, "")

	// OR semantics: matches if ANY of the actions match.
	require.True(t, km.Matches(msg, ActionEditorBack, ActionEditorSave))
}

func TestKeymap_Matches_PlainLetter(t *testing.T) {
	km := DefaultKeymap()
	// 'a' typed into a main screen context should trigger "ask".
	msg := mkKey('a', 0, "a")

	require.True(t, km.Matches(msg, ActionMainAsk))
	require.False(t, km.Matches(msg, ActionMainDeleteNote))
}

func TestKeymap_Matches_ShiftedLetter(t *testing.T) {
	km := &Keymap{bindings: map[ActionID][]string{
		"test.upper_a": {"A"},
	}}

	// Legacy terminal: shift+a arrives with Text="A", Mod=Shift, Code='a'.
	legacy := mkKey('a', uv.ModShift, "A")
	require.True(t, km.Matches(legacy, "test.upper_a"))

	// Kitty protocol: shift+a arrives with ShiftedCode='A', Text="A",
	// Code='a', Mod=Shift. MatchString still matches via k.Text.
	kitty := tea.KeyPressMsg(tea.Key{Code: 'a', ShiftedCode: 'A', Text: "A", Mod: uv.ModShift})
	require.True(t, km.Matches(kitty, "test.upper_a"))
}

func TestKeymap_Matches_UnboundAction(t *testing.T) {
	km := &Keymap{bindings: map[ActionID][]string{
		ActionEditorSave: nil,
	}}
	msg := mkKey('s', uv.ModCtrl, "")
	require.False(t, km.Matches(msg, ActionEditorSave))
}

func TestKeymap_Matches_NilReceiver(t *testing.T) {
	var km *Keymap
	msg := mkKey('s', uv.ModCtrl, "")
	require.False(t, km.Matches(msg, ActionEditorSave))
}

func TestKeymap_Display(t *testing.T) {
	km := DefaultKeymap()
	tests := []struct {
		action ActionID
		want   string
	}{
		{ActionEditorSave, "Ctrl+S"},
		{ActionEditorBack, "Esc"},
		{ActionEditorToggleTitle, "Ctrl+T"},
		{ActionMainQuit, "Q"},
		{ActionMainSearch, "/"},
		{ActionMainSettings, ","},
		{ActionTimelineGroupNext, "]"},
		{ActionVoiceToggleRecord, "Enter"},
		{ActionAskNewline, "Shift+Enter"},
		{ActionLoginPrevField, "Shift+Tab"},
	}
	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			require.Equal(t, tt.want, km.Display(tt.action))
		})
	}
}

func TestKeymap_Display_Unbound(t *testing.T) {
	km := &Keymap{bindings: map[ActionID][]string{
		ActionEditorSave: nil,
	}}
	require.Equal(t, "-", km.Display(ActionEditorSave))
	require.Equal(t, "-", km.Display("nonexistent.action"))
}

func TestKeymap_DisplayAll(t *testing.T) {
	km := DefaultKeymap()
	require.Equal(t, "Ctrl+S/Cmd+S", km.DisplayAll(ActionEditorSave))
	require.Equal(t, "K/Up", km.DisplayAll(ActionMainNavUp))
}

func TestLoadKeymap_NoOverrides(t *testing.T) {
	cfg := TUIConfig{}
	km := LoadKeymap(cfg)
	require.Equal(t, DefaultKeymap(), km)
}

func TestLoadKeymap_ValidOverride(t *testing.T) {
	cfg := TUIConfig{
		Keybindings: map[string][]string{
			"editor.save": {"ctrl+w", "ctrl+s"},
		},
	}
	km := LoadKeymap(cfg)
	require.Equal(t, []string{"ctrl+w", "ctrl+s"}, km.bindings[ActionEditorSave])

	// ctrl+w should now match.
	require.True(t, km.Matches(mkKey('w', uv.ModCtrl, ""), ActionEditorSave))
	// ctrl+s still matches because the user kept it.
	require.True(t, km.Matches(mkKey('s', uv.ModCtrl, ""), ActionEditorSave))
	// super+s no longer matches because the user's list replaced defaults.
	require.False(t, km.Matches(mkKey('s', uv.ModSuper, ""), ActionEditorSave))
}

func TestLoadKeymap_UnbindViaEmptyList(t *testing.T) {
	cfg := TUIConfig{
		Keybindings: map[string][]string{
			"main.delete_note": {},
		},
	}
	km := LoadKeymap(cfg)
	require.Empty(t, km.bindings[ActionMainDeleteNote])
	require.False(t, km.Matches(mkKey('d', 0, "d"), ActionMainDeleteNote))
}

func TestLoadKeymap_UnknownActionIgnored(t *testing.T) {
	cfg := TUIConfig{
		Keybindings: map[string][]string{
			"nonexistent.action": {"ctrl+z"},
			"editor.save":        {"ctrl+w"},
		},
	}
	km := LoadKeymap(cfg)
	// Known action override still applied.
	require.Equal(t, []string{"ctrl+w"}, km.bindings[ActionEditorSave])
}

func TestLoadKeymap_BarePrintableInTextInputScope(t *testing.T) {
	cfg := TUIConfig{
		Keybindings: map[string][]string{
			// editor is a text-input scope and does NOT allow bare "s".
			"editor.save": {"s", "ctrl+w"},
		},
	}
	km := LoadKeymap(cfg)
	// "s" dropped, "ctrl+w" kept.
	require.Equal(t, []string{"ctrl+w"}, km.bindings[ActionEditorSave])
}

func TestLoadKeymap_BarePrintableAllowedOnMainScreen(t *testing.T) {
	cfg := TUIConfig{
		Keybindings: map[string][]string{
			// main_screen has no text input; bare letters are fine.
			"main.search": {"s"},
		},
	}
	km := LoadKeymap(cfg)
	require.Equal(t, []string{"s"}, km.bindings[ActionMainSearch])
	require.True(t, km.Matches(mkKey('s', 0, "s"), ActionMainSearch))
}

func TestLoadKeymap_AllowBarePrintableForModalActions(t *testing.T) {
	cfg := TUIConfig{
		Keybindings: map[string][]string{
			// ask is a text-input scope but ask.approve is modal-only
			// and opts in via allowBarePrintable.
			"ask.approve": {"y"},
		},
	}
	km := LoadKeymap(cfg)
	require.Equal(t, []string{"y"}, km.bindings[ActionAskApprove])
}

func TestLoadKeymap_EmptyKeyStringSkipped(t *testing.T) {
	cfg := TUIConfig{
		Keybindings: map[string][]string{
			"editor.save": {"", "ctrl+w"},
		},
	}
	km := LoadKeymap(cfg)
	require.Equal(t, []string{"ctrl+w"}, km.bindings[ActionEditorSave])
}

func TestLoadKeymap_WhitespaceIsTrimmed(t *testing.T) {
	cfg := TUIConfig{
		Keybindings: map[string][]string{
			"editor.save": {"  ctrl+w  "},
		},
	}
	km := LoadKeymap(cfg)
	require.Equal(t, []string{"ctrl+w"}, km.bindings[ActionEditorSave])
}

func TestIsBarePrintable(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"a", true},
		{"Z", true},
		{"1", true},
		{"/", true},
		{",", true},
		{"ctrl+s", false},
		{"alt+s", false},
		{"super+s", false},
		{"esc", false},
		{"enter", false},
		{"tab", false},
		{"space", false},
		{"up", false},
		{"down", false},
		{"f2", false},
		{"F12", false},
		{"", false},
		{"ab", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			require.Equal(t, tt.want, isBarePrintable(tt.in))
		})
	}
}

func TestHasAltLetter(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"alt+s", true},
		{"alt+a", true},
		{"Alt+S", true},
		{"alt+ctrl+s", false},
		{"alt+1", false},
		{"alt+enter", false},
		{"ctrl+s", false},
		{"s", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			require.Equal(t, tt.want, hasAltLetter(tt.in))
		})
	}
}

func TestHasSuper(t *testing.T) {
	require.True(t, hasSuper("super+s"))
	require.True(t, hasSuper("ctrl+super+s"))
	require.True(t, hasSuper("Super+S"))
	require.False(t, hasSuper("ctrl+s"))
	require.False(t, hasSuper("alt+s"))
}

func TestPrettifyKey(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"ctrl+s", "Ctrl+S"},
		{"super+s", "Cmd+S"},
		{"alt+a", "Alt+A"},
		{"shift+tab", "Shift+Tab"},
		{"ctrl+shift+p", "Ctrl+Shift+P"},
		{"esc", "Esc"},
		{"enter", "Enter"},
		{"space", "Space"},
		{"up", "Up"},
		{"pageup", "PgUp"},
		{"f2", "F2"},
		{"f12", "F12"},
		{"/", "/"},
		{"]", "]"},
		{"a", "A"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			require.Equal(t, tt.want, prettifyKey(tt.in))
		})
	}
}

// setKeymapForTest sets activeKeymap for the duration of t and restores it
// on cleanup. Used by refactored screen tests that depend on currentKeymap.
func setKeymapForTest(t *testing.T, km *Keymap) {
	t.Helper()
	prev := activeKeymap
	activeKeymap = km
	t.Cleanup(func() { activeKeymap = prev })
}

func TestCurrentKeymap_FallsBackToDefault(t *testing.T) {
	setKeymapForTest(t, nil)
	km := currentKeymap()
	require.NotNil(t, km)
	// Should behave like the default.
	require.True(t, km.Matches(mkKey('s', uv.ModCtrl, ""), ActionEditorSave))
}

func TestCurrentKeymap_UsesActive(t *testing.T) {
	custom := &Keymap{bindings: map[ActionID][]string{
		ActionEditorSave: {"ctrl+w"},
	}}
	setKeymapForTest(t, custom)
	require.Same(t, custom, currentKeymap())
}
