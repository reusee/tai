package main

import (
	"github.com/reusee/tai/taiui"
)

// tabRenderBox returns the box used by a tab's panel and every geometry
// reader. A searching tree pane is inset one row at the top for its
// search bar; every other pane keeps its layout box. See
// TheoryOfTreeSearch.
func (t *TUI) tabRenderBox(idx int, box taiui.Box) taiui.Box {
	kind, ok := t.tabKindAt(idx)
	if !ok || (kind != tabTree && kind != tabPlan) {
		return box
	}
	if idx < 0 || idx >= len(t.tabs.Expanded) || !t.tabs.Expanded[idx] || !t.paneSearching(kind) {
		return box
	}
	return taiui.Box{Top: box.Top + 1, Left: box.Left, Bottom: box.Bottom, Right: box.Right}
}

// treeSearchElement renders a pane's search row: keyword input on the
// left, then the match indicator and right-aligned Prev/Next/First/
// Last/Cancel buttons. It returns nil when the pane does not search.
func (t *TUI) treeSearchElement(kind tuiTab, box taiui.Box) taiui.Element {
	var el taiui.Element
	t.withPane(kind, func() {
		st := &t.treeTab
		if !st.searching {
			return
		}
		row := taiui.Box{Top: box.Top, Left: box.Left, Bottom: box.Top + 1, Right: box.Right}
		slots := taiui.ToolbarLayout(row, treeSearchButtons)
		inputBox := row
		indicator := searchIndicator(st)
		indWidth := taiui.DisplayWidthOptions().String(indicator)
		var indicatorEl taiui.Element
		if len(slots) > 0 {
			indLeft := slots[0].X0 - 1 - indWidth
			indicatorEl = taiui.Text(indicator,
				taiui.Box{Top: row.Top, Left: indLeft, Bottom: row.Bottom, Right: indLeft + indWidth},
				taiui.FGColor(panelStyle.LabelFG))
			right := indLeft - 1
			if right < inputBox.Left {
				right = inputBox.Left
			}
			inputBox.Right = right
		}
		idx := st.paneIdx
		focused := idx == t.tabs.Focus
		children := []any{
			st.searchBar.Element(inputBox, focused, focused, inputBarStyle),
		}
		if indicatorEl != nil {
			children = append(children, indicatorEl)
		}
		hover := -1
		if t.ctlHover && t.mouseReporting {
			hover = taiui.ToolbarHoverAt(row, treeSearchButtons, t.ctlHoverX, t.ctlHoverY)
		}
		if buttons := taiui.ToolbarElement(row, treeSearchButtons, panelStyle, focused, hover); buttons != nil {
			children = append(children, buttons)
		}
		el = taiui.Overlay(children...)
	})
	return el
}

// treeSearchHit maps a press on the search row to a button action. An
// empty action means a harmless press on the row itself; false means
// the press was outside the search row.
func (t *TUI) treeSearchHit(kind tuiTab, x, y int) (string, bool) {
	action := ""
	hit := false
	t.withPane(kind, func() {
		st := &t.treeTab
		idx := st.paneIdx
		if !st.searching || idx < 0 || idx >= len(t.tabs.Expanded) || !t.tabs.Expanded[idx] {
			return
		}
		box := t.tabs.Boxes(t.width, t.height)[idx]
		if y != box.Top || x < box.Left || x >= box.Right {
			return
		}
		row := taiui.Box{Top: box.Top, Left: box.Left, Bottom: box.Top + 1, Right: box.Right}
		if slot, ok := taiui.ToolbarButtonAt(row, treeSearchButtons, x, y); ok {
			action = treeSearchButtons[slot.Index].Action
		}
		hit = true
	})
	return action, hit
}

// treeSearchCursorWanted reports whether the terminal cursor should be
// shown because the focused tree pane is searching.
func (t *TUI) treeSearchCursorWanted() bool {
	kind, ok := t.focusedTreeKind()
	if !ok {
		return false
	}
	wanted := false
	t.withPane(kind, func() { wanted = t.treeTab.searching })
	return wanted
}
