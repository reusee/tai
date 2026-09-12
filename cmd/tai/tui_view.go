package main

import (
	"strings"

	"github.com/reusee/tai/taiui"
)

// The view builders below turn the TUI's raw state into elements. They
// read the TUI state directly and must be called with t.mu held;
// render() holds the lock while computing the displays and building the
// root. See TheoryOfTUI.

func outputTabLabel(finished bool, generating bool, handoff bool) (label string) {
	// The Output tab sits at index 1 after the tab-order swap (Tree 0 /
	// Output 1 / Logs 2), so the default label comes from tabNames[1].
	// See TheoryOfTUI.
	label = tabNames[1]
	switch {
	case finished:
		label = "Output (done)"
	case handoff:
		label = "Output (handoff...)"
	case generating:
		label = "Output (generating...)"
	}
	return
}

// treeContentWidth returns the Tree tab's content width: the
// scrollbar column is reserved at the right edge. The fold column
// lives inside the header text, so no left strip is reserved. See
// TheoryOfTreeTab.
func treeContentWidth(boxWidth int) int {
	return max(boxWidth-1, 1)
}

// wrappedDisplay computes the wrapped, colored lines of one expanded tab
// from its content and box: the Tree tab walks the pipeline's session
// tree with the current projection (see TheoryOfTreeTab), the Output tab
// renders its per-section projection (see TheoryOfOutputControls), and
// the Logs tab wraps through its cache. The tabs are indexed in display
// order: 0 Tree, 1 Output, 2 Logs. See TheoryOfTUI.
func wrappedDisplay(t *TUI, idx int, box taiui.Box) []taiui.Line {
	switch idx {
	case 0:
		base := panelStyle.BaseBG
		if t.tabs.Focus == 0 {
			base = panelStyle.FocusBG
		}
		return t.treeDisplay(treeContentWidth(box.Width()), base)
	case 1:
		// The control column reserves two cells at the panel's left
		// edge, and the scrollbar column one at the right. See
		// TheoryOfOutputControls.
		contentWidth := max(box.Width()-1, 1)
		if t.tabs.Expanded[1] && box.Width() > controlColumnWidth {
			contentWidth = max(box.Width()-controlColumnWidth-1, 1)
		}
		return t.outputDisplay(contentWidth)
	case 2:
		base := panelStyle.BaseBG
		if t.tabs.Focus == 2 {
			base = panelStyle.FocusBG
		}
		return t.logsCache.Plain(t.logs, max(box.Width()-1, 1), base)
	}
	return nil
}

// tuiPaneHeight returns the scroll view height of tab idx's box: every
// panel reserves its one-row label strip (taiui.PaneHeight), and an
// interactive Output tab (index 1) reserves one more row for the chat
// input bar at its bottom, so its scroll view is two rows shorter than
// the box — a non-interactive Output tab keeps the full pane height
// because the bar is not rendered. Every pane-height consumer — the
// scroll updates in render, page scrolling, and the section jumps —
// must use this helper so the view and the layout never disagree. See
// TheoryOfTUIChatInput.
func (t *TUI) tuiPaneHeight(idx int, box taiui.Box) int {
	if idx == 1 && t.interactive {
		return max(box.Height()-2, 1)
	}
	return taiui.PaneHeight(box)
}

// helpLines returns the help overlay's key-binding lines for this
// session: the full list in interactive sessions, and a variant without
// the input bar's entries in the others — Enter's input clause and the
// input-row click semantics describe a bar that is not rendered. The
// submit-glyph entry drops with the bar. See TheoryOfTUIChatInput.
func (t *TUI) helpLines() []string {
	if t.interactive {
		return tuiHelpLines
	}
	lines := make([]string, 0, len(tuiHelpLines))
	for _, line := range tuiHelpLines {
		key, _, _ := strings.Cut(line, "\t")
		switch key {
		case "enter":
			lines = append(lines, "enter\ttoggle the latest tree node's expansion")
		case "click":
			lines = append(lines, "click\tselect / toggle tab under cursor")
		case "input bar", "submit glyph":
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
	"c\tfold every section (Output tab) or node (Tree tab) of the focused tab; press again to restore; click a collapsed row to expand it",
	"v\tcycle the Tree tab's projection (all / events / summary / model / program / user / stream)",
	"enter\tsend the input line when focused; toggle the latest tree node's expansion otherwise",
	"click\tselect / toggle tab under cursor; click the input row to focus input",
	"output column\tclick ▸ / ▾ at a section's first row to collapse / expand it",
	"tree row\tclick 👉 on an attempt line to jump the Output tab to its output section; double-click a node to expand or collapse it",
	"tree column\tclick ▸ / ▾ on an expandable node's first row (or its first visible row when scrolled) to collapse / expand it",
	"title buttons\tclick the labels at an expanded tab title's right edge: Tree SwitchCollapse, Output UpDownCollapse, Logs SplitMouseHelpQuit",
	"wheel / drag\tscroll pane under cursor",
	"m\ttoggle mouse reporting (off: select & copy in the terminal)",
	"q / Ctrl-C\tquit (press again to confirm)",
	"input bar\tclick the bottom row to focus and type; esc or view-changing keys release",
	"?\ttoggle this help overlay",
	"submit glyph\tclick ↵ at the input bar's right end to send the typed line",
	"help close\tclick anywhere on the help overlay to close it",
}

func buildRoot(t *TUI, width, height int, displays [3][]taiui.Line) taiui.Element {
	boxes := t.tabs.Boxes(width, height)
	var elements []any
	for i := range tabNames {
		label := tabNames[i]
		if i == 0 {
			// The Tree tab's label states the current projection: the
			// v key cycles it. See TheoryOfTreeTab.
			label = t.treeTabLabel()
		} else if i == 1 {
			// The Output tab's label states the request lifecycle:
			// generating, handoff, or done. See TheoryOfTUI.
			label = outputTabLabel(t.finished, t.generating, t.handoff)
		}
		box := boxes[i]
		var inputBar taiui.Element
		var submitGlyphEl taiui.Element
		if i == 1 && t.interactive && t.tabs.Expanded[1] && box.Height() > 1 && box.Width() > 0 {
			// The chat input bar is the bottom row of the Output tab's
			// box: the panel above it shrinks by one row and the bar
			// spans the tab's width, so the bar is part of the tab's
			// layout rather than a screen-wide overlay. Interactive
			// sessions only — the bar is not rendered otherwise. See
			// TheoryOfTUIChatInput.
			inputBar = t.inputBar.Element(box, t.inputFocused, t.tabs.Focus == 1, inputBarStyle)
			if box.Width() >= 2 {
				// The submit glyph overlays the bar's right end: a press
				// there sends the typed line, colored by whether a
				// ChatInput call waits. See TheoryOfSessionActions.
				glyph, glyphColor := submitGlyph(t.inputResult != nil)
				submitGlyphEl = taiui.Text(glyph,
					taiui.Box{Top: box.Bottom - 1, Left: box.Right - 2, Bottom: box.Bottom, Right: box.Right},
					taiui.FGColor(glyphColor),
					taiui.Bold(true),
				)
			}
			box.Bottom--
		}
		var panel taiui.Element
		if i == 1 && t.tabs.Expanded[1] && box.Width() > controlColumnWidth && box.Height() > 0 {
			// The expanded Output tab reserves its leftmost column for
			// the section controls. See TheoryOfOutputControls.
			panel = t.outputPanelView(box, displays[1], label)
		} else {
			panel = taiui.TabPanel(
				box, tabNames[i], label,
				t.tabs.Expanded[i], t.tabs.Focus == i, t.tabs.Unseen[i],
				displays[i], t.scrolls[i], panelStyle,
			)
		}
		if panel != nil {
			elements = append(elements, panel)
		}
		if i == 0 && panel != nil && t.tabs.Expanded[0] {
			// The Tree tab's title row shows the loop and attempt of
			// the first visible entry, two cells from the box's left
			// edge; the two leading cells keep the title's rule. See
			// TheoryOfTreeTitleStatus.
			if status := t.treeTitleStatus(); status != "" {
				elements = append(elements, treeStatusElement(box, status, t.tabs.Focus == 0))
			}
			// The node under the pointer's fold column renders its fold
			// glyph reversed, the affordance that marks the press
			// target. See TheoryOfTreeTab.
			if el := t.treeFoldHoverElement(box, displays[0]); el != nil {
				elements = append(elements, el)
			}
		}
		if panel != nil && t.tabs.Expanded[i] {
			// The title row's operation buttons draw over the panel,
			// right-aligned before the two reserved cells. See
			// TheoryOfToolbars.
			if el := t.titleButtonsElement(i, box); el != nil {
				elements = append(elements, el)
			}
		}
		if inputBar != nil {
			elements = append(elements, inputBar)
			if submitGlyphEl != nil {
				elements = append(elements, submitGlyphEl)
			}
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

// outputPanelView builds the expanded Output tab: the full-width
// content panel whose content rows are indented past the control
// column, the column's background over the content rows, and each
// visible section's fold control with its content-type letter below.
// The title row spans the full box width and is not part of the
// column. The Output tab is the display's second tab (index 1) after
// the Tree/Output swap, so the panel, its focus state, and its scroll
// state use that index. The caller holds t.mu. See
// TheoryOfOutputControls.
func (t *TUI) outputPanelView(box taiui.Box, display []taiui.Line, label string) taiui.Element {
	panel := taiui.TabPanel(box, tabNames[1], label,
		t.tabs.Expanded[1], t.tabs.Focus == 1, t.tabs.Unseen[1], display, t.scrolls[1], panelStyle,
		taiui.ContentIndent(controlColumnWidth))
	base := panelStyle.BaseBG
	if t.tabs.Focus == 1 {
		base = panelStyle.FocusBG
	}
	// The box buildRoot hands over has already given up the label strip
	// and, in interactive sessions, the chat input bar row, so the
	// visible content rows are the box height minus the label strip
	// alone. The control rows keep their own window. See
	// TheoryOfOutputControls.
	paneHeight := max(box.Height()-1, 1)
	offset := taiui.ClampOffset(t.scrolls[1].Offset, len(display), paneHeight)
	// The control column is part of the content area: it paints the
	// content rows only, leaving the title row to the panel's centered
	// label. See TheoryOfOutputControls.
	children := []any{panel, taiui.Rect(
		taiui.Box{Top: box.Top + 1, Left: box.Left, Bottom: box.Bottom, Right: box.Left + controlColumnWidth},
		taiui.Fill(true),
		taiui.BGColor(base),
	)}
	for _, row := range t.outputControlRows(box, display, offset) {
		controls := t.sectionControls(row.section)
		if len(controls) == 0 {
			continue
		}
		text := controls[0].Glyph
		right := box.Left + controlColumnWidth
		// Hovering the control column on a control's row lays the
		// section's controls out horizontally, one Han-width slot
		// each, extending past the column. The strip needs mouse
		// reporting on: with reporting off the tracked position is
		// stale. See TheoryOfOutputControls.
		stripWidth := controlColumnWidth * len(controls)
		visibleRight := min(box.Left+stripWidth, box.Right)
		hovered := t.ctlHover && t.mouseReporting && t.ctlHoverY == row.row &&
			t.ctlHoverX >= box.Left && t.ctlHoverX < visibleRight
		if hovered && len(controls) > 1 {
			text = controlStripText(controls)
			right = visibleRight
		}
		children = append(children, taiui.Text(text, taiui.Box{
			Top: row.row, Left: box.Left, Bottom: row.row + 1, Right: right,
		}))
		// The control under the pointer renders reversed, the same
		// affordance the Tree fold column shows, so the press target is
		// visible before any press. See TheoryOfOutputControls.
		if hovered {
			slot := (t.ctlHoverX - box.Left) / controlColumnWidth
			slotLeft := box.Left + slot*controlColumnWidth
			children = append(children, taiui.Text(controls[slot].Glyph, taiui.Box{
				Top: row.row, Left: slotLeft, Bottom: row.row + 1, Right: slotLeft + controlColumnWidth,
			}, taiui.Reverse(true)))
		}
		// The section's content-type letter renders below its fold
		// glyph; a section with no second visible row shows only the
		// glyph. See TheoryOfOutputControls.
		if letterRow, ok := t.outputTypeRow(box, row, offset); ok {
			children = append(children, taiui.Text(t.outputSections[row.section].letter, taiui.Box{
				Top: letterRow, Left: box.Left, Bottom: letterRow + 1, Right: box.Left + controlColumnWidth,
			}))
		}
	}
	return taiui.Overlay(children...)
}

// inputBarStyle styles the chat input bar: the bar's background
// follows the panels' (none by default), a focused bar shows bright
// text, an unfocused bar dim text. UIStyle.apply re-derives it from
// the resolved configuration at startup. See TheoryOfTUIChatInput and
// taiui.TheoryOfInputBar.
var inputBarStyle = UIStyle{}.inputBarStyleOf()
