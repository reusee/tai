package taiui

import "slices"

const TheoryOfToolbar = `
taiui title toolbar theory:
- A toolbar is a strip of buttons laid along a title row's right edge,
  one button per operation that acts on the pane or on the session.
  Each button carries a label and an opaque action string: the widget
  reports the action a press resolves to, and the application maps it
  to its own dispatch, so the mechanism is reusable across
  applications.
- The layout is pure and shared by the renderer and the hit tests, so
  what is drawn is what is pressed. Buttons lay out right to left from
  a reserved blank margin at the right edge; the margin keeps the host
  title row's own rule visible past the buttons. A button occupies
  exactly its label's display width, and adjacent buttons carry no
  separator cells, so the labels tile the toolbar span.
- A row too narrow for every button drops the buttons that do not fit,
  rightmost first, keeping the reserved margin.
- Pointer hover is affordance only: the button under the pointer
  renders reversed, and hovering runs no action.
`

// ToolbarButton is one button of a title toolbar: its label and the
// action a press runs. The label is a single glyph rendered at its
// display width; the action is opaque to the widget. See
// TheoryOfToolbar.
type ToolbarButton struct {
	Label  string
	Action string
}

// ToolbarReservedCells is the blank cells reserved at the title row's
// right edge, so the host title row's own rule stays visible past the
// buttons. See TheoryOfToolbar.
const ToolbarReservedCells = 2

// ToolbarSlot is one laid-out button: the buttons-slice index and the
// row's cell range [X0, X1). See TheoryOfToolbar.
type ToolbarSlot struct {
	Index  int
	X0, X1 int
}

// ToolbarLayout lays the buttons out right to left from the reserved
// margin, each slot spanning its label's display width. It is pure:
// the renderer draws from it and the hit tests map presses through it,
// so the two cannot disagree. Buttons past the row's left edge are
// dropped. See TheoryOfToolbar.
func ToolbarLayout(row Box, buttons []ToolbarButton) []ToolbarSlot {
	if row.Height() <= 0 || row.Width() <= ToolbarReservedCells {
		return nil
	}
	options := DisplayWidthOptions()
	var out []ToolbarSlot
	x := row.Right - ToolbarReservedCells
	for i := len(buttons) - 1; i >= 0; i-- {
		x0 := x - options.String(buttons[i].Label)
		if x0 < row.Left {
			break
		}
		out = append(out, ToolbarSlot{Index: i, X0: x0, X1: x})
		x = x0
	}
	slices.Reverse(out)
	return out
}

// ToolbarButtonAt maps a cell onto the button it hits: the cell must
// lie on the toolbar's row, inside one of its slots. It reports false
// when the cell is outside the toolbar. See TheoryOfToolbar.
func ToolbarButtonAt(row Box, buttons []ToolbarButton, x, y int) (ToolbarSlot, bool) {
	if y != row.Top || x < row.Left || x >= row.Right {
		return ToolbarSlot{}, false
	}
	for _, slot := range ToolbarLayout(row, buttons) {
		if x >= slot.X0 && x < slot.X1 {
			return slot, true
		}
	}
	return ToolbarSlot{}, false
}

// ToolbarHoverAt returns the index of the button under a pointer cell,
// or -1. Hover is affordance only: it runs no action. See
// TheoryOfToolbar.
func ToolbarHoverAt(row Box, buttons []ToolbarButton, x, y int) int {
	slot, ok := ToolbarButtonAt(row, buttons, x, y)
	if !ok {
		return -1
	}
	return slot.Index
}

// ToolbarElement renders the buttons of a title row: one label per
// slot, in the panel label colors selected by the pane's focus state.
// The button at hoverIndex renders reversed. The labels tile the
// toolbar span, so the title row's own rule does not show through the
// buttons; the reserved margin keeps that rule visible at the row's
// right edge. It returns nil when the row or the button set
// degenerates. See TheoryOfToolbar.
func ToolbarElement(row Box, buttons []ToolbarButton, style PanelStyle, focused bool, hoverIndex int) Element {
	slots := ToolbarLayout(row, buttons)
	if len(slots) == 0 {
		return nil
	}
	base := style.BaseBG
	fg := style.LabelFG
	if focused {
		base = style.FocusBG
		fg = style.FocusLabelFG
	}
	var children []any
	for _, slot := range slots {
		specs := []any{
			buttons[slot.Index].Label,
			Box{Top: row.Top, Left: slot.X0, Bottom: row.Top + 1, Right: slot.X1},
			FGColor(fg),
		}
		if base != NoColor {
			specs = append(specs, BGColor(base))
		}
		if slot.Index == hoverIndex {
			specs = append(specs, Reverse(true))
		}
		children = append(children, Text(specs...))
	}
	return Overlay(children...)
}
