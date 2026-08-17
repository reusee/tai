package main

import (
	"fmt"

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
// from its content and box using an incremental wrapping cache. See
// TheoryOfTUI.
func wrappedDisplay(t *TUI, idx int, box taiui.Box) []taiui.Line {
	contentWidth := max(box.Width()-1, 1)
	switch idx {
	case 0:
		if t.outputCache.width != contentWidth {
			t.outputCache = wrappedDisplayCache{width: contentWidth}
		}
		completed := t.output.CompletedLines()
		if len(completed) > t.outputCache.count {
			newLines := completed[t.outputCache.count:]
			t.outputCache.lines = taiui.WrapLinesColoredInto(newLines, contentWidth, t.outputCache.lines)
			t.outputCache.count = len(completed)
		}
		partial := t.output.Partial()
		if partial.Text == "" {
			return t.outputCache.lines
		}
		t.displayBuf[0] = append(t.displayBuf[0][:0], t.outputCache.lines...)
		t.displayBuf[0] = taiui.WrapLinesColoredInto([]taiui.Line{partial}, contentWidth, t.displayBuf[0])
		return t.displayBuf[0]

	case 1:
		if t.summaryCache.width != contentWidth {
			t.summaryCache = wrappedDisplayCache{width: contentWidth}
		}
		if len(t.signals) > t.summaryCache.count {
			newSignals := t.signals[t.summaryCache.count:]
			t.summaryCache.lines = taiui.WrapLinesColoredInto(newSignals, contentWidth, t.summaryCache.lines)
			t.summaryCache.count = len(t.signals)
		}
		return t.summaryCache.lines

	case 2:
		base := panelStyle.BaseBG
		if t.tabs.Focus == 2 {
			base = panelStyle.FocusBG
		}
		if t.logsCache.width != contentWidth || t.logsCache.base != base {
			t.logsCache = wrappedDisplayCache{width: contentWidth, base: base}
		}
		completed := t.logs.CompletedLines()
		if len(completed) > t.logsCache.count {
			newLogs := completed[t.logsCache.count:]
			t.logsCache.lines = taiui.WrapPlainLinesInto(newLogs, base, contentWidth, t.logsCache.count, t.logsCache.lines)
			t.logsCache.count = len(completed)
		}
		partial := t.logs.Partial()
		if partial == "" {
			return t.logsCache.lines
		}
		bg := base
		if len(completed)%2 == 1 {
			bg = taiui.AltBG(base)
		}
		t.displayBuf[2] = append(t.displayBuf[2][:0], t.logsCache.lines...)
		t.displayBuf[2] = taiui.WrapLinesColoredInto([]taiui.Line{{Text: partial, BGColor: bg, Color: taiui.NoColor}}, contentWidth, t.displayBuf[2])
		return t.displayBuf[2]
	}
	return nil
}

// transitionBoundaries returns the display-line indices where the
// Output's sections change: a display line whose color differs from the
// previous line's color. The Output tab colors each section by its role
// and thinking state (captureContent colors thoughts distinctly), so a
// color change is exactly a role change or a thought/non-thought change.
// WrapLinesColored carries a source line's color onto every wrapped
// display line, so transitions are identified in display coordinates,
// matching the scroll offsets. The first display line is never a
// boundary: there is no previous section to transition from, so the
// backward jump in jumpToTransition falls back to the very beginning of
// the content to reach the first section's start. See TheoryOfTUI.
func transitionBoundaries(display []taiui.Line) []int {
	var indices []int
	for i := 1; i < len(display); i++ {
		if display[i].Color != display[i-1].Color {
			indices = append(indices, i)
		}
	}
	return indices
}

func outputPanel(t *TUI, box taiui.Box, lines []taiui.Line) taiui.Element {
	if box.Width() <= 0 || box.Height() <= 0 {
		return nil
	}
	if !t.tabs.Expanded[0] {
		return taiui.CollapsedPanel(
			box,
			fmt.Sprintf("%d %s", 1, tabNames[0]),
			t.tabs.Focus == 0,
			panelStyle,
		)
	}
	label, highlight := outputTabLabel(t.finished, t.generating, t.handoff)
	return taiui.Panel(
		box,
		label,
		highlight,
		lines,
		t.scrolls[0].Offset,
		t.tabs.Focus == 0,
		t.scrolls[0].Follow,
		panelStyle,
	)
}

// summaryPanel builds the Summary tab element. See outputPanel.
func summaryPanel(t *TUI, box taiui.Box, lines []taiui.Line) taiui.Element {
	if box.Width() <= 0 || box.Height() <= 0 {
		return nil
	}
	if !t.tabs.Expanded[1] {
		return taiui.CollapsedPanel(
			box,
			fmt.Sprintf("%d %s", 2, tabNames[1]),
			t.tabs.Focus == 1,
			panelStyle,
		)
	}
	return taiui.Panel(
		box,
		tabNames[1],
		false,
		lines,
		t.scrolls[1].Offset,
		t.tabs.Focus == 1,
		t.scrolls[1].Follow,
		panelStyle,
	)
}

// logsPanel builds the Logs tab element. See outputPanel.
func logsPanel(t *TUI, box taiui.Box, lines []taiui.Line) taiui.Element {
	if box.Width() <= 0 || box.Height() <= 0 {
		return nil
	}
	if !t.tabs.Expanded[2] {
		return taiui.CollapsedPanel(
			box,
			fmt.Sprintf("%d %s", 3, tabNames[2]),
			t.tabs.Focus == 2,
			panelStyle,
		)
	}
	return taiui.Panel(
		box,
		tabNames[2],
		false,
		lines,
		t.scrolls[2].Offset,
		t.tabs.Focus == 2,
		t.scrolls[2].Follow,
		panelStyle,
	)
}

var tuiHelpLines = []string{
	"1 / 2 / 3\tselect tab; press focused tab again to collapse",
	"tab\tcycle focus among expanded tabs",
	"s\ttoggle vertical / horizontal split",
	"up / down\tscroll focused pane",
	"page up / down\tscroll focused pane by page",
	"home / end\tjump to start / end of focused pane",
	"[ / ]\tjump to previous / next section",
	"click\tselect / toggle tab under cursor",
	"wheel / drag\tscroll pane under cursor",
	"q / Ctrl-C\tquit (press again to confirm)",
	"?\ttoggle this help overlay",
}

func buildRoot(t *TUI, width, height int, displays [3][]taiui.Line) taiui.Element {
	boxes := t.tabs.Boxes(width, height)
	var elements []any
	for _, panel := range []taiui.Element{
		outputPanel(t, boxes[0], displays[0]),
		summaryPanel(t, boxes[1], displays[1]),
		logsPanel(t, boxes[2], displays[2]),
	} {
		if panel != nil {
			elements = append(elements, panel)
		}
	}
	root := taiui.Overlay(elements...)
	if t.showHelp {
		// The help overlay is centered over the tabs and lists the key
		// bindings. It is derived from state like the quit confirmation
		// bar: toggling showHelp re-renders the overlay.
		helpHeight := min(len(tuiHelpLines)+4, max(height-2, 1))
		helpWidth := min(72, max(width-4, 1))
		top := max((height-helpHeight)/2, 0)
		left := max((width-helpWidth)/2, 0)
		root = taiui.Overlay(
			root,
			taiui.Rect(
				taiui.Box{
					Top:    top,
					Left:   left,
					Bottom: top + helpHeight,
					Right:  left + helpWidth,
				},
				taiui.Border(true),
				taiui.Fill(true),
				taiui.BGColor(taiui.HexColor(0x202020)),
				taiui.Title(" Help "),
				taiui.Padding(1),
				taiui.Text(tuiHelpLines, taiui.TabWidth(16)),
			),
		)
	}
	if t.confirmQuit {
		// A pending quit confirmation draws a confirmation bar over the
		// bottom row of the screen, on top of every tab, so it is always
		// visible. See TheoryOfTUI.
		root = taiui.Overlay(
			root,
			taiui.Rect(
				taiui.Box{Top: height - 1, Left: 0, Bottom: height, Right: width},
				taiui.Fill(true),
				taiui.BGColor(taiui.HexColor(0x800000)),
				taiui.Bold(true),
				taiui.Text(" Quit? Press q again to confirm, any other key to cancel "),
			),
		)
	}
	return root
}
