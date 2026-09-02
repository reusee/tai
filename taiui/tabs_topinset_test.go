package taiui

import "testing"

// TestTabsBoxesTopInset pins the reserved top rows: every box starts
// below the inset in both split axes, and an inset past the height
// degenerates the boxes. See TheoryOfTabs.
func TestTabsBoxesTopInset(t *testing.T) {
	tabs := NewTabs(2)
	tabs.FocusTab(0)
	tabs.TopInset = 2

	tabs.SplitVertical = false
	boxes := tabs.Boxes(40, 20)
	if boxes[0].Top != 2 || boxes[0].Bottom != 19 {
		t.Fatalf("stacked layout must start below the inset, got %+v", boxes[0])
	}
	if boxes[1].Top != 19 || boxes[1].Bottom != 20 {
		t.Fatalf("collapsed strip must sit below the inset, got %+v", boxes[1])
	}

	tabs.SplitVertical = true
	boxes = tabs.Boxes(40, 20)
	if boxes[0].Top != 2 || boxes[0].Bottom != 20 {
		t.Fatalf("side-by-side layout must start below the inset, got %+v", boxes[0])
	}
	if boxes[0].Left != 0 || boxes[0].Right != 39 {
		t.Fatalf("side-by-side columns must keep the split, got %+v", boxes[0])
	}

	tabs.TopInset = 25
	boxes = tabs.Boxes(40, 20)
	if boxes[0].Height() != 0 {
		t.Fatalf("an inset past the height must degenerate, got %+v", boxes[0])
	}
}
