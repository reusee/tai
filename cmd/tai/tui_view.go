package main

import (
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v3/color"
	"github.com/reusee/tai/taiui"
)

// The view builders below turn the TUI's raw state into elements. They
// read the TUI state directly and must be called with t.mu held;
// render() holds the lock while computing the displays and building the
// root. See TheoryOfTUI.

func outputTabLabel(finished bool, generating bool, handoff bool) (label string, highlight bool) {
	label = tabNames[0]
	switch {
	case finished:
		label = "Output (done)"
	case handoff:
		label = "Output (handoff...)"
		highlight = true
	case generating:
		label = "Output (generating...)"
		highlight = true
	}
	return
}

// wrappedDisplay computes the wrapped, colored lines of one expanded tab
// from its content and box: the Output and Logs tabs wrap incrementally
// through their taiui.WrapCache, the Events tab walks its event tree
// (taiui.EventTree). See TheoryOfTUI and TheoryOfEventTree.
func wrappedDisplay(t *TUI, idx int, box taiui.Box) []taiui.Line {
	contentWidth := max(box.Width()-1, 1)
	switch idx {
	case 0:
		return t.outputCache.Colored(t.output, contentWidth)
	case 1:
		base := panelStyle.BaseBG
		if t.tabs.Focus == 1 {
			base = panelStyle.FocusBG
		}
		return t.events.Display(contentWidth, base)
	case 2:
		base := panelStyle.BaseBG
		if t.tabs.Focus == 2 {
			base = panelStyle.FocusBG
		}
		return t.logsCache.Plain(t.logs, contentWidth, base)
	}
	return nil
}

// tuiPaneHeight returns the scroll view height of tab idx's box: every
// panel reserves its one-row label strip (taiui.PaneHeight), and an
// interactive Output tab reserves one more row for the chat input bar
// at its bottom, so its scroll view is two rows shorter than the box —
// a non-interactive Output tab keeps the full pane height because the
// bar is not rendered. Every pane-height consumer — the scroll updates
// in render, page scrolling, and the section jumps — must use this
// helper so the view and the layout never disagree. See
// TheoryOfTUIChatInput.
func (t *TUI) tuiPaneHeight(idx int, box taiui.Box) int {
	if idx == 0 && t.interactive {
		return max(box.Height()-2, 1)
	}
	return taiui.PaneHeight(box)
}

// helpLines returns the help overlay's key-binding lines for this
// session: the full list in interactive sessions, and a variant without
// the input bar's entries in the others — Enter's input clause and the
// input-row click semantics describe a bar that is not rendered. See
// TheoryOfTUIChatInput.
func (t *TUI) helpLines() []string {
	if t.interactive {
		return tuiHelpLines
	}
	lines := make([]string, 0, len(tuiHelpLines))
	for _, line := range tuiHelpLines {
		key, _, _ := strings.Cut(line, "\t")
		switch key {
		case "enter":
			lines = append(lines, "enter\ttoggle latest handoff summary")
		case "click":
			lines = append(lines, "click\tselect / toggle tab under cursor")
		case "input bar":
			// The bar is not rendered in non-interactive sessions.
		default:
			lines = append(lines, line)
		}
	}
	return lines
}

var tuiHelpLines = []string{
	"1 / 2 / 3\tselect tab; press focused tab again to collapse",
	"tab\tcycle focus among expanded tabs",
	"s\ttoggle vertical / horizontal split",
	"up / down\tscroll focused pane",
	"page up / down\tscroll focused pane by page",
	"home / end\tjump to start / end of focused pane",
	"[ / ]\tjump to previous / next section start or end",
	"enter\tsend the input line when focused; toggle latest handoff otherwise",
	"click\tselect / toggle tab under cursor; click the input row to focus input",
	"wheel / drag\tscroll pane under cursor",
	"m\ttoggle mouse reporting (off: select & copy in the terminal)",
	"q / Ctrl-C\tquit (press again to confirm)",
	"input bar\tclick the bottom row to focus and type; esc or view-changing keys release",
	"?\ttoggle this help overlay",
}

func buildRoot(t *TUI, width, height int, displays [3][]taiui.Line) taiui.Element {
	boxes := t.tabs.Boxes(width, height)
	var elements []any
	for i := range tabNames {
		label, highlight := tabNames[i], false
		if i == 0 {
			label, highlight = outputTabLabel(t.finished, t.generating, t.handoff)
		}
		box := boxes[i]
		var inputBar taiui.Element
		if i == 0 && t.interactive && t.tabs.Expanded[0] && box.Height() > 1 && box.Width() > 0 {
			// The chat input bar is the bottom row of the Output tab's
			// box: the panel above it shrinks by one row and the bar
			// spans the tab's width, so the bar is part of the tab's
			// layout rather than a screen-wide overlay. Interactive
			// sessions only — the bar is not rendered otherwise. See
			// TheoryOfTUIChatInput.
			inputBar = chatInputBar(box, t.tabs.Focus == 0, t.inputFocused, t.inputPrompt, t.inputLine, t.inputCursor)
			box.Bottom--
		}
		panel := taiui.TabPanel(
			box, tabNames[i], label, highlight,
			t.tabs.Expanded[i], t.tabs.Focus == i, t.tabs.Unseen[i],
			displays[i], t.scrolls[i], panelStyle,
		)
		if panel != nil {
			elements = append(elements, panel)
		}
		if inputBar != nil {
			elements = append(elements, inputBar)
		}
	}
	root := taiui.Overlay(elements...)
	if t.showHelp {
		// The help overlay is centered over the tabs and lists the key
		// bindings. It is derived from state like the quit confirmation
		// bar: toggling showHelp re-renders the overlay.
		root = taiui.Overlay(root, taiui.HelpOverlay(t.helpLines(), 16, width, height))
	}
	if t.quit.Pending() {
		// A pending quit confirmation draws a confirmation bar over the
		// bottom row of the screen, on top of every tab, so it is always
		// visible. See TheoryOfTUI.
		root = taiui.Overlay(root, taiui.QuitConfirmBar(width, height))
	}
	return root
}

func chatInputBar(box taiui.Box, tabFocused, focused bool, prompt string, line []rune, cursor int) taiui.Element {
	if prompt == "" {
		prompt = ">> "
	}
	text := prompt + string(line)
	// A focused bar shows the bright text and the terminal cursor at
	// the editing position (the Input element records it in the frame);
	// an unfocused bar keeps the text in a dimmer foreground so the
	// focus state is visible at a glance. The bar's background follows
	// the Output tab's focus state, so an unfocused tab shows the bar
	// in the same unfocused background as the panel above it. See
	// TheoryOfTUIChatInput.
	var input taiui.Element
	if focused {
		input = taiui.Input(text, utf8.RuneCountInString(prompt)+cursor, taiui.FGColor(color.PaletteColor(15)))
	} else {
		input = taiui.Text(text, taiui.FGColor(color.PaletteColor(8)))
	}
	bg := panelStyle.BaseBG
	if tabFocused {
		bg = panelStyle.FocusBG
	}
	return taiui.Rect(
		taiui.Box{Top: box.Bottom - 1, Left: box.Left, Bottom: box.Bottom, Right: box.Right},
		taiui.Fill(true),
		taiui.BGColor(bg),
		input,
	)
}
