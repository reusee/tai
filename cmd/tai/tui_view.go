package main

import (
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
// from its content and box via the tab's taiui.WrapCache (incremental
// wrapping). See TheoryOfTUI.
func wrappedDisplay(t *TUI, idx int, box taiui.Box) []taiui.Line {
	contentWidth := max(box.Width()-1, 1)
	switch idx {
	case 0:
		return t.outputCache.Colored(t.output, contentWidth)
	case 1:
		return t.eventsCache.Lines(t.events, contentWidth)
	case 2:
		base := panelStyle.BaseBG
		if t.tabs.Focus == 2 {
			base = panelStyle.FocusBG
		}
		return t.logsCache.Plain(t.logs, contentWidth, base)
	}
	return nil
}

var tuiHelpLines = []string{
	"1 / 2 / 3\tselect tab; press focused tab again to collapse",
	"tab\tcycle focus among expanded tabs",
	"s\ttoggle vertical / horizontal split",
	"up / down\tscroll focused pane",
	"page up / down\tscroll focused pane by page",
	"home / end\tjump to start / end of focused pane",
	"[ / ]\tjump to previous / next section start or end",
	"click\tselect / toggle tab under cursor",
	"wheel / drag\tscroll pane under cursor",
	"q / Ctrl-C\tquit (press again to confirm)",
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
		panel := taiui.TabPanel(
			boxes[i], i+1, tabNames[i], label, highlight,
			t.tabs.Expanded[i], t.tabs.Focus == i,
			displays[i], t.scrolls[i], panelStyle,
		)
		if panel != nil {
			elements = append(elements, panel)
		}
	}
	root := taiui.Overlay(elements...)
	if t.showHelp {
		// The help overlay is centered over the tabs and lists the key
		// bindings. It is derived from state like the quit confirmation
		// bar: toggling showHelp re-renders the overlay.
		root = taiui.Overlay(root, taiui.HelpOverlay(tuiHelpLines, 16, width, height))
	}
	if t.quit.Pending() {
		// A pending quit confirmation draws a confirmation bar over the
		// bottom row of the screen, on top of every tab, so it is always
		// visible. See TheoryOfTUI.
		root = taiui.Overlay(root, taiui.QuitConfirmBar(width, height))
	}
	return root
}
