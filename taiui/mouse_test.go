package taiui

import "testing"

func TestParseMouseKey(t *testing.T) {
	event, x, y, ok := ParseMouseKey("mouse-left@12,34")
	if !ok || event != "left" || x != 12 || y != 34 {
		t.Fatalf("got %q, %d, %d, %v", event, x, y, ok)
	}
	event, x, y, ok = ParseMouseKey("mouse-wheel-up@0,0")
	if !ok || event != "wheel-up" || x != 0 || y != 0 {
		t.Fatalf("got %q, %d, %d, %v", event, x, y, ok)
	}
	event, x, y, ok = ParseMouseKey("mouse-leftdrag@3,4")
	if !ok || event != "leftdrag" || x != 3 || y != 4 {
		t.Fatalf("got %q, %d, %d, %v", event, x, y, ok)
	}
	event, x, y, ok = ParseMouseKey("mouse-release@80,44")
	if !ok || event != "release" || x != 80 || y != 44 {
		t.Fatalf("got %q, %d, %d, %v", event, x, y, ok)
	}
	for _, bad := range []string{
		"mouse-left",     // no coordinates
		"mouse-left@a,b", // non-numeric coordinates
		"mouse-left@1",   // missing the y coordinate
		"plain@1,2",      // missing the mouse prefix
		"mouse-@1,2",     // empty event kind
	} {
		if _, _, _, ok := ParseMouseKey(bad); ok {
			t.Fatalf("expected %q to be invalid", bad)
		}
	}
}

func TestTabAt(t *testing.T) {
	tabs := NewTabs(3)
	tabs.Expanded = []bool{true, true, false}
	tabs.Focus = 0
	cases := []struct {
		x, y, want int
	}{
		{5, 10, 0},
		{5, 40, 1},
		{5, 44, 2},
		{-1, 0, -1},
	}
	for _, tc := range cases {
		if got := TabAt(tabs, 80, 45, tc.x, tc.y); got != tc.want {
			t.Fatalf("TabAt(%d,%d) = %d, want %d", tc.x, tc.y, got, tc.want)
		}
	}
}

func TestTabMousePress(t *testing.T) {
	t.Run("CollapsedStripExpandsAndFocuses", func(t *testing.T) {
		tabs := NewTabs(3)
		tabs.Expanded = []bool{true, false, false}
		tabs.Focus = 0
		var scrolls [3]ScrollState
		var m TabMouse
		m.Press(tabs, scrolls[:], 80, 45, 5, 43)
		if !tabs.Expanded[1] {
			t.Fatal("pressing a collapsed tab's strip must expand it")
		}
		if tabs.Focus != 1 {
			t.Fatalf("expected the focus on the pressed tab, got %d", tabs.Focus)
		}
		if !scrolls[1].Follow {
			t.Fatal("expanding must resume following the tail")
		}
	})

	t.Run("FocusedStripCollapses", func(t *testing.T) {
		tabs := NewTabs(3)
		tabs.Expanded = []bool{true, false, false}
		tabs.Focus = 0
		var scrolls [3]ScrollState
		var m TabMouse
		m.Press(tabs, scrolls[:], 80, 45, 5, 0)
		if tabs.Expanded[0] {
			t.Fatal("pressing the focused tab's label strip must collapse it")
		}
		if tabs.Focus != -1 {
			t.Fatalf("expected no focused tab after collapsing, got %d", tabs.Focus)
		}
	})

	t.Run("NonFocusedStripFocuses", func(t *testing.T) {
		tabs := NewTabs(3)
		tabs.Expanded = []bool{true, true, false}
		tabs.Focus = 0
		var scrolls [3]ScrollState
		var m TabMouse
		m.Press(tabs, scrolls[:], 80, 45, 5, 33)
		if !tabs.Expanded[1] {
			t.Fatal("the pressed tab must stay expanded")
		}
		if tabs.Focus != 1 {
			t.Fatalf("expected the focus on the pressed tab, got %d", tabs.Focus)
		}
	})

	t.Run("ScrollAreaFocusesAndDragScrolls", func(t *testing.T) {
		tabs := NewTabs(3)
		tabs.Expanded = []bool{true, true, false}
		tabs.Focus = 1
		var scrolls [3]ScrollState
		scrolls[0].MaxOffset = 100
		scrolls[0].Offset = 10
		var m TabMouse

		m.Press(tabs, scrolls[:], 80, 45, 5, 10)
		if tabs.Focus != 0 {
			t.Fatalf("a press inside the scroll area focuses the tab, got %d", tabs.Focus)
		}

		// Dragging up reveals earlier content.
		m.Drag(tabs, scrolls[:], 5)
		if scrolls[0].Offset != 15 {
			t.Fatalf("expected offset 15 after dragging up, got %d", scrolls[0].Offset)
		}
		// Dragging down reveals the tail.
		m.Drag(tabs, scrolls[:], 15)
		if scrolls[0].Offset != 5 {
			t.Fatalf("expected offset 5 after dragging down, got %d", scrolls[0].Offset)
		}
		// The drag offset clamps at the content extent.
		m.Drag(tabs, scrolls[:], 200)
		if scrolls[0].Offset != 0 {
			t.Fatalf("expected offset 0 after clamping, got %d", scrolls[0].Offset)
		}
		m.Release()
		m.Drag(tabs, scrolls[:], 0)
		if scrolls[0].Offset != 0 {
			t.Fatalf("expected the offset unchanged after release, got %d", scrolls[0].Offset)
		}
	})
}

func TestTabMouseWheel(t *testing.T) {
	tabs := NewTabs(3)
	tabs.Expanded = []bool{true, false, true}
	tabs.Focus = 0
	var scrolls [3]ScrollState
	scrolls[2].MaxOffset = 100
	scrolls[2].Offset = 50
	var m TabMouse

	m.Wheel(tabs, scrolls[:], 80, 45, 5, 40, 1)
	if scrolls[2].Offset != 51 {
		t.Fatalf("expected offset 51 after wheel down, got %d", scrolls[2].Offset)
	}
	if tabs.Focus != 0 {
		t.Fatalf("wheel must not change the focus, got %d", tabs.Focus)
	}
	m.Wheel(tabs, scrolls[:], 80, 45, 5, 40, -1)
	if scrolls[2].Offset != 50 {
		t.Fatalf("expected offset 50 after wheel up, got %d", scrolls[2].Offset)
	}

	// A wheel over the collapsed summary strip (row 33) is a no-op.
	m.Wheel(tabs, scrolls[:], 80, 45, 5, 33, 1)
	if scrolls[2].Offset != 50 || scrolls[1].Offset != 0 || tabs.Focus != 0 {
		t.Fatal("wheel over a collapsed tab must be a no-op")
	}
}
