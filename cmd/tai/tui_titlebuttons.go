package main

import (
	"github.com/reusee/tai/taiui"
)

const TheoryOfToolbars = `
Tab toolbar theory (cmd/tai):

- Every expanded tab's title row carries, right-aligned, its operation
  toolbar: a collapsed toolbar — the default — shows a single unicode
  icon at the row's right end, and pressing it expands the toolbar to
  the tab's actions: the Tree tab the view cycling and the nodes
  collapse-all, the Plan tab the nodes collapse-all, the Output tab the
  section navigation and the sections collapse-all, and the Logs tab
  the session-level controls — the split toggle, the mouse-reporting
  toggle, the help overlay, and quit. The icon keeps the rightmost slot
  in both states, so the cell that expanded the toolbar collapses it
  again and the icon's own cells never move as the toolbar opens.
- The toolbar is transient, and an action press is the one press that
  does not fold it: an action button runs its action and keeps the
  toolbar expanded, so consecutive actions need no re-expansion and the
  quit action's two-press confirmation reaches the same button twice.
  Every other press folds the whole toolbar: the toggle icon on its
  expanded toolbar folds it through the toggle, and a press outside the
  toolbar's buttons folds every open toolbar while keeping its ordinary
  handling, so an open toolbar never outlives the press that used it.
  One toolbar is open at a time: expanding a tab's toolbar collapses
  every other one. A tab that collapses or leaves the layout takes its
  toolbar with it, so a re-expanded tab starts collapsed and a
  collapsed tab never renders one.
- The toggle acts on the tab whose icon was pressed, so it runs in the
  pointer path, like the quit button's early resolution; the reusable
  mechanism — the right-to-left layout, the reserved margin, the press
  hit test, and the hover highlight — lives in taiui (see
  taiui.TheoryOfToolbar). The action labels are English words, so every
  terminal renders them and the visible label spans its press target;
  the collapse icon is a single unicode glyph that stands for the whole
  action set.
- Every button press preempts the ordinary press handling, so a press
  on a button never toggles or focuses the tab; a press on the title row
  outside the buttons keeps the ordinary strip semantics.
`

// The button labels: plain, colorable characters. See TheoryOfToolbars.
const (
	titleButtonPrev     = "Up"
	titleButtonNext     = "Down"
	titleButtonCollapse = "Collapse"
	titleButtonCycle    = "Switch"
	titleButtonSplit    = "Split"
	titleButtonMouse    = "Mouse"
	titleButtonHelp     = "Help"
	titleButtonQuit     = "Quit"
)

// titleButtonToggle is the collapse icon of a title toolbar: the single
// unicode glyph a collapsed toolbar shows at the title row's right end.
// Pressing it expands the toolbar's action buttons, and it keeps the
// rightmost slot in both states, so the same cell collapses the toolbar
// again. See TheoryOfToolbars.
const titleButtonToggle = "☰"

// tabTitleActions returns the tab's action buttons, in left-to-right
// order: the buttons a collapsed toolbar hides behind its collapse icon.
// See TheoryOfToolbars.
func tabTitleActions(kind tuiTab) []taiui.ToolbarButton {
	switch kind {
	case tabTree:
		return []taiui.ToolbarButton{
			{Label: titleButtonCycle, Action: string(controlTreeViewCycle)},
			{Label: titleButtonCollapse, Action: string(controlCollapseTree)},
		}
	case tabPlan:
		// The Plan tab folds its nodes like the Tree tab. See
		// TheoryOfTUIDynamicPlanTab.
		return []taiui.ToolbarButton{
			{Label: titleButtonCollapse, Action: string(controlCollapseTree)},
		}
	case tabOutput:
		return []taiui.ToolbarButton{
			{Label: titleButtonPrev, Action: string(controlPrevSections)},
			{Label: titleButtonNext, Action: string(controlNextSections)},
			{Label: titleButtonCollapse, Action: string(controlCollapseAll)},
		}
	default:
		return []taiui.ToolbarButton{
			{Label: titleButtonSplit, Action: string(controlSplitToggle)},
			{Label: titleButtonMouse, Action: string(controlMouseToggle)},
			{Label: titleButtonHelp, Action: string(controlHelpToggle)},
			{Label: titleButtonQuit, Action: string(controlQuit)},
		}
	}
}

// titleToolbarButtons returns the buttons one tab's title toolbar
// renders: the collapse icon alone while the toolbar is collapsed — the
// default — or the tab's action buttons with the icon rightmost while it
// is expanded. The icon keeps the rightmost slot in both states, so the
// cell that expanded the toolbar collapses it again; the hit test and
// the renderer share this list, so what is drawn is what is pressed. The
// caller holds t.mu. See TheoryOfToolbars.
func (t *TUI) titleToolbarButtons(kind tuiTab) []taiui.ToolbarButton {
	toggle := taiui.ToolbarButton{
		Label:  titleButtonToggle,
		Action: string(controlToolbarToggle),
	}
	if !t.toolbarExpanded(kind) {
		return []taiui.ToolbarButton{toggle}
	}
	return append(tabTitleActions(kind), toggle)
}

// toolbarExpanded reports whether the tab's toolbar is expanded and
// visible: a collapsed tab renders no title row to carry a toolbar, so
// its recorded state reads collapsed. The caller holds t.mu. See
// TheoryOfToolbars.
func (t *TUI) toolbarExpanded(kind tuiTab) bool {
	if !t.toolbarOpen || t.toolbarKind != kind {
		return false
	}
	idx := t.tabIndex(kind)
	return idx >= 0 && idx < len(t.tabs.Expanded) && t.tabs.Expanded[idx]
}

// openToolbar expands the tab's toolbar; the recorded kind is the single
// open toolbar, so opening one collapses every other. The caller holds
// t.mu. See TheoryOfToolbars.
func (t *TUI) openToolbar(kind tuiTab) {
	t.toolbarOpen = true
	t.toolbarKind = kind
}

// collapseToolbar folds every title toolbar. The caller holds t.mu. See
// TheoryOfToolbars.
func (t *TUI) collapseToolbar() {
	t.toolbarOpen = false
}

// normalizeToolbar folds the recorded toolbar state when its tab is no
// longer expanded or has left the layout, so a re-expanded tab starts
// collapsed. The caller holds t.mu. See TheoryOfToolbars.
func (t *TUI) normalizeToolbar() {
	if !t.toolbarOpen {
		return
	}
	if !t.toolbarExpanded(t.toolbarKind) {
		t.collapseToolbar()
	}
}

// titleButtonsElement renders one expanded tab's title toolbar: the
// collapse icon alone while the toolbar is collapsed, or the tab's action
// buttons beside the icon while it is expanded. The caller holds t.mu.
// See TheoryOfToolbars.
func (t *TUI) titleButtonsElement(kind tuiTab, box taiui.Box) taiui.Element {
	idx := t.tabIndex(kind)
	if idx < 0 || !t.tabs.Expanded[idx] {
		return nil
	}
	buttons := t.titleToolbarButtons(kind)
	if len(buttons) == 0 {
		return nil
	}
	hover := -1
	if t.ctlHover && t.mouseReporting {
		hover = taiui.ToolbarHoverAt(box, buttons, t.ctlHoverX, t.ctlHoverY)
	}
	return taiui.ToolbarElement(box, buttons, panelStyle, t.tabs.Focus == idx, hover)
}

// titleButtonHitLocked maps a cell onto the title toolbar button under
// it: the tab whose toolbar carries the button and the button's action.
// The hit test uses the same button list the renderer draws, so what is
// drawn is what is pressed. The caller holds t.mu. See TheoryOfToolbars.
func (t *TUI) titleButtonHitLocked(x, y int) (kind tuiTab, action controlBarAction, ok bool) {
	boxes := t.tabs.Boxes(t.width, t.height)
	for _, k := range t.tabKinds() {
		idx := t.tabIndex(k)
		if idx < 0 || idx >= len(boxes) || !t.tabs.Expanded[idx] {
			continue
		}
		// The buttons render on the panel's title row, which a
		// searching tree pane's search row pushes down one row; the
		// inset box is the one geometry source. See TheoryOfTreeSearch.
		box := t.tabRenderBox(idx, boxes[idx])
		if y != box.Top || x < box.Left || x >= box.Right {
			continue
		}
		buttons := t.titleToolbarButtons(k)
		slot, hit := taiui.ToolbarButtonAt(box, buttons, x, y)
		if !hit {
			return 0, "", false
		}
		return k, controlBarAction(buttons[slot.Index].Action), true
	}
	return 0, "", false
}
