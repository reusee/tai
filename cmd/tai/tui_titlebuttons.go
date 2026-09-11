package main

import (
	"slices"

	"github.com/clipperhouse/displaywidth"
	"github.com/reusee/tai/taiui"
)

const TheoryOfToolbars = `
Tab toolbar theory (cmd/tai):

- Every expanded tab's title row carries, right-aligned, one unicode
  button per operation that acts on that tab or on the session: the
  Output tab gets the section navigation and the sections collapse-all,
  the Tree tab the view cycling and the nodes collapse-all, and the Logs
  tab the session-level controls — the split toggle, the mouse-reporting
  toggle, the help overlay, and quit. The two rightmost cells stay
  unpainted, so the panel's dim title rule shows through as the row's
  right margin.
- Each button renders a single Han character as its label — two cells
  wide — so the visible label spans the press target; adjacent buttons
  carry no separator cells.
- The labels tile the toolbar span with no gaps, so the panel title
  row's dim strike-through rule does not show through; the reserved
  cells stay outside the span, so the rule survives as the row's right
  margin.
- The renderer and the hit test share titleButtonLayout, so what is
  drawn is what is pressed. A press on a button runs its action through
  dispatchControlBar — the same action vocabulary the key dispatch
  uses, so a click and a keystroke mean the same thing — and preempts
  the ordinary press handling, so a press on a button never toggles or
  focuses the tab. A press on the title row outside the buttons keeps
  the ordinary strip semantics. Hover renders the button reversed,
  affordance only.
- A tab too narrow for the buttons drops the buttons that do not fit,
  rightmost first; the reserved cells stay.
`

// titleButton is one button of a tab title row: its unicode glyph and
// the action the press runs. See TheoryOfToolbars.
type titleButton struct {
	Glyph  string
	Action controlBarAction
}

// The button glyphs: plain, colorable characters. See TheoryOfToolbars.
const (
	titleButtonPrev     = "上"
	titleButtonNext     = "下"
	titleButtonCollapse = "收"
	titleButtonCycle    = "换"
	titleButtonSplit    = "分"
	titleButtonMouse    = "鼠"
	titleButtonHelp     = "帮"
	titleButtonQuit     = "退"
)

// tabTitleButtons returns the title buttons of tab idx: one button per
// operation that acts on the tab or on the session. The Logs tab
// carries the session-level controls. See TheoryOfToolbars.
func tabTitleButtons(idx int) []titleButton {
	switch idx {
	case 0:
		return []titleButton{
			{Glyph: titleButtonPrev, Action: controlPrevSections},
			{Glyph: titleButtonNext, Action: controlNextSections},
			{Glyph: titleButtonCollapse, Action: controlCollapseAll},
		}
	case 1:
		return []titleButton{
			{Glyph: titleButtonCycle, Action: controlTreeViewCycle},
			{Glyph: titleButtonCollapse, Action: controlCollapseTree},
		}
	case 2:
		return []titleButton{
			{Glyph: titleButtonSplit, Action: controlSplitToggle},
			{Glyph: titleButtonMouse, Action: controlMouseToggle},
			{Glyph: titleButtonHelp, Action: controlHelpToggle},
			{Glyph: titleButtonQuit, Action: controlQuit},
		}
	}
	return nil
}

// titleReservedCells is the blank cells reserved at the title row's
// right edge: the panel's dim rule shows through. See
// TheoryOfToolbars.
const titleReservedCells = 2

// titleButtonSlot is one laid-out button: the buttons-slice index and
// the title row's cell range [x0, x1).
type titleButtonSlot struct {
	index  int
	x0, x1 int
}

// titleButtonLayout lays the buttons out right to left from the two
// reserved cells, each slot spanning its label's cells; adjacent
// buttons carry no separator cells. It is measured with the same
// width options the renderer uses. It is pure: the renderer draws
// from it and the hit test maps presses through it, so the two
// cannot disagree. Buttons past the box's left edge are dropped. See
// TheoryOfToolbars.
func titleButtonLayout(box taiui.Box, options displaywidth.Options, buttons []titleButton) []titleButtonSlot {
	if box.Height() <= 0 || box.Width() <= titleReservedCells {
		return nil
	}
	var out []titleButtonSlot
	x := box.Right - titleReservedCells
	for i := len(buttons) - 1; i >= 0; i-- {
		x0 := x - options.String(buttons[i].Glyph)
		if x0 < box.Left {
			break
		}
		out = append(out, titleButtonSlot{index: i, x0: x0, x1: x})
		x = x0
	}
	slices.Reverse(out)
	return out
}

// titleButtonsElement renders tab idx's title-row buttons as an
// overlay over the panel: one label per slot, in the tab's label
// colors, reversed under the pointer. The labels tile the toolbar
// span, so the panel title row's dim strike-through rule does not
// show through. It returns nil when the tab is collapsed, carries no
// buttons, or the layout drops every button. The caller holds t.mu.
// See TheoryOfToolbars.
func (t *TUI) titleButtonsElement(idx int, box taiui.Box) taiui.Element {
	if !t.tabs.Expanded[idx] {
		return nil
	}
	buttons := tabTitleButtons(idx)
	if len(buttons) == 0 {
		return nil
	}
	slots := titleButtonLayout(box, taiui.DisplayWidthOptions(), buttons)
	if len(slots) == 0 {
		return nil
	}
	base := panelStyle.BaseBG
	fg := panelStyle.LabelFG
	if t.tabs.Focus == idx {
		base = panelStyle.FocusBG
		fg = panelStyle.FocusLabelFG
	}
	hover := t.titleButtonHoverLocked(box, slots)
	var children []any
	for _, slot := range slots {
		specs := []any{
			buttons[slot.index].Glyph,
			taiui.Box{Top: box.Top, Left: slot.x0, Bottom: box.Top + 1, Right: slot.x1},
			taiui.FGColor(fg),
		}
		if base != taiui.NoColor {
			specs = append(specs, taiui.BGColor(base))
		}
		if slot.index == hover {
			specs = append(specs, taiui.Reverse(true))
		}
		children = append(children, taiui.Text(specs...))
	}
	return taiui.Overlay(children...)
}

// titleButtonHoverLocked returns the slot the pointer hovers, or -1.
// The highlight is affordance only. The caller holds t.mu. See
// TheoryOfToolbars.
func (t *TUI) titleButtonHoverLocked(box taiui.Box, slots []titleButtonSlot) int {
	if !t.ctlHover || !t.mouseReporting || t.ctlHoverY != box.Top {
		return -1
	}
	for _, slot := range slots {
		if t.ctlHoverX >= slot.x0 && t.ctlHoverX < slot.x1 {
			return slot.index
		}
	}
	return -1
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
		for _, slot := range titleButtonLayout(box, taiui.DisplayWidthOptions(), buttons) {
			if x >= slot.x0 && x < slot.x1 {
				return buttons[slot.index].Action, true
			}
		}
		return "", false
	}
	return "", false
}
