package main

import (
	"fmt"

	"github.com/reusee/tai/taiui"
)

// The view builders below turn the TUI's raw state into elements. They
// read the TUI state directly and must be called with t.mu held;
// render() holds the lock while computing the displays and building the
// root. See TheoryOfTUI.

// outputTabLabel returns the Output tab's title with the session-state
// hint: "Output (generating...)" while the model is actively generating,
// "Output (done)" after the session ends, and "Output" otherwise. The
// highlight result reports whether the title should be drawn in
// tabActiveLabelFg. Finished takes precedence over generating. See
// TheoryOfTUI.
func outputTabLabel(finished bool, generating bool) (label string, highlight bool) {
	label = tabNames[0]
	switch {
	case finished:
		label = "Output (done)"
	case generating:
		label = "Output (generating...)"
		highlight = true
	}
	return
}

// wrappedDisplay computes the wrapped, colored lines of one expanded tab
// from its content and box. The content width reserves one column for the
// scrollbar, matching the scroll's visible-width rendering. See
// TheoryOfTUI.
func wrappedDisplay(t *TUI, idx int, box taiui.Box) []taiui.Line {
	contentWidth := max(box.Width()-1, 1)
	switch idx {
	case 0:
		return taiui.WrapLinesColored(t.output.Lines(), contentWidth)
	case 1:
		return taiui.WrapLinesColored(t.signals, contentWidth)
	case 2:
		// The logs tab alternates line backgrounds derived from its tab
		// background, so consecutive log entries are visually distinct.
		// The base is the focused or unfocused tab background, whichever
		// the logs tab currently has. See taiui.PlainLines.
		base := panelStyle.BaseBG
		if t.tabs.Focus == 2 {
			base = panelStyle.FocusBG
		}
		return taiui.WrapLinesColored(
			taiui.PlainLines(t.logs.Lines(), base),
			contentWidth,
		)
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

// outputPanel builds the Output tab element: a one-row label strip with
// the session-state hint and a scroll view spanning the remaining rows,
// or a collapsed strip when the tab is collapsed. A degenerate box yields
// nil, which buildRoot skips.
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
	label, highlight := outputTabLabel(t.finished, t.generating)
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

// tuiHelpLines lists the TUI key bindings shown by the ? help overlay.
// The leading tabs align the descriptions when Text renders with the
// TabWidth spec.
var tuiHelpLines = []string{
	"1 / 2 / 3\tselect tab; pressing the focused tab again collapses it",
	"tab\tcycle focus among the expanded tabs",
	"s\ttoggle vertical / horizontal split",
	"up / down\tscroll the focused pane",
	"page up / down\tscroll the focused pane by a page",
	"home / end\tjump to the start / end of the focused pane",
	"[ / ]\tjump to the previous / next output section",
	"q / Ctrl-C\tquit (confirmation bar: press again to confirm)",
	"?\tthis help",
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
		root = taiui.Overlay(
			root,
			taiui.Rect(
				taiui.Box{
					Top:    height / 4,
					Left:   width / 4,
					Bottom: 3 * height / 4,
					Right:  3 * width / 4,
				},
				taiui.Border(true),
				taiui.Fill(true),
				taiui.BGColor(taiui.HexColor(0x202020)),
				taiui.Title(" Help "),
				taiui.Padding(1),
				taiui.Text(tuiHelpLines, taiui.TabWidth(18)),
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
