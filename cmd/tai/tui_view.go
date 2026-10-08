package main

import (
	"strings"
	"time"

	"github.com/reusee/tai/taiui"
)

// The view builders below turn the TUI's raw state into elements. They
// read the TUI state directly and must be called with t.mu held;
// render() holds the lock while computing the displays and building the
// root. See TheoryOfTUI.

// outputStatus renders the Output tab's left status: the request
// lifecycle state as one key value segment — state done, state handoff,
// state processing <kind> <elapsed>, or state generating — or "" when
// none applies. A component's block processing is observed through the
// display decorator, because the session tree carries the block-result
// nodes only after the work returns. The processing segment carries the
// elapsed stopwatch fragment, so the user sees how long a component has
// been working. See TheoryOfTUIBlockProcessing and
// TheoryOfTabTitleStatus.
func outputStatus(finished bool, generating bool, handoff bool, processing string, processingElapsed time.Duration) string {
	value := ""
	switch {
	case finished:
		value = "done"
	case handoff:
		value = "handoff"
	case processing != "":
		// Processing outranks a stale generating hint: a request that
		// ended without a finish node leaves generating set while a
		// component still works. See TheoryOfTUIBlockProcessing.
		value = "processing " + processing + " " + formatTreeElapsed(processingElapsed)
	case generating:
		value = "generating"
	}
	if value == "" {
		return ""
	}
	return "state " + value
}

// treeContentWidth returns the Tree tab's content width: the
// scrollbar column is reserved at the right edge. The fold column
// lives inside the header text, so no left strip is reserved. See
// TheoryOfTreeTab.
func treeContentWidth(boxWidth int) int {
	return max(boxWidth-1, 1)
}

// statusSeparator joins the key value segments of a tab title's left
// status. See TheoryOfTabTitleStatus.
const statusSeparator = " / "

// statusText joins a tab title's left status segments with
// statusSeparator, dropping the empty ones so a state that is not
// available contributes nothing. Every non-empty segment is one
// "key value" pair; the tab's name never appears here — it stays fixed in
// the title's center. See TheoryOfTabTitleStatus.
func statusText(segments ...string) string {
	var out string
	for _, s := range segments {
		if s == "" {
			continue
		}
		if out != "" {
			out += statusSeparator
		}
		out += s
	}
	return out
}

// titleLabelStart returns the display column where a tab title's
// centered fixed name begins, mirroring the panel's centering formula.
// The left status ends there, so the mutable state never paints over the
// title's fixed name. See TheoryOfTabTitleStatus.
func titleLabelStart(box taiui.Box, label string) int {
	options := taiui.DisplayWidthOptions()
	return box.Left + max((box.Width()-options.String(label))/2, 0)
}

func wrappedDisplay(t *TUI, idx int, box taiui.Box) []taiui.Line {
	kind, ok := t.tabKindAt(idx)
	if !ok {
		return nil
	}
	// A searching tree pane reads the inset box so its display wraps
	// at the shifted panel's width and the scroll math uses the same
	// geometry the hit tests read. See TheoryOfTreeSearch.
	box = t.tabRenderBox(idx, box)
	base := panelStyle.BaseBG
	if t.tabs.Focus == idx {
		base = panelStyle.FocusBG
	}
	switch kind {
	case tabTree:
		return t.treeDisplay(treeContentWidth(box.Width()), base)
	case tabPlan:
		// The Plan tab renders the current loop's plan subtree with the
		// Tree tab's own rendering. See TheoryOfTUIDynamicPlanTab.
		return t.paneTreeDisplay(tabPlan, treeContentWidth(box.Width()), base)
	case tabOutput:
		// The control column reserves two cells at the panel's left
		// edge, and the scrollbar column one at the right. See
		// TheoryOfOutputControls.
		contentWidth := max(box.Width()-1, 1)
		if t.tabs.Expanded[idx] && box.Width() > controlColumnWidth {
			contentWidth = max(box.Width()-controlColumnWidth-1, 1)
		}
		return t.outputDisplay(contentWidth)
	case tabLogs:
		return t.logsCache.Plain(t.logs, max(box.Width()-1, 1), base)
	}
	return nil
}

// tabStatus renders a tab's left status: the tab's mutable state as key
// value segments, or "" for a tab that carries no state. The title's
// center keeps the tab's fixed name. See TheoryOfTabTitleStatus.
func (t *TUI) tabStatus(kind tuiTab) string {
	switch kind {
	case tabTree:
		return t.treeStatus()
	case tabPlan:
		return t.planStatus()
	case tabOutput:
		return outputStatus(t.finished, t.generating, t.handoff, t.blockProcessing, t.blockProcessingElapsedLocked())
	}
	return ""
}

func (t *TUI) tuiPaneHeight(idx int, box taiui.Box) int {
	if t.interactive && idx == t.tabIndex(tabOutput) {
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
	"1 / 2 / 3 / 4\tselect tab; press focused tab again to collapse",
	"tab\tcycle focus among expanded tabs",
	"s\ttoggle vertical / horizontal split",
	"up / down\tscroll focused pane",
	"page up / down\tscroll focused pane by page",
	"home / end\tjump to start / end of focused pane",
	"[ / ]\tjump to previous / next section start or end",
	"c\tfold every section (Output tab) or node (Tree and Plan tabs) of the focused tab; press again to restore; double-click a collapsed row to expand it",
	"v\tcycle the Tree tab's projection (all / events / summary / model / program / user / stream)",
	"enter\tsend the input line when focused; toggle the latest tree node's expansion otherwise",
	"click\tselect / toggle tab under cursor; click the input row to focus input",
	"output column\tclick ▸ / ▾ at a section's first row to collapse / expand it",
	"tree row\tclick 👉 on an attempt line to jump the Output tab to its output section; double-click the text to expand or collapse a node (a single text press is inert)",
	"tree column\tclick ▸ / ▾ on an expandable node's first row (or its first visible row when scrolled) to collapse / expand it",
	"title toolbar\tclick the icon at an expanded tab title's right edge to show its buttons — Tree Switch Collapse, Plan Collapse, Output Up Down Collapse, Logs Split Mouse Help Quit; an action press runs the action and keeps the toolbar open, so press the icon again or anywhere else to hide it",
	"wheel / drag\tscroll pane under cursor",
	"m\ttoggle mouse reporting (off: select & copy in the terminal)",
	"q / Ctrl-C\tquit (press again to confirm)",
	"input bar\tclick the bottom row to focus and type; esc or view-changing keys release",
	"?\ttoggle this help overlay",
	"submit glyph\tclick ↵ at the input bar's right end to send the typed line",
	"help close\tclick anywhere on the help overlay to close it",
}

func buildRoot(t *TUI, width, height int, displays [][]taiui.Line) taiui.Element {
	boxes := t.tabs.Boxes(width, height)
	var elements []any
	for _, kind := range t.tabKinds() {
		i := t.tabIndex(kind)
		if i < 0 || i >= len(boxes) {
			continue
		}
		// A caller may pass a displays slice shorter than the layout —
		// or nil, when it renders only the panels' skeleton — so a
		// missing display renders nothing instead of panicking; the
		// scroll state is read the same way. See
		// TheoryOfTUIDynamicPlanTab.
		var display []taiui.Line
		if i < len(displays) {
			display = displays[i]
		}
		var scroll taiui.ScrollState
		if i < len(t.scrolls) {
			scroll = t.scrolls[i]
		}
		// The title row's center carries only the tab's fixed name;
		// every mutable state of the tab renders at the row's left side
		// as key value segments. See TheoryOfTabTitleStatus.
		title := tabTitleOf(kind)
		// A searching tree pane's panel sits in the inset box below its
		// search row, so the panel, its title buttons, the fold
		// controls, and the title status all read one geometry. See
		// TheoryOfTreeSearch.
		box := t.tabRenderBox(i, boxes[i])
		var inputBar taiui.Element
		var submitGlyphEl taiui.Element
		if kind == tabOutput && t.interactive && t.tabs.Expanded[i] && box.Height() > 1 && box.Width() > 0 {
			// The chat input bar is the bottom row of the Output tab's
			// box: the panel above it shrinks by one row and the bar
			// spans the tab's width, so the bar is part of the tab's
			// layout rather than a screen-wide overlay. Interactive
			// sessions only — the bar is not rendered otherwise. See
			// TheoryOfTUIChatInput.
			inputBar = t.inputBar.Element(box, t.inputFocused, t.tabs.Focus == i, inputBarStyle)
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
		if kind == tabOutput && t.tabs.Expanded[i] && box.Width() > controlColumnWidth && box.Height() > 0 {
			// The expanded Output tab reserves its leftmost column for
			// the section controls. See TheoryOfOutputControls.
			panel = t.outputPanelView(box, display)
		} else {
			panel = taiui.TabPanel(
				box, title, title,
				t.tabs.Expanded[i], t.tabs.Focus == i, t.tabs.Unseen[i],
				display, scroll, panelStyle,
			)
		}
		if panel != nil {
			elements = append(elements, panel)
		}
		if panel != nil && t.tabs.Expanded[i] {
			// The title row's left side carries the tab's mutable state
			// as key value segments, clipped before the centered fixed
			// name so the name always stays visible; the two leading
			// cells keep the title's rule. See TheoryOfTabTitleStatus.
			if status := t.tabStatus(kind); status != "" {
				if el := titleStatusElement(box, title, status, t.tabs.Focus == i); el != nil {
					elements = append(elements, el)
				}
			}
			// A searching tree pane's search row occupies the row its
			// inset panel freed: keyword editing on the left, then the
			// match indicator and the navigation buttons on the right.
			// See TheoryOfTreeSearch.
			if kind == tabTree || kind == tabPlan {
				if searchEl := t.treeSearchElement(kind, boxes[i]); searchEl != nil {
					elements = append(elements, searchEl)
				}
			}
			// The node under the pointer's fold column renders its fold
			// glyph reversed, the affordance that marks the press
			// target. It applies to every tree-shaped pane. See
			// TheoryOfTreeTab and TheoryOfTUIDynamicPlanTab.
			if kind == tabTree || kind == tabPlan {
				var foldEl taiui.Element
				t.withPane(kind, func() {
					foldEl = t.treeFoldHoverElement(box, display)
				})
				if foldEl != nil {
					elements = append(elements, foldEl)
				}
			}
			// The title row's operation buttons draw over the panel,
			// right-aligned before the two reserved cells. See
			// TheoryOfToolbars.
			if el := t.titleButtonsElement(kind, box); el != nil {
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

func (t *TUI) outputPanelView(box taiui.Box, display []taiui.Line) taiui.Element {
	idx := t.tabIndex(tabOutput)
	panel := taiui.TabPanel(box, tabTitleOf(tabOutput), tabTitleOf(tabOutput),
		t.tabs.Expanded[idx], t.tabs.Focus == idx, t.tabs.Unseen[idx], display, t.scrolls[idx], panelStyle,
		taiui.ContentIndent(controlColumnWidth))
	base := panelStyle.BaseBG
	if t.tabs.Focus == idx {
		base = panelStyle.FocusBG
	}
	// The box buildRoot hands over has already given up the label strip
	// and, in interactive sessions, the chat input bar row, so the
	// visible content rows are the box height minus the label strip
	// alone. The control rows keep their own window. See
	// TheoryOfOutputControls.
	paneHeight := max(box.Height()-1, 1)
	offset := taiui.ClampOffset(t.scrolls[idx].Offset, len(display), paneHeight)
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
