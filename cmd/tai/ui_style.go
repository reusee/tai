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
  default) and the palette label foregrounds, so the default
  interface paints no background and alternating log shades stay
  inert.
- apply re-derives the package-level style values (panelStyle and
  inputBarStyle) from the resolved configuration once at startup,
  before the TUI's first render; the display functions keep reading
  the package-level values, so no call site changes. The
  configuration is fixed for the session, runWithTUI applies it
  before any goroutine starts, and tests implicitly use the built-in
  defaults. This one-time init is the sanctioned use of the
  package-level style values.
- An empty background setting paints no background; an empty
  foreground setting keeps the built-in default. taiui.AltBG returns
  an unset base unchanged, so configuring a background re-activates
  the log alternation without further wiring.
- Tree tab node lines carry no built-in role colors: the configured
  tui.tree_colors rules decide the foreground of every tree line —
  the first rule whose every non-empty field (category, type, author)
  matches the node wins, and no match keeps the default foreground.
  The Output tab carries no role colors at all: a section's content
  type is stated by the full-width letter its control column draws
  (see TheoryOfOutputControls), so the style surface covers the
  panels, the input bar, and the tree rules only.
`

var _ configs.Config = UIStyle{}

// TreeColorRule is one rule of the tui.tree_colors configuration: a
// foreground color for the tree tab's node lines. Every non-empty
// field — category, type, author — must match the node for the rule
// to apply; an empty field matches any node. color accepts a W3C
// name or a "#rrggbb" hex value; an empty color keeps the default
// foreground.
type TreeColorRule struct {
	Category string `json:"category"`
	Type     string `json:"type"`
	Author   string `json:"author"`
	Color    string `json:"color"`
}

// UIStyle carries the terminal UI's configurable colors, decoded from
// the tui config section. Every field is a color string, a W3C name
// or a "#rrggbb" hex value. An empty background field paints no
// background; an empty foreground field keeps the built-in default.
// The Output tab's content carries no configurable role color: its
// content type is stated by the type letter in the control column.
// See TheoryOfUIStyle.
type UIStyle struct {
	TabUnfocusedBG   string          `json:"tab_unfocused_bg"`
	TabFocusedBG     string          `json:"tab_focused_bg"`
	LabelFG          string          `json:"label_fg"`
	FocusLabelFG     string          `json:"focus_label_fg"`
	UnseenDotColor   string          `json:"unseen_dot_color"`
	InputFocusedFG   string          `json:"input_focused_fg"`
	InputUnfocusedFG string          `json:"input_unfocused_fg"`
	TreeColors       []TreeColorRule `json:"tree_colors"`
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
// non-empty ones; fields already set survive. A rule list set by one
// config file survives the empty lists of the others.
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
	if s.UnseenDotColor == "" {
		s.UnseenDotColor = parsed.UnseenDotColor
	}
	if s.InputFocusedFG == "" {
		s.InputFocusedFG = parsed.InputFocusedFG
	}
	if s.InputUnfocusedFG == "" {
		s.InputUnfocusedFG = parsed.InputUnfocusedFG
	}
	if len(s.TreeColors) == 0 {
		s.TreeColors = parsed.TreeColors
	}
	return s
}

// apply re-derives the package-level style values from the resolved
// configuration. runWithTUI calls it once before the TUI starts, so
// every render reads the configured colors. See TheoryOfUIStyle.
func (s UIStyle) apply() {
	panelStyle = s.panelStyleOf()
	inputBarStyle = s.inputBarStyleOf()
	treeColorRules = s.treeColorRulesOf()
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

// treeColorRule is one resolved tree color rule: the color string is
// decoded once at startup, so rendering never parses settings per
// frame. An empty color keeps the default foreground.
type treeColorRule struct {
	category string
	nodeType string
	author   string
	color    taiui.Color
}

// treeColorRules holds the resolved tui.tree_colors rules; the zero
// value keeps every tree line in the default foreground.
var treeColorRules []treeColorRule

// treeColorRulesOf derives the tree tab color rules from the
// configuration: the color strings are decoded with the shared
// foreground parser, so an empty or unrecognized setting yields the
// default foreground.
func (s UIStyle) treeColorRulesOf() []treeColorRule {
	rules := make([]treeColorRule, 0, len(s.TreeColors))
	for _, r := range s.TreeColors {
		rules = append(rules, treeColorRule{
			category: r.Category,
			nodeType: r.Type,
			author:   r.Author,
			color:    parseFGColor(r.Color, taiui.NoColor),
		})
	}
	return rules
}

// UIStyle provides the zero style: the built-in defaults, no
// background. configs.Load forks the configured value over it.
func (Module) UIStyle() UIStyle {
	return UIStyle{}
}
