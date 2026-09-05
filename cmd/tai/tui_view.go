package main

import (
	"strings"

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

// treeContentWidth returns the Tree tab's content width: the
// scrollbar column is reserved at the right edge, and the status
// column at the left when the expanded tab is wide enough to carry
// it. See TheoryOfTreeTab.
func treeContentWidth(expanded bool, boxWidth int) int {
	contentWidth := max(boxWidth-1, 1)
	if expanded && boxWidth > controlColumnWidth {
		contentWidth = max(boxWidth-controlColumnWidth-1, 1)
	}
	return contentWidth
}

// wrappedDisplay computes the wrapped, colored lines of one expanded tab
// from its content and box: the Output tab renders its per-section
// projection (see TheoryOfOutputControls), the Logs tab wraps through
// its cache, and the Tree tab walks the pipeline's session tree with
// the current projection (see TheoryOfTreeTab). See TheoryOfTUI.
func wrappedDisplay(t *TUI, idx int, box taiui.Box) []taiui.Line {
	switch idx {
	case 0:
		// The control column reserves two cells at the panel's left
		// edge, and the scrollbar column one at the right. See
		// TheoryOfOutputControls.
		contentWidth := max(box.Width()-1, 1)
		if t.tabs.Expanded[0] && box.Width() > controlColumnWidth {
			contentWidth = max(box.Width()-controlColumnWidth-1, 1)
		}
		return t.outputDisplay(contentWidth)
	case 1:
		base := panelStyle.BaseBG
		if t.tabs.Focus == 1 {
			base = panelStyle.FocusBG
		}
		return t.treeDisplay(treeContentWidth(t.tabs.Expanded[1], box.Width()), base)
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
	"c\tcollapse all sections; press again to restore; click a collapsed section to expand it",
	"v\tcycle the Tree tab's projection (all / events / summary / model / program / user)",
	"view menu\tpick the Tree tab's projection from the View menu",
	"enter\tsend the input line when focused; toggle the latest tree node's expansion otherwise",
	"click\tselect / toggle tab under cursor; click the input row to focus input",
	"output column\tclick ▸ / ▾ at a section's first row to collapse / expand it",
	"tree row\tclick 👉 on an attempt-start line to jump the Output tab to its output section; click a node to expand or collapse it",
	"tree column\tclick ▸ / ▾ on an expandable node's first row to collapse / expand it",
	"wheel / drag\tscroll pane under cursor",
	"m\ttoggle mouse reporting (off: select & copy in the terminal)",
	"q / Ctrl-C\tquit (press again to confirm)",
	"input bar\tclick the bottom row to focus and type; esc or view-changing keys release",
	"?\ttoggle this help overlay",
	"menu bar\tclick a category on the top row to open its menu; click an item to run it; Quit confirms on the second click",
	"submit glyph\tclick ↵ at the input bar's right end to send the typed line",
	"help close\tclick anywhere on the help overlay to close it",
}

func buildRoot(t *TUI, width, height int, displays [3][]taiui.Line) taiui.Element {
	boxes := t.tabs.Boxes(width, height)
	var elements []any
	for i := range tabNames {
		label, highlight := tabNames[i], false
		if i == 0 {
			label, highlight = outputTabLabel(t.finished, t.generating, t.handoff)
		} else if i == 1 {
			// The Tree tab's label states the current projection: the
			// v key cycles it. See TheoryOfTreeTab.
			label = t.treeTabLabel()
		}
		box := boxes[i]
		var inputBar taiui.Element
		var submitGlyphEl taiui.Element
		if i == 0 && t.interactive && t.tabs.Expanded[0] && box.Height() > 1 && box.Width() > 0 {
			// The chat input bar is the bottom row of the Output tab's
			// box: the panel above it shrinks by one row and the bar
			// spans the tab's width, so the bar is part of the tab's
			// layout rather than a screen-wide overlay. Interactive
			// sessions only — the bar is not rendered otherwise. See
			// TheoryOfTUIChatInput.
			inputBar = t.inputBar.Element(box, t.inputFocused, t.tabs.Focus == 0, inputBarStyle)
			if box.Width() >= 2 {
				// The submit glyph overlays the bar's right end: a press
				// there sends the typed line, colored by whether a
				// ChatInput call waits. See TheoryOfControlBar.
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
		if i == 0 && t.tabs.Expanded[0] && box.Width() > controlColumnWidth && box.Height() > 0 {
			// The expanded Output tab reserves its leftmost column for
			// the section controls. See TheoryOfOutputControls.
			panel = t.outputPanelView(box, displays[0], label, highlight)
		} else if i == 1 && t.tabs.Expanded[1] && box.Width() > controlColumnWidth && box.Height() > 0 {
			// The expanded Tree tab reserves its leftmost column for
			// the node fold controls. See TheoryOfTreeTab.
			panel = t.treePanelView(box, displays[1], label, highlight)
		} else {
			panel = taiui.TabPanel(
				box, tabNames[i], label, highlight,
				t.tabs.Expanded[i], t.tabs.Focus == i, t.tabs.Unseen[i],
				displays[i], t.scrolls[i], panelStyle,
			)
		}
		if panel != nil {
			elements = append(elements, panel)
		}
		if inputBar != nil {
			elements = append(elements, inputBar)
			if submitGlyphEl != nil {
				elements = append(elements, submitGlyphEl)
			}
		}
	}
	root := taiui.Overlay(elements...)
	if t.tabs.TopInset > 0 {
		// The menu bar draws over the top row the inset reserves: every
		// keyboard action without a pointer path is reachable through a
		// category dropdown. The row carries only the category titles,
		// so it keeps the terminal default background. See
		// TheoryOfControlBar.
		root = taiui.Overlay(root, menuBarElement(width, t.openMenu, t.menuHoverTitleLocked()))
	}
	if t.showHelp {
		// The help overlay is centered over the tabs and lists the key
		// bindings. It is derived from state like the quit confirmation
		// bar: toggling showHelp re-renders the overlay.
		root = taiui.Overlay(root, taiui.HelpOverlay(t.helpLines(), 16, width, height))
	}
	if t.openMenu >= 0 {
		// The open menu's dropdown covers the tabs below its title. See
		// TheoryOfControlBar.
		if dropdown := menuDropdownElement(width, height, t.openMenu, t.menuHoverItemLocked()); dropdown != nil {
			root = taiui.Overlay(root, dropdown)
		}
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
// column, the column's background over the content rows, and one
// control glyph per visible section. The title row spans the full box
// width and is not part of the column. The caller holds t.mu. See
// TheoryOfOutputControls.
func (t *TUI) outputPanelView(box taiui.Box, display []taiui.Line, label string, highlight bool) taiui.Element {
	panel := taiui.TabPanel(box, tabNames[0], label, highlight,
		t.tabs.Expanded[0], t.tabs.Focus == 0, t.tabs.Unseen[0], display, t.scrolls[0], panelStyle,
		taiui.ContentIndent(controlColumnWidth))
	base := panelStyle.BaseBG
	if t.tabs.Focus == 0 {
		base = panelStyle.FocusBG
	}
	// The control column is part of the content area: it paints the
	// content rows only, leaving the title row to the panel's centered
	// label. See TheoryOfOutputControls.
	children := []any{panel, taiui.Rect(
		taiui.Box{Top: box.Top + 1, Left: box.Left, Bottom: box.Bottom, Right: box.Left + controlColumnWidth},
		taiui.Fill(true),
		taiui.BGColor(base),
	)}
	offset := taiui.ClampOffset(t.scrolls[0].Offset, len(display), t.tuiPaneHeight(0, box))
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
		if t.ctlHover && t.mouseReporting && len(controls) > 1 && t.ctlHoverY == row.row &&
			t.ctlHoverX >= box.Left && t.ctlHoverX < box.Left+stripWidth {
			text = controlStripText(controls)
			right = min(box.Left+stripWidth, box.Right)
		}
		children = append(children, taiui.Text(text, taiui.Box{
			Top: row.row, Left: box.Left, Bottom: row.row + 1, Right: right,
		}))
	}
	return taiui.Overlay(children...)
}

// treePanelView builds the expanded Tree tab: the full-width content
// panel whose content rows are indented past the status column, the
// column's background over the content rows, and one fold glyph per
// visible expandable node. The title row spans the full box width and
// is not part of the column. The caller holds t.mu. See
// TheoryOfTreeTab.
func (t *TUI) treePanelView(box taiui.Box, display []taiui.Line, label string, highlight bool) taiui.Element {
	panel := taiui.TabPanel(box, tabNames[1], label, highlight,
		t.tabs.Expanded[1], t.tabs.Focus == 1, t.tabs.Unseen[1], display, t.scrolls[1], panelStyle,
		taiui.ContentIndent(controlColumnWidth))
	base := panelStyle.BaseBG
	if t.tabs.Focus == 1 {
		base = panelStyle.FocusBG
	}
	children := []any{panel, taiui.Rect(
		taiui.Box{Top: box.Top + 1, Left: box.Left, Bottom: box.Bottom, Right: box.Left + controlColumnWidth},
		taiui.Fill(true),
		taiui.BGColor(base),
	)}
	offset := taiui.ClampOffset(t.scrolls[1].Offset, len(display), t.tuiPaneHeight(1, box))
	for _, row := range t.treeControlRows(box, display, offset) {
		n, ok := t.treeView.Node(row.name)
		if !ok {
			continue
		}
		controls := t.treeNodeControls(n)
		if len(controls) == 0 {
			continue
		}
		children = append(children, taiui.Text(controls[0].Glyph, taiui.Box{
			Top: row.row, Left: box.Left, Bottom: row.row + 1, Right: box.Left + controlColumnWidth,
		}))
	}
	return taiui.Overlay(children...)
}

// inputBarStyle styles the chat input bar: the bar's background
// follows the panels' (none by default), a focused bar shows bright
// text, an unfocused bar dim text. UIStyle.apply re-derives it from
// the resolved configuration at startup. See TheoryOfTUIChatInput and
// taiui.TheoryOfInputBar.
var inputBarStyle = UIStyle{}.inputBarStyleOf()
