package main

import (
	"cuelang.org/go/cue"

	"github.com/gdamore/tcell/v3/color"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/taiui"
)

const TheoryOfUIStyle = `
UI style theory:
- The terminal UI's colors resolve from the dscope scope as one
  UIStyle value, decoded from the tui config section. The zero value
  is the built-in default: no background anywhere (the terminal
  default) and the historical palette foregrounds, so the default
  interface paints no background and alternating log shades stay
  inert.
- apply re-derives the package-level style values (panelStyle,
  inputBarStyle, and the role colors) from the resolved configuration
  once at startup, before the TUI's first render; the display
  functions keep reading the package-level values, so no call site
  changes. The configuration is fixed for the session, runWithTUI
  applies it before any goroutine starts, and tests implicitly use
  the built-in defaults. This one-time init is the sanctioned use of
  the package-level style values.
- An empty background setting paints no background; an empty
  foreground setting keeps the built-in default. taiui.AltBG returns
  an unset base unchanged, so configuring a background re-activates
  the log alternation without further wiring.
`

var _ configs.Config = UIStyle{}

// UIStyle carries the terminal UI's configurable colors, decoded from
// the tui config section. Every field is a color string, a W3C name
// or a "#rrggbb" hex value. An empty background field paints no
// background; an empty foreground field keeps the built-in default.
// See TheoryOfUIStyle.
type UIStyle struct {
	TabUnfocusedBG   string `json:"tab_unfocused_bg"`
	TabFocusedBG     string `json:"tab_focused_bg"`
	LabelFG          string `json:"label_fg"`
	FocusLabelFG     string `json:"focus_label_fg"`
	ActiveLabelFG    string `json:"active_label_fg"`
	UnseenDotColor   string `json:"unseen_dot_color"`
	UserColor        string `json:"user_color"`
	ToolColor        string `json:"tool_color"`
	SystemColor      string `json:"system_color"`
	LogColor         string `json:"log_color"`
	ThoughtColor     string `json:"thought_color"`
	InputFocusedFG   string `json:"input_focused_fg"`
	InputUnfocusedFG string `json:"input_unfocused_fg"`
}

// ConfigPaths registers the tui config section.
func (s UIStyle) ConfigPaths() []string {
	return []string{"tui"}
}

// HandleConfig merges the values of one config path into a style. The
// values arrive in loader root order, most local first, so the first
// non-empty setting of each field wins and more global files fill
// only the fields no local file set. See
// configs.TheoryOfConfigPathPrecedence.
func (s UIStyle) HandleConfig(path string, values []*cue.Value) (any, error) {
	merged := UIStyle{}
	for _, value := range values {
		var parsed UIStyle
		if err := value.Decode(&parsed); err != nil {
			return nil, err
		}
		merged = merged.fillFrom(parsed)
	}
	return &merged, nil
}

// fillFrom returns a copy whose still-empty fields take parsed's
// non-empty ones; fields already set survive.
func (s UIStyle) fillFrom(parsed UIStyle) UIStyle {
	if s.TabUnfocusedBG == "" {
		s.TabUnfocusedBG = parsed.TabUnfocusedBG
	}
	if s.TabFocusedBG == "" {
		s.TabFocusedBG = parsed.TabFocusedBG
	}
	if s.LabelFG == "" {
		s.LabelFG = parsed.LabelFG
	}
	if s.FocusLabelFG == "" {
		s.FocusLabelFG = parsed.FocusLabelFG
	}
	if s.ActiveLabelFG == "" {
		s.ActiveLabelFG = parsed.ActiveLabelFG
	}
	if s.UnseenDotColor == "" {
		s.UnseenDotColor = parsed.UnseenDotColor
	}
	if s.UserColor == "" {
		s.UserColor = parsed.UserColor
	}
	if s.ToolColor == "" {
		s.ToolColor = parsed.ToolColor
	}
	if s.SystemColor == "" {
		s.SystemColor = parsed.SystemColor
	}
	if s.LogColor == "" {
		s.LogColor = parsed.LogColor
	}
	if s.ThoughtColor == "" {
		s.ThoughtColor = parsed.ThoughtColor
	}
	if s.InputFocusedFG == "" {
		s.InputFocusedFG = parsed.InputFocusedFG
	}
	if s.InputUnfocusedFG == "" {
		s.InputUnfocusedFG = parsed.InputUnfocusedFG
	}
	return s
}

// apply re-derives the package-level style values from the resolved
// configuration. runWithTUI calls it once before the TUI starts, so
// every render reads the configured colors. See TheoryOfUIStyle.
func (s UIStyle) apply() {
	panelStyle = s.panelStyleOf()
	inputBarStyle = s.inputBarStyleOf()
	outputColorUserLine = s.userColor()
	outputColorToolLine = s.toolColor()
	outputColorSystemLine = s.systemColor()
	outputColorLogLine = s.logColor()
	outputColorThoughtLine = s.thoughtColor()
}

// panelStyleOf derives the panel style: empty background settings
// paint no background; empty foreground settings keep the built-in
// palette colors.
func (s UIStyle) panelStyleOf() taiui.PanelStyle {
	return taiui.PanelStyle{
		BaseBG:         parseBGColor(s.TabUnfocusedBG),
		FocusBG:        parseBGColor(s.TabFocusedBG),
		LabelFG:        parseFGColor(s.LabelFG, color.PaletteColor(8)),
		FocusLabelFG:   parseFGColor(s.FocusLabelFG, color.PaletteColor(15)),
		ActiveLabelFG:  parseFGColor(s.ActiveLabelFG, color.PaletteColor(int(tabActiveLabelFg))),
		UnseenDotColor: parseFGColor(s.UnseenDotColor, taiui.HexColor(0xd23b3b)),
	}
}

// inputBarStyleOf derives the chat input bar style: the bar's
// background follows the panels', and the foregrounds keep the
// built-in palette colors when unset.
func (s UIStyle) inputBarStyleOf() taiui.InputBarStyle {
	return taiui.InputBarStyle{
		BaseBG:      s.panelStyleOf().BaseBG,
		FocusBG:     s.panelStyleOf().FocusBG,
		FocusedFG:   parseFGColor(s.InputFocusedFG, color.PaletteColor(15)),
		UnfocusedFG: parseFGColor(s.InputUnfocusedFG, color.PaletteColor(8)),
	}
}

// userColor is the user input line color.
func (s UIStyle) userColor() taiui.Color {
	return parseFGColor(s.UserColor, color.PaletteColor(int(outputColorUser)))
}

// toolColor is the tool call line color.
func (s UIStyle) toolColor() taiui.Color {
	return parseFGColor(s.ToolColor, color.PaletteColor(int(outputColorTool)))
}

// systemColor is the system message line color.
func (s UIStyle) systemColor() taiui.Color {
	return parseFGColor(s.SystemColor, color.PaletteColor(int(outputColorSystem)))
}

// logColor is the log and event line color.
func (s UIStyle) logColor() taiui.Color {
	return parseFGColor(s.LogColor, color.PaletteColor(int(outputColorLog)))
}

// thoughtColor is the thought summary header color.
func (s UIStyle) thoughtColor() taiui.Color {
	return parseFGColor(s.ThoughtColor, color.PaletteColor(int(outputColorThought)))
}

// parseBGColor decodes a background setting: an empty string paints
// no background, the terminal default.
func parseBGColor(setting string) taiui.Color {
	if setting == "" {
		return taiui.NoColor
	}
	return color.GetColor(setting)
}

// parseFGColor decodes a foreground setting: an empty string keeps
// the built-in default; an unrecognized color name yields the
// terminal default foreground.
func parseFGColor(setting string, fallback taiui.Color) taiui.Color {
	if setting == "" {
		return fallback
	}
	return color.GetColor(setting)
}

// UIStyle provides the zero style: the built-in defaults, no
// background. configs.Load forks the configured value over it.
func (Module) UIStyle() UIStyle {
	return UIStyle{}
}
