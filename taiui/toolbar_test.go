package taiui

import (
	"slices"
	"testing"

	"github.com/gdamore/tcell/v3/vt"
)

var testToolbarButtons = []ToolbarButton{
	{Label: "上", Action: "prev"},
	{Label: "下", Action: "next"},
	{Label: "收", Action: "collapse"},
}

// toolbarTestScreen records the frames Render presents, so tests
// inspect rendered cell styles.
type toolbarTestScreen struct {
	w, h   int
	frames []Frame
}

func (s *toolbarTestScreen) Width() int  { return s.w }
func (s *toolbarTestScreen) Height() int { return s.h }
func (s *toolbarTestScreen) Present(f Frame) {
	s.frames = append(s.frames, f)
}

// toolbarCellReversed reports whether the cell carries the reverse
// attribute: the drawn cell's style may be nil, which never counts.
func toolbarCellReversed(f *Frame, x int) bool {
	cell := f.Cells[x]
	return cell.Set && cell.Style != nil && cell.Style.Attr()&vt.Reverse != 0
}

// TestToolbarLayout pins the right-to-left layout: the two rightmost
// cells stay reserved, each button occupies its label's display width,
// adjacent buttons carry no separator cells, and a row too narrow
// drops the buttons that do not fit. See TheoryOfToolbar.
func TestToolbarLayout(t *testing.T) {
	row := Box{Top: 0, Left: 0, Bottom: 1, Right: 40}
	slots := ToolbarLayout(row, testToolbarButtons)
	want := []ToolbarSlot{
		{Index: 0, X0: 32, X1: 34},
		{Index: 1, X0: 34, X1: 36},
		{Index: 2, X0: 36, X1: 38},
	}
	if !slices.Equal(slots, want) {
		t.Fatalf("unexpected layout: %+v", slots)
	}

	// A row five cells wide holds exactly one button.
	slots = ToolbarLayout(Box{Top: 0, Left: 0, Bottom: 1, Right: 5}, testToolbarButtons)
	if len(slots) != 1 || slots[0].X0 != 1 || slots[0].X1 != 3 {
		t.Fatalf("unexpected narrow-row layout: %+v", slots)
	}

	// A row narrower than one button drops every button; the reserved
	// cells stay.
	slots = ToolbarLayout(Box{Top: 0, Left: 0, Bottom: 1, Right: 3}, testToolbarButtons)
	if len(slots) != 0 {
		t.Fatalf("expected no slots in a 3-wide row, got %+v", slots)
	}

	// A zero-height row carries no layout.
	if slots := ToolbarLayout(Box{Top: 0, Left: 0, Bottom: 0, Right: 40}, testToolbarButtons); len(slots) != 0 {
		t.Fatalf("a zero-height row must not lay out buttons, got %+v", slots)
	}
}

// TestToolbarButtonAt pins the pure hit test: each cell of a slot
// resolves to its button, and a cell outside the toolbar — the
// reserved margin, a region before the toolbar, another row — resolves
// to nothing. See TheoryOfToolbar.
func TestToolbarButtonAt(t *testing.T) {
	row := Box{Top: 0, Left: 0, Bottom: 1, Right: 40}
	for _, want := range []struct {
		x     int
		index int
	}{
		{32, 0}, {33, 0},
		{34, 1}, {35, 1},
		{36, 2}, {37, 2},
	} {
		slot, ok := ToolbarButtonAt(row, testToolbarButtons, want.x, row.Top)
		if !ok || slot.Index != want.index {
			t.Fatalf("cell %d resolved %+v ok=%v, want index %d", want.x, slot, ok, want.index)
		}
	}
	for _, x := range []int{0, 31, 38, 39} {
		if _, ok := ToolbarButtonAt(row, testToolbarButtons, x, row.Top); ok {
			t.Fatalf("cell %d must not resolve to a button", x)
		}
	}
	if _, ok := ToolbarButtonAt(row, testToolbarButtons, 32, 1); ok {
		t.Fatal("another row must not resolve to a button")
	}
}

// TestToolbarHoverAt pins the hover hit test: the pointer's cell
// resolves to the button under it, or to no button. See
// TheoryOfToolbar.
func TestToolbarHoverAt(t *testing.T) {
	row := Box{Top: 0, Left: 0, Bottom: 1, Right: 40}
	if got := ToolbarHoverAt(row, testToolbarButtons, 33, 0); got != 0 {
		t.Fatalf("the pointer on the first button must resolve to it, got %d", got)
	}
	if got := ToolbarHoverAt(row, testToolbarButtons, 39, 0); got != -1 {
		t.Fatalf("the pointer on the reserved margin must resolve to no button, got %d", got)
	}
}

// TestToolbarElementRendering pins the rendered labels, the reserved
// margin, and the hover highlight: the buttons draw right-aligned and
// adjacent before the reserved stretch, and the hovered button renders
// reversed. See TheoryOfToolbar.
func TestToolbarElementRendering(t *testing.T) {
	row := Box{Top: 0, Left: 0, Bottom: 1, Right: 40}
	lastFrame := func(e Element) *Frame {
		screen := &toolbarTestScreen{w: 40, h: 1}
		Render(e, screen)
		return &screen.frames[len(screen.frames)-1]
	}

	frame := lastFrame(ToolbarElement(row, testToolbarButtons, PanelStyle{}, false, -1))
	if frame.Cells[32].Rune != '上' || frame.Cells[34].Rune != '下' || frame.Cells[36].Rune != '收' {
		t.Fatalf("unexpected button labels: %q %q %q",
			string(frame.Cells[32].Rune), string(frame.Cells[34].Rune), string(frame.Cells[36].Rune))
	}
	for _, x := range []int{38, 39} {
		if frame.Cells[x].Set {
			t.Fatalf("cell %d must stay reserved", x)
		}
	}

	frame = lastFrame(ToolbarElement(row, testToolbarButtons, PanelStyle{}, false, 0))
	if !toolbarCellReversed(frame, 32) {
		t.Fatal("expected the hovered button to render reversed")
	}
	if toolbarCellReversed(frame, 34) {
		t.Fatal("an unhovered button must stay plain")
	}
}

// TestToolbarElementDegenerateInputs pins the nil results: an empty
// button set and a row too narrow for one button both yield no
// element. See TheoryOfToolbar.
func TestToolbarElementDegenerateInputs(t *testing.T) {
	if el := ToolbarElement(Box{Top: 0, Left: 0, Bottom: 1, Right: 40}, nil, PanelStyle{}, false, -1); el != nil {
		t.Fatal("an empty button set must yield no element")
	}
	if el := ToolbarElement(Box{Top: 0, Left: 0, Bottom: 1, Right: 3}, testToolbarButtons, PanelStyle{}, false, -1); el != nil {
		t.Fatal("a row too narrow for one button must yield no element")
	}
}
