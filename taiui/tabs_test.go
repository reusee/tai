package taiui

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v3/color"
)

func TestTabsAutoExpand(t *testing.T) {
	tabs := NewTabs(3)
	tabs.Focus = -1
	if !tabs.AutoExpand(0) {
		t.Fatal("first content should expand the tab")
	}
	if !tabs.Expanded[0] || tabs.Focus != 0 {
		t.Fatalf("expected expanded output tab with focus, got %+v", tabs)
	}
	if tabs.AutoExpand(0) {
		t.Fatal("second content must not re-expand")
	}

	// Auto-expanding another tab keeps the established focus.
	tabs.AutoExpand(2)
	if !tabs.Expanded[2] || tabs.Focus != 0 {
		t.Fatalf("auto-expand must not change an established focus, got %+v", tabs)
	}

	// A tab collapsed by the user must not re-expand on later content.
	tabs.Toggle(0)
	if tabs.Expanded[0] {
		t.Fatal("output tab should be collapsed")
	}
	if tabs.AutoExpand(0) {
		t.Fatal("output tab must not re-expand after user collapse")
	}
}

func TestTabsToggle(t *testing.T) {
	tabs := NewTabs(3)
	tabs.Expanded = []bool{true, true, false}
	tabs.HasContent = []bool{true, true, false}
	tabs.Focus = 0
	tabs.LastFocus = []int{0, -1, -1}
	tabs.focusOrder = 1

	// Focused tab: pressing its key collapses it and moves the focus to
	// the expanded tab that was last focused.
	tabs.Toggle(0)
	if tabs.Expanded[0] {
		t.Fatal("focused output tab should be collapsed")
	}
	if tabs.Focus != 1 {
		t.Fatalf("focus should move to the last-focused expanded tab, got %d", tabs.Focus)
	}

	// Collapsed tab: pressing its key expands it and switches the focus.
	tabs.Toggle(2)
	if !tabs.Expanded[2] {
		t.Fatal("collapsed logs tab should expand")
	}
	if tabs.Focus != 2 {
		t.Fatalf("focus should switch to the logs tab, got %d", tabs.Focus)
	}

	// Expanded non-focused tab: pressing its key switches the focus
	// without collapsing it.
	tabs.Toggle(1)
	if !tabs.Expanded[1] {
		t.Fatal("expanded round tab must stay expanded")
	}
	if tabs.Focus != 1 {
		t.Fatalf("focus should switch to the round tab, got %d", tabs.Focus)
	}

	// Focused tab again: collapsing leaves the other expanded tab focused.
	tabs.Toggle(1)
	if tabs.Expanded[1] {
		t.Fatal("focused round tab should collapse")
	}
	if tabs.Focus != 2 {
		t.Fatalf("focus should move to the last-focused expanded tab, got %d", tabs.Focus)
	}

	// Collapsing the last expanded tab clears the focus.
	tabs.Toggle(2)
	if tabs.Expanded[2] {
		t.Fatal("focused logs tab should collapse")
	}
	if tabs.Focus != -1 {
		t.Fatalf("focus should be -1 when no tab is expanded, got %d", tabs.Focus)
	}
}

func TestTabsCycleFocusSkipsCollapsedTabs(t *testing.T) {
	tabs := NewTabs(3)
	tabs.Expanded = []bool{true, false, true}
	tabs.Focus = 0
	tabs.CycleFocus()
	if tabs.Focus != 2 {
		t.Fatalf("focus should skip the collapsed round tab and land on logs, got %d", tabs.Focus)
	}
	tabs.CycleFocus()
	if tabs.Focus != 0 {
		t.Fatalf("focus should wrap to the output tab, got %d", tabs.Focus)
	}
	tabs.Expanded = []bool{false, false, false}
	tabs.CycleFocus()
	if tabs.Focus != -1 {
		t.Fatalf("focus should be -1 with no expanded tabs, got %d", tabs.Focus)
	}
}

func TestTabsBoxesWeighted(t *testing.T) {
	tabs := NewTabs(3)
	// The first set of assertions exercises the side-by-side (vertical
	// split) layout; the default is horizontal (stacked).
	tabs.SplitVertical = true
	tabs.Expanded = []bool{true, true, false}
	tabs.Focus = 0
	boxes := tabs.Boxes(90, 40)
	if boxes[0].Left != 0 || boxes[0].Right != 66 || boxes[0].Top != 0 || boxes[0].Bottom != 40 {
		t.Fatalf("unexpected focused panel box: %+v", boxes[0])
	}
	if boxes[1].Left != 66 || boxes[1].Right != 89 {
		t.Fatalf("unexpected non-focused panel box: %+v", boxes[1])
	}
	if boxes[2].Left != 89 || boxes[2].Right != 90 {
		t.Fatalf("unexpected collapsed panel box: %+v", boxes[2])
	}

	tabs.Focus = 1
	boxes = tabs.Boxes(90, 40)
	if boxes[0].Left != 0 || boxes[0].Right != 22 {
		t.Fatalf("unexpected non-focused panel box: %+v", boxes[0])
	}
	if boxes[1].Left != 22 || boxes[1].Right != 89 {
		t.Fatalf("unexpected focused panel box: %+v", boxes[1])
	}

	tabs.Focus = -1
	boxes = tabs.Boxes(90, 40)
	if boxes[0].Left != 0 || boxes[0].Right != 44 {
		t.Fatalf("unexpected equal-share panel box: %+v", boxes[0])
	}
	if boxes[1].Left != 44 || boxes[1].Right != 89 {
		t.Fatalf("unexpected equal-share panel box: %+v", boxes[1])
	}

	tabs2 := NewTabs(3)
	tabs2.Expanded = []bool{true, true, false}
	tabs2.Focus = 0
	boxes = tabs2.Boxes(80, 45)
	if boxes[0].Top != 0 || boxes[0].Bottom != 33 {
		t.Fatalf("unexpected focused panel box: %+v", boxes[0])
	}
	if boxes[1].Top != 33 || boxes[1].Bottom != 44 {
		t.Fatalf("unexpected non-focused panel box: %+v", boxes[1])
	}
	if boxes[2].Top != 44 || boxes[2].Bottom != 45 {
		t.Fatalf("unexpected collapsed panel box: %+v", boxes[2])
	}
}

func TestTabsBoxesCollapsedInPlace(t *testing.T) {
	tabs := NewTabs(3)
	// Side-by-side (vertical split) layout is exercised explicitly; the
	// default is horizontal (stacked).
	tabs.SplitVertical = true
	tabs.Expanded = []bool{true, false, true}
	tabs.Focus = 0
	boxes := tabs.Boxes(90, 40)
	if boxes[1].Left != 66 || boxes[1].Right != 67 {
		t.Fatalf("collapsed round tab must stay in the middle, got %+v", boxes[1])
	}
	if boxes[0].Left != 0 || boxes[0].Right != 66 {
		t.Fatalf("unexpected output panel box: %+v", boxes[0])
	}
	if boxes[2].Left != 67 || boxes[2].Right != 90 {
		t.Fatalf("unexpected logs panel box: %+v", boxes[2])
	}
}

func TestTabsBoxesMaxSizes(t *testing.T) {
	// Stacked layout: the capped, unfocused tab clamps to its cap and
	// frees rows to the uncapped expanded tabs by weight (3 for the
	// focused tab, 1 for the other).
	tabs := NewTabs(3)
	tabs.MaxSizes = []int{0, 0, 3}
	tabs.Expanded = []bool{true, true, true}
	tabs.Focus = 0
	boxes := tabs.Boxes(80, 40)
	// Base split 24/8/8; the cap clamps tab 2 to 3 rows and frees 5,
	// split 3:1 between tabs 0 and 1.
	if boxes[0].Top != 0 || boxes[0].Bottom != 27 {
		t.Fatalf("unexpected first panel box: %+v", boxes[0])
	}
	if boxes[1].Top != 27 || boxes[1].Bottom != 37 {
		t.Fatalf("unexpected second panel box: %+v", boxes[1])
	}
	if boxes[2].Top != 37 || boxes[2].Bottom != 40 {
		t.Fatalf("capped tab must keep 3 rows, got %+v", boxes[2])
	}

	// Vertical split: the same numbers apply along the width axis.
	tabs.SplitVertical = true
	boxes = tabs.Boxes(90, 40)
	if boxes[2].Left != 87 || boxes[2].Right != 90 {
		t.Fatalf("capped tab must keep 3 columns, got %+v", boxes[2])
	}

	// The focused tab ignores its cap: the usual 1:1:3 ratio holds.
	tabs.SplitVertical = false
	tabs.Focus = 2
	boxes = tabs.Boxes(80, 40)
	if boxes[2].Top != 16 || boxes[2].Bottom != 40 {
		t.Fatalf("focused tab must ignore its cap, got %+v", boxes[2])
	}

	// Without MaxSizes the weighted layout is unchanged.
	plain := NewTabs(3)
	plain.Expanded = []bool{true, true, true}
	plain.Focus = 0
	boxes = plain.Boxes(80, 40)
	if boxes[2].Top != 32 || boxes[2].Bottom != 40 {
		t.Fatalf("uncapped layout must keep the weighted split, got %+v", boxes[2])
	}
}

func testPanelStyle() PanelStyle {
	return PanelStyle{
		BaseBG:        HexColor(0x0a1428),
		FocusBG:       HexColor(0x2e2e2e),
		LabelFG:       color.PaletteColor(8),
		FocusLabelFG:  color.PaletteColor(15),
		ActiveLabelFG: color.PaletteColor(10),
	}
}

func TestPanelRenders(t *testing.T) {
	element := Panel(
		Box{Top: 0, Left: 0, Bottom: 4, Right: 12},
		"Output",
		false,
		[]Line{{Text: "content"}},
		0,
		false,
		true,
		testPanelStyle(),
	)
	screen := newFakeScreen(12, 4)
	Render(element, screen)
	if len(screen.frames) == 0 {
		t.Fatal("expected a rendered frame")
	}
	frame := screen.frames[len(screen.frames)-1]
	if cell := frame.Cells[2]; cell.Rune != 'O' {
		t.Fatalf("expected title 'O' at (2,0), got %v", cell.Rune)
	}
	if cell := frame.Cells[1*frame.Width]; cell.Rune != 'c' {
		t.Fatalf("expected content at (0,1), got %v", cell.Rune)
	}
	wantR, wantG, wantB := HexColor(0x0a1428).RGB()
	if r, g, b := frame.Cells[1*frame.Width].Style.Bg().RGB(); r != wantR || g != wantG || b != wantB {
		t.Fatalf("expected unfocused background, got %#x %#x %#x", r, g, b)
	}
}

func TestCollapsedPanelRendering(t *testing.T) {
	style := testPanelStyle()

	t.Run("Horizontal", func(t *testing.T) {
		element := CollapsedPanel(Box{Top: 0, Left: 0, Bottom: 1, Right: 12}, "1 Output", false, style)
		screen := newFakeScreen(12, 1)
		Render(element, screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		frame := screen.frames[len(screen.frames)-1]
		if cell := frame.Cells[2]; cell.Rune != '1' {
			t.Fatalf("expected '1' at (2,0), got %v", cell.Rune)
		}
		if cell := frame.Cells[4]; cell.Rune != 'O' {
			t.Fatalf("expected 'O' at (4,0), got %v", cell.Rune)
		}
	})

	t.Run("Vertical", func(t *testing.T) {
		element := CollapsedPanel(Box{Top: 0, Left: 0, Bottom: 8, Right: 1}, "1 Output", false, style)
		screen := newFakeScreen(1, 8)
		Render(element, screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		frame := screen.frames[len(screen.frames)-1]
		if cell := frame.Cells[0]; cell.Rune != '1' {
			t.Fatalf("expected '1' at (0,0), got %v", cell.Rune)
		}
		if cell := frame.Cells[2*frame.Width]; cell.Rune != 'O' {
			t.Fatalf("expected 'O' at (0,2), got %v", cell.Rune)
		}
	})
}

func TestPanelLargeOutput(t *testing.T) {
	lines := make([]Line, 100000)
	for i := range lines {
		lines[i] = Line{Text: fmt.Sprintf("line %06d", i)}
	}
	element := Panel(
		Box{Top: 0, Left: 0, Bottom: 10, Right: 40},
		"Output",
		false,
		lines,
		50000,
		false,
		false,
		testPanelStyle(),
	)
	screen := newFakeScreen(40, 10)
	Render(element, screen)
	if len(screen.frames) == 0 {
		t.Fatal("expected a rendered frame")
	}
	frame := screen.frames[len(screen.frames)-1]
	// Row 1 should be line 50000: 'l' 'i' 'n' 'e' ' ' '0' '5' '0' '0' '0' '0'
	cell := frame.Cells[1*frame.Width]
	if cell.Rune != 'l' {
		t.Fatalf("expected 'l' at row 1 col 0, got %v", cell.Rune)
	}
	digit := frame.Cells[1*frame.Width+6]
	if digit.Rune != '5' {
		t.Fatalf("expected '5' at row 1 col 6 for line 50000, got %v", digit.Rune)
	}
}

func TestPanelDegenerateBoxRendersNothing(t *testing.T) {
	// The constructor box is authoritative: a Panel with a degenerate box
	// (zero width or height) must render nothing rather than falling back
	// to the parent-assigned box, which would paint over unrelated
	// regions. See TheoryOfTabs.
	element := Panel(
		Box{Top: 2, Left: 2, Bottom: 2, Right: 8},
		"Output",
		false,
		[]Line{{Text: "content"}},
		0,
		false,
		true,
		testPanelStyle(),
	)
	screen := newFakeScreen(10, 4)
	Render(element, screen)
	if len(screen.frames) == 0 {
		t.Fatal("expected a rendered frame")
	}
	frame := screen.frames[len(screen.frames)-1]
	for i, cell := range frame.Cells {
		if cell.Set {
			t.Fatalf("expected no cells set for a degenerate panel box, got cell %d set to %q", i, string(cell.Rune))
		}
	}
}
