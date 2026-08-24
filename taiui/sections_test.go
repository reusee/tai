package taiui

import "testing"

func TestTransitionBoundaries(t *testing.T) {
	if got := TransitionBoundaries(nil); len(got) != 0 {
		t.Fatalf("expected no boundaries for no lines, got %v", got)
	}

	red := HexColor(0xff0000)
	green := HexColor(0x00ff00)
	lines := []Line{
		{Text: "a", Color: red},
		{Text: "b", Color: red},
		{Text: "c", Color: NoColor},
		{Text: "d", Color: green},
		{Text: "e", Color: green},
	}
	got := TransitionBoundaries(lines)
	if len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("expected boundaries [2 3], got %v", got)
	}

	// The first display line is never a boundary.
	if got := TransitionBoundaries([]Line{{Text: "a", Color: red}}); len(got) != 0 {
		t.Fatalf("expected no boundaries for a single line, got %v", got)
	}
}

func TestTransitionJumpStops(t *testing.T) {
	red := HexColor(0xff0000)
	lines := []Line{
		{Text: "a", Color: red},
		{Text: "b", Color: NoColor},
		{Text: "c", Color: NoColor},
		{Text: "d", Color: red},
	}
	// Boundaries sit at display indices 1 and 3; with a pane height of
	// 2 each boundary contributes an exit stop (boundary-2, clamped to
	// the content start) and an entry stop (the boundary itself).
	got := TransitionJumpStops(lines, 2)
	want := []int{0, 1, 1, 3}
	if len(got) != len(want) {
		t.Fatalf("expected stops %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected stops %v, got %v", want, got)
		}
	}
}

func TestJumpStopOffset(t *testing.T) {
	stops := []int{0, 1, 1, 3}
	cases := []struct {
		from, dir, want int
		wantOK          bool
	}{
		{2, -1, 1, true},
		{1, -1, 0, true},
		{0, -1, 0, true},
		{0, 1, 1, true},
		{1, 1, 3, true},
		{3, 1, 0, false},
	}
	for _, tc := range cases {
		got, ok := JumpStopOffset(stops, tc.from, tc.dir)
		if ok != tc.wantOK || (ok && got != tc.want) {
			t.Fatalf("JumpStopOffset(from %d, dir %d) = (%d, %v), want (%d, %v)",
				tc.from, tc.dir, got, ok, tc.want, tc.wantOK)
		}
	}

	// Backward navigation falls back to the content start, so the
	// beginning is always reachable; forward navigation reports none.
	if got, ok := JumpStopOffset(nil, 5, -1); !ok || got != 0 {
		t.Fatalf("backward navigation must fall back to 0, got (%d, %v)", got, ok)
	}
	if _, ok := JumpStopOffset(nil, 5, 1); ok {
		t.Fatal("forward navigation with no stops must report none")
	}
}
