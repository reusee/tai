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

func tabTitleButtons(kind tuiTab) []taiui.ToolbarButton {
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

func (t *TUI) titleButtonsElement(kind tuiTab, box taiui.Box) taiui.Element {
	idx := t.tabIndex(kind)
	if idx < 0 || !t.tabs.Expanded[idx] {
		return nil
	}
	buttons := tabTitleButtons(kind)
	if len(buttons) == 0 {
		return nil
	}
	hover := -1
	if t.ctlHover && t.mouseReporting {
		hover = taiui.ToolbarHoverAt(box, buttons, t.ctlHoverX, t.ctlHoverY)
	}
	return taiui.ToolbarElement(box, buttons, panelStyle, t.tabs.Focus == idx, hover)
}

func (t *TUI) titleButtonHitLocked(x, y int) (controlBarAction, bool) {
	boxes := t.tabs.Boxes(t.width, t.height)
	for _, kind := range t.tabKinds() {
		idx := t.tabIndex(kind)
		if idx < 0 || idx >= len(boxes) || !t.tabs.Expanded[idx] {
			continue
		}
		box := boxes[idx]
		if y != box.Top || x < box.Left || x >= box.Right {
			continue
		}
		buttons := tabTitleButtons(kind)
		slot, ok := taiui.ToolbarButtonAt(box, buttons, x, y)
		if !ok {
			return "", false
		}
		return controlBarAction(buttons[slot.Index].Action), true
	}
	return "", false
}
