package taiui

import (
	"testing"
)

func TestTabStateToggleAndAutoExpand(t *testing.T) {
	s := NewTabState(3)
	s.AutoExpand(0)
	if s.Focus != 0 || !s.Expanded[0] {
		t.Fatalf("first auto-expand should take focus: %+v", s)
	}
	// Auto-expansion never changes an established focus.
	s.AutoExpand(1)
	if s.Focus != 0 || !s.Expanded[1] {
		t.Fatalf("auto-expand must not steal focus: %+v", s)
	}
	// Toggling a focused tab collapses it; the focus moves to the most
	// recently focused expanded tab. Tab 1 was never focused (lastFocus -1),
	// so the index-order tie-break picks it.
	s.Toggle(0)
	if s.Expanded[0] {
		t.Fatal("focused tab should be collapsed")
	}
	if s.Focus != 1 {
		t.Fatalf("focus should fall back to the last-focused expanded tab, got %d", s.Focus)
	}
	// Toggling a collapsed tab expands it and takes focus. Tab 0 was
	// collapsed in the previous step, so the focus lands on 0.
	s.Toggle(0)
	if !s.Expanded[0] || s.Focus != 0 {
		t.Fatalf("toggling a collapsed tab should expand and focus it: %+v", s)
	}
	// To collapse tab 1, first move the focus onto it, then toggle the
	// now-focused tab: the first Toggle switches focus, the second
	// collapses it. Toggle on a non-focused tab never collapses it.
	s.Toggle(1)
	s.Toggle(1)
	if s.Expanded[1] {
		t.Fatal("tab 1 should be collapsed")
	}
	if s.Focus != 0 {
		t.Fatalf("focus should return to tab 0, got %d", s.Focus)
	}
	// Subsequent content does not re-expand a tab the user collapsed: the
	// HasContent guard short-circuits before the expansion branch.
	s.AutoExpand(1)
	if s.Expanded[1] {
		t.Fatal("subsequent content must not re-expand a collapsed tab")
	}
	if s.Focus != 0 {
		t.Fatalf("content arrival must not change the focus, got %d", s.Focus)
	}
}

func TestTabStateCycleSkipsCollapsed(t *testing.T) {
	s := NewTabState(3)
	s.AutoExpand(0)
	s.AutoExpand(2)
	if s.Focus != 0 {
		t.Fatalf("expected focus 0, got %d", s.Focus)
	}
	s.Cycle()
	if s.Focus != 2 {
		t.Fatalf("cycle should skip collapsed tab 1, got %d", s.Focus)
	}
	s.Cycle()
	if s.Focus != 0 {
		t.Fatalf("cycle should wrap to tab 0, got %d", s.Focus)
	}
	// With no expanded tabs the focus clears.
	s.Toggle(0)
	s.Toggle(2)
	s.Cycle()
	if s.Focus != -1 {
		t.Fatalf("focus should clear with no expanded tabs, got %d", s.Focus)
	}
}

func TestTabStateFocusOrderAfterCollapse(t *testing.T) {
	s := NewTabState(3)
	s.AutoExpand(0)
	s.AutoExpand(1)
	s.AutoExpand(2)
	s.Toggle(1)
	s.Toggle(0) // focus order: 0 was just focused, 1 was focused before
	// Collapse the now-focused tab 0; the most recently focused expanded
	// tab is 1.
	s.Toggle(0)
	if s.Focus != 1 {
		t.Fatalf("focus should follow the recency order, got %d", s.Focus)
	}
}

func TestTabLayoutVerticalSplit(t *testing.T) {
	// Focused pane gets 2x weight; collapsed panes take one column and
	// stay in place. Values mirror cmd/tai's pane geometry tests.
	l := TabLayout{Expanded: []bool{true, true, false}, Focus: 0, VerticalSplit: true}
	box := l.Boxes(90, 40)
	want := []Box{
		{Top: 0, Left: 0, Bottom: 40, Right: 59},
		{Top: 0, Left: 59, Bottom: 40, Right: 89},
		{Top: 0, Left: 89, Bottom: 40, Right: 90},
	}
	for i, b := range want {
		if box[i] != b {
			t.Fatalf("box %d: got %+v, want %+v", i, box[i], b)
		}
	}

	// Focusing the second pane swaps the proportions.
	l.Focus = 1
	box = l.Boxes(90, 40)
	want = []Box{
		{Top: 0, Left: 0, Bottom: 40, Right: 29},
		{Top: 0, Left: 29, Bottom: 40, Right: 89},
		{Top: 0, Left: 89, Bottom: 40, Right: 90},
	}
	for i, b := range want {
		if box[i] != b {
			t.Fatalf("box %d: got %+v, want %+v", i, box[i], b)
		}
	}

	// No focus: equal sharing, the last pane absorbs the remainder.
	l.Focus = -1
	box = l.Boxes(90, 40)
	want = []Box{
		{Top: 0, Left: 0, Bottom: 40, Right: 44},
		{Top: 0, Left: 44, Bottom: 40, Right: 89},
		{Top: 0, Left: 89, Bottom: 40, Right: 90},
	}
	for i, b := range want {
		if box[i] != b {
			t.Fatalf("box %d: got %+v, want %+v", i, box[i], b)
		}
	}

	// A collapsed pane in the middle keeps its position.
	l = TabLayout{Expanded: []bool{true, false, true}, Focus: 0, VerticalSplit: true}
	box = l.Boxes(90, 40)
	want = []Box{
		{Top: 0, Left: 0, Bottom: 40, Right: 59},
		{Top: 0, Left: 59, Bottom: 40, Right: 60},
		{Top: 0, Left: 60, Bottom: 40, Right: 90},
	}
	for i, b := range want {
		if box[i] != b {
			t.Fatalf("box %d: got %+v, want %+v", i, box[i], b)
		}
	}

	// All collapsed: each pane takes one column in order.
	l = TabLayout{Expanded: []bool{false, false, false}, Focus: -1, VerticalSplit: true}
	box = l.Boxes(80, 24)
	want = []Box{
		{Top: 0, Left: 0, Bottom: 24, Right: 1},
		{Top: 0, Left: 1, Bottom: 24, Right: 2},
		{Top: 0, Left: 2, Bottom: 24, Right: 3},
	}
	for i, b := range want {
		if box[i] != b {
			t.Fatalf("box %d: got %+v, want %+v", i, box[i], b)
		}
	}

	// A single expanded pane fills the remaining space.
	l = TabLayout{Expanded: []bool{false, true, false}, Focus: 1, VerticalSplit: true}
	box = l.Boxes(90, 40)
	want = []Box{
		{Top: 0, Left: 0, Bottom: 40, Right: 1},
		{Top: 0, Left: 1, Bottom: 40, Right: 89},
		{Top: 0, Left: 89, Bottom: 40, Right: 90},
	}
	for i, b := range want {
		if box[i] != b {
			t.Fatalf("box %d: got %+v, want %+v", i, box[i], b)
		}
	}
}

func TestTabLayoutHorizontalSplit(t *testing.T) {
	l := TabLayout{Expanded: []bool{true, true, false}, Focus: 0, VerticalSplit: false}
	box := l.Boxes(80, 45)
	want := []Box{
		{Top: 0, Left: 0, Bottom: 29, Right: 80},
		{Top: 29, Left: 0, Bottom: 44, Right: 80},
		{Top: 44, Left: 0, Bottom: 45, Right: 80},
	}
	for i, b := range want {
		if box[i] != b {
			t.Fatalf("box %d: got %+v, want %+v", i, box[i], b)
		}
	}
}

func TestScrollFollow(t *testing.T) {
	s := &ScrollFollow{Offset: 1 << 30, Follow: true}
	if got := s.Handle(10, 3); got != 7 {
		t.Fatalf("expected offset 7, got %d", got)
	}
	if !s.Follow {
		t.Fatal("follow must remain active at the tail")
	}
	// New content arrives while following: stick to the new tail.
	if got := s.Handle(12, 3); got != 9 {
		t.Fatalf("expected offset 9 after new content, got %d", got)
	}
	// Scrolling away clears follow.
	s.Scroll(-1)
	if s.Follow {
		t.Fatal("scrolling away must clear follow")
	}
	if s.Offset != 8 {
		t.Fatalf("expected offset 8, got %d", s.Offset)
	}
	if got := s.Handle(12, 3); got != 8 {
		t.Fatalf("scrolled-away offset must be preserved, got %d", got)
	}
	if s.Follow {
		t.Fatal("follow must stay cleared while scrolled away")
	}
	// Reaching the tail re-enables follow.
	s.Scroll(1)
	if got := s.Handle(12, 3); got != 9 {
		t.Fatalf("expected offset 9 at the tail, got %d", got)
	}
	if !s.Follow {
		t.Fatal("reaching the tail must re-enable follow")
	}
	// ResumeFollow sticks to the tail again.
	got := s.ResumeFollow(12, 3)
	if got != 9 {
		t.Fatalf("expected resumed offset 9, got %d", got)
	}
	// ScrollTo jumps to a desired row: a non-tail jump is preserved and
	// stops following.
	s.ScrollTo(2)
	if s.Offset != 2 || s.Follow {
		t.Fatalf("ScrollTo should set the offset and clear follow, got %+v", s)
	}
	if got := s.Handle(12, 3); got != 2 {
		t.Fatalf("non-tail jump must be preserved, got %d", got)
	}
	if s.Follow {
		t.Fatal("follow must stay cleared after a non-tail jump")
	}
	// The 1<<30 sentinel marks the end: Handle clamps it to the tail and
	// re-enables follow.
	s.ScrollTo(1 << 30)
	if s.Follow {
		t.Fatal("ScrollTo must clear follow until the handle re-enables it")
	}
	if got := s.Handle(12, 3); got != 9 {
		t.Fatalf("sentinel jump must land on the tail, got %d", got)
	}
	if !s.Follow {
		t.Fatal("landing on the tail must re-enable follow")
	}
	s.ScrollTo(7)
	if got := s.Handle(12, 3); got != 7 {
		t.Fatalf("non-tail jump must be preserved, got %d", got)
	}
	if s.Follow {
		t.Fatal("a jump below the tail must stop following")
	}
	// Clamping: past-the-start clamps to 0, content shorter than the
	// window clamps to 0.
	s.Scroll(-100)
	if got := s.Handle(12, 3); got != 0 {
		t.Fatalf("expected offset 0, got %d", got)
	}
	if got := s.Handle(2, 3); got != 0 {
		t.Fatalf("expected offset 0 for short content, got %d", got)
	}
}

func TestScrollFollowPage(t *testing.T) {
	s := &ScrollFollow{Offset: 0, Follow: false}
	s.Handle(10, 3)
	// One page is windowHeight - 1 = 2, so one line of the previous view
	// remains visible.
	s.Page(1)
	if got := s.Handle(10, 3); got != 2 {
		t.Fatalf("expected page-down offset 2, got %d", got)
	}
	s.Page(-1)
	if got := s.Handle(10, 3); got != 0 {
		t.Fatalf("expected page-up offset 0, got %d", got)
	}
}
