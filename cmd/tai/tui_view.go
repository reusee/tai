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

// buildRoot composes the panel elements and the optional quit-confirmation
// bar into the root element. The confirmation bar draws over every tab
// when a quit confirmation is pending, so it is always visible. See
// TheoryOfTUI.
func buildRoot(t *TUI, width, height int, displays [3][]taiui.Line) taiui.Root {
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
	root := taiui.Root{Element: taiui.Overlay(elements...)}
	if t.confirmQuit {
		// A pending quit confirmation draws a confirmation bar over the
		// bottom row of the screen, on top of every tab, so it is always
		// visible. See TheoryOfTUI.
		root.Element = taiui.Overlay(
			root.Element,
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
