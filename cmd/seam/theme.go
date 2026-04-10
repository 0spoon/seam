package main

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Theme is a semantic color palette plus border shape used to build all
// visible TUI styles. A single Theme drives both the main UI and (when
// assistant_theme is "follow_global") the assistant screen, so the slots
// must cover both surfaces.
//
// Catppuccin variants follow canonical role assignments: Primary = Mauve,
// Secondary = Lavender, Fg = Text, Muted = Subtext1, Dim = Overlay0,
// HeaderBg/StatusBarBg = Mantle, Selected = Surface0, Border = Surface1,
// Error = Red, Success = Green.
//
// The Mario theme keeps the original assistant ambience: warm reds, pipe
// green, coin gold, with a thick double-line border instead of rounded.
type Theme struct {
	Name        string
	Primary     color.Color
	Secondary   color.Color
	Muted       color.Color
	Fg          color.Color
	HeaderBg    color.Color
	StatusBarBg color.Color
	Selected    color.Color
	Border      color.Color
	BorderShape lipgloss.Border
	Error       color.Color
	Success     color.Color
	Dim         color.Color
	// AccentBlock is the glyph used to prefix accent text in the assistant
	// screen (Mario uses the question-block; Catppuccin uses an empty
	// string so the surrounding label stands alone).
	AccentBlock string
}

// -- Built-in themes ---------------------------------------------------------

// themeSeam is the original warm-amber palette, kept as a built-in so
// users who prefer the historical Seam look can opt back into it.
var themeSeam = Theme{
	Name:        "seam",
	Primary:     lipgloss.Color("#c4915c"),
	Secondary:   lipgloss.Color("#a68a6e"),
	Muted:       lipgloss.Color("#9992a6"),
	Fg:          lipgloss.Color("#e8e2d9"),
	HeaderBg:    lipgloss.Color("#242120"),
	StatusBarBg: lipgloss.Color("#242120"),
	Selected:    lipgloss.Color("#3a3330"),
	Border:      lipgloss.Color("#3a3330"),
	BorderShape: lipgloss.RoundedBorder(),
	Error:       lipgloss.Color("#c46b6b"),
	Success:     lipgloss.Color("#6b9b7a"),
	Dim:         lipgloss.Color("#5e5a6e"),
	AccentBlock: "",
}

var themeCatppuccinMocha = Theme{
	Name:        "catppuccin-mocha",
	Primary:     lipgloss.Color("#cba6f7"), // Mauve
	Secondary:   lipgloss.Color("#b4befe"), // Lavender
	Muted:       lipgloss.Color("#bac2de"), // Subtext1
	Fg:          lipgloss.Color("#cdd6f4"), // Text
	HeaderBg:    lipgloss.Color("#181825"), // Mantle
	StatusBarBg: lipgloss.Color("#181825"), // Mantle
	Selected:    lipgloss.Color("#313244"), // Surface0
	Border:      lipgloss.Color("#45475a"), // Surface1
	BorderShape: lipgloss.RoundedBorder(),
	Error:       lipgloss.Color("#f38ba8"), // Red
	Success:     lipgloss.Color("#a6e3a1"), // Green
	Dim:         lipgloss.Color("#6c7086"), // Overlay0
	AccentBlock: "",
}

var themeCatppuccinMacchiato = Theme{
	Name:        "catppuccin-macchiato",
	Primary:     lipgloss.Color("#c6a0f6"),
	Secondary:   lipgloss.Color("#b7bdf8"),
	Muted:       lipgloss.Color("#b8c0e0"),
	Fg:          lipgloss.Color("#cad3f5"),
	HeaderBg:    lipgloss.Color("#1e2030"),
	StatusBarBg: lipgloss.Color("#1e2030"),
	Selected:    lipgloss.Color("#363a4f"),
	Border:      lipgloss.Color("#494d64"),
	BorderShape: lipgloss.RoundedBorder(),
	Error:       lipgloss.Color("#ed8796"),
	Success:     lipgloss.Color("#a6da95"),
	Dim:         lipgloss.Color("#6e738d"),
	AccentBlock: "",
}

var themeCatppuccinFrappe = Theme{
	Name:        "catppuccin-frappe",
	Primary:     lipgloss.Color("#ca9ee6"),
	Secondary:   lipgloss.Color("#babbf1"),
	Muted:       lipgloss.Color("#b5bfe2"),
	Fg:          lipgloss.Color("#c6d0f5"),
	HeaderBg:    lipgloss.Color("#292c3c"),
	StatusBarBg: lipgloss.Color("#292c3c"),
	Selected:    lipgloss.Color("#414559"),
	Border:      lipgloss.Color("#51576d"),
	BorderShape: lipgloss.RoundedBorder(),
	Error:       lipgloss.Color("#e78284"),
	Success:     lipgloss.Color("#a6d189"),
	Dim:         lipgloss.Color("#737994"),
	AccentBlock: "",
}

var themeCatppuccinLatte = Theme{
	Name:        "catppuccin-latte",
	Primary:     lipgloss.Color("#8839ef"),
	Secondary:   lipgloss.Color("#7287fd"),
	Muted:       lipgloss.Color("#6c6f85"),
	Fg:          lipgloss.Color("#4c4f69"),
	HeaderBg:    lipgloss.Color("#e6e9ef"),
	StatusBarBg: lipgloss.Color("#e6e9ef"),
	Selected:    lipgloss.Color("#ccd0da"),
	Border:      lipgloss.Color("#bcc0cc"),
	BorderShape: lipgloss.RoundedBorder(),
	Error:       lipgloss.Color("#d20f39"),
	Success:     lipgloss.Color("#40a02b"),
	Dim:         lipgloss.Color("#9ca0b0"),
	AccentBlock: "",
}

// themeMario is the assistant screen's signature look. Used only when
// assistant_theme is "mario". The pipe border and accent block glyph live
// here so the assistant style builder is unaware of which theme it is
// receiving.
var themeMario = Theme{
	Name:        "mario",
	Primary:     lipgloss.Color("#FBD000"), // Coin gold
	Secondary:   lipgloss.Color("#5C94FC"), // Sky
	Muted:       lipgloss.Color("#8B7355"), // Brown muted
	Fg:          lipgloss.Color("#FCE5C8"), // White
	HeaderBg:    lipgloss.Color("#E52521"), // Red
	StatusBarBg: lipgloss.Color("#7B3F00"), // Brick brown
	Selected:    lipgloss.Color("#7B3F00"),
	Border:      lipgloss.Color("#FBD000"), // Coin gold
	BorderShape: marioPipeBorder,
	Error:       lipgloss.Color("#E52521"),
	Success:     lipgloss.Color("#43B047"), // Pipe green
	Dim:         lipgloss.Color("#8B7355"),
	AccentBlock: marioBlock,
}

// -- Registry ----------------------------------------------------------------

// themeRegistry maps a slug to a Theme. New themes go here.
var themeRegistry = map[string]Theme{
	themeSeam.Name:                themeSeam,
	themeCatppuccinMocha.Name:     themeCatppuccinMocha,
	themeCatppuccinMacchiato.Name: themeCatppuccinMacchiato,
	themeCatppuccinFrappe.Name:    themeCatppuccinFrappe,
	themeCatppuccinLatte.Name:     themeCatppuccinLatte,
	themeMario.Name:               themeMario,
}

// defaultTheme is the theme used when no config, env var, or CLI flag
// has chosen one. Catppuccin Mocha is the dark default; users who prefer
// the historical warm-amber Seam look can opt into "seam" via the picker
// or config file.
var defaultTheme = themeCatppuccinMocha

// defaultAssistantTheme is the assistant screen's theme when no override
// is provided. Mario stays the default to preserve the historical look.
var defaultAssistantTheme = themeMario

// AssistantInheritName is the special value that means "use the active
// global theme on the assistant screen too".
const AssistantInheritName = "follow_global"

// activeTheme holds the currently applied global theme. Mutated by
// ApplyTheme; safe because Bubble Tea Update/View is single-goroutine.
var activeTheme = defaultTheme

// activeAssistantTheme holds the theme used by assistant screen styles.
// When assistant_theme is "follow_global", this mirrors activeTheme;
// otherwise it is a registered theme like themeMario.
var activeAssistantTheme = defaultAssistantTheme

// ResolveTheme returns a registered theme by name. Returns false if the
// name is not in the registry.
func ResolveTheme(name string) (Theme, bool) {
	t, ok := themeRegistry[strings.ToLower(strings.TrimSpace(name))]
	return t, ok
}

// ListThemes returns the slugs of all themes registered for the global
// theme picker. The Mario theme is excluded because it is assistant-only.
func ListThemes() []string {
	out := make([]string, 0, len(themeRegistry))
	for name := range themeRegistry {
		if name == themeMario.Name {
			continue
		}
		out = append(out, name)
	}
	// Stable order for picker rendering.
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j] < out[j-1] {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// ApplyTheme switches the active global theme. Rebuilds the package-level
// styleSet so subsequent renders pick up the new colors. If the assistant
// theme is "follow_global", the assistant style set is rebuilt too.
func ApplyTheme(name string) error {
	t, ok := ResolveTheme(name)
	if !ok {
		return fmt.Errorf("unknown theme %q", name)
	}
	activeTheme = t
	styles = buildStyleSet(activeTheme)
	// If the assistant follows the global theme, rebuild it too.
	if activeAssistantTheme.Name != themeMario.Name {
		activeAssistantTheme = activeTheme
		assistantStyles = buildAssistantStyleSet(activeAssistantTheme)
	}
	return nil
}

// ApplyAssistantTheme switches the assistant screen's theme. The mode is
// either "mario" or AssistantInheritName ("follow_global"). On
// "follow_global" the assistant uses the current global theme.
func ApplyAssistantTheme(mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case themeMario.Name:
		activeAssistantTheme = themeMario
	case AssistantInheritName, "":
		activeAssistantTheme = activeTheme
	default:
		return fmt.Errorf("unknown assistant theme %q", mode)
	}
	assistantStyles = buildAssistantStyleSet(activeAssistantTheme)
	return nil
}
