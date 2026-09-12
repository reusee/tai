package main

import (
	"github.com/reusee/tai/taiui"
)

const TheoryOfToolbars = `
Tab toolbar theory (cmd/tai):

- Every expanded tab's title row carries, right-aligned, one unicode
  button per operation that acts on that tab or on the session: the
  Output tab gets the section navigation and the sections collapse-all,
  the Tree tab the view cycling and the nodes collapse-all, and the Logs
  tab the session-level controls — the split toggle, the mouse-reporting
  toggle, the help overlay, and quit. The reusable mechanism — the
  right-to-left layout, the reserved margin, the press hit test, and
  the hover highlight — lives in taiui (see taiui.TheoryOfToolbar).
- The button labels are English words, so every terminal renders them
  and the visible label spans its press target.
- A press on a button runs the button's action through
  dispatchControlBar — the same action vocabulary the key dispatch
  uses, so a click and a keystroke mean the same thing — and preempts
  the ordinary press handling, so a press on a button never toggles or
  focuses the tab. A press on the title row outside the buttons keeps
  the ordinary strip semantics.
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

// tabTitleButtons returns the title buttons of tab idx: one button per
// operation that acts on the tab or on the session. The tabs are indexed
// in display order (0 Tree, 1 Output, 2 Logs), and the Logs tab carries
// the session-level controls. See TheoryOfToolbars.
func tabTitleButtons(idx int) []taiui.ToolbarButton {
	switch idx {
	case 0:
		return []taiui.ToolbarButton{
			{Label: titleButtonCycle, Action: string(controlTreeViewCycle)},
			{Label: titleButtonCollapse, Action: string(controlCollapseTree)},
		}
	case 1:
		return []taiui.ToolbarButton{
			{Label: titleButtonPrev, Action: string(controlPrevSections)},
			{Label: titleButtonNext, Action: string(controlNextSections)},
			{Label: titleButtonCollapse, Action: string(controlCollapseAll)},
		}
	case 2:
		return []taiui.ToolbarButton{
			{Label: titleButtonSplit, Action: string(controlSplitToggle)},
			{Label: titleButtonMouse, Action: string(controlMouseToggle)},
			{Label: titleButtonHelp, Action: string(controlHelpToggle)},
			{Label: titleButtonQuit, Action: string(controlQuit)},
		}
	}
	return nil
}

// titleButtonsElement renders tab idx's title-row buttons as an
// overlay over the panel. The hover highlight requires mouse reporting
// on: with reporting off the tracked pointer position is stale. It
// returns nil when the tab is collapsed, carries no buttons, or the
// layout drops every button. The caller holds t.mu. See
// TheoryOfToolbars and taiui.TheoryOfToolbar.
func (t *TUI) titleButtonsElement(idx int, box taiui.Box) taiui.Element {
	if !t.tabs.Expanded[idx] {
		return nil
	}
	buttons := tabTitleButtons(idx)
	if len(buttons) == 0 {
		return nil
	}
	hover := -1
	if t.ctlHover && t.mouseReporting {
		hover = taiui.ToolbarHoverAt(box, buttons, t.ctlHoverX, t.ctlHoverY)
	}
	return taiui.ToolbarElement(box, buttons, panelStyle, t.tabs.Focus == idx, hover)
}

// titleButtonHitLocked maps a left press onto the title button it
// hits: the press must land on an expanded tab's title row, inside one
// of the tab's button slots. A press on the title row outside the
// buttons is not consumed, so the ordinary strip semantics stay. The
// caller holds t.mu. See TheoryOfToolbars.
func (t *TUI) titleButtonHitLocked(x, y int) (controlBarAction, bool) {
	boxes := t.tabs.Boxes(t.width, t.height)
	for idx := range tabNames {
		if !t.tabs.Expanded[idx] {
			continue
		}
		box := boxes[idx]
		if y != box.Top || x < box.Left || x >= box.Right {
			continue
		}
		buttons := tabTitleButtons(idx)
		slot, ok := taiui.ToolbarButtonAt(box, buttons, x, y)
		if !ok {
			return "", false
		}
		return controlBarAction(buttons[slot.Index].Action), true
	}
	return "", false
}
