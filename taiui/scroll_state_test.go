package taiui

import "testing"

func TestClampOffset(t *testing.T) {
	if got := ClampOffset(0, 10, 3); got != 0 {
		t.Fatalf("offset 0 should be unchanged, got %d", got)
	}
	if got := ClampOffset(7, 10, 3); got != 7 {
		t.Fatalf("offset 7 (the max) should be unchanged, got %d", got)
	}
	if got := ClampOffset(8, 10, 3); got != 7 {
		t.Fatalf("offset 8 should clamp to 7, got %d", got)
	}
	if got := ClampOffset(100, 10, 3); got != 7 {
		t.Fatalf("offset 100 should clamp to 7, got %d", got)
	}
	if got := ClampOffset(1<<30, 10, 3); got != 7 {
		t.Fatalf("tail sentinel should clamp to 7, got %d", got)
	}
	if got := ClampOffset(-5, 10, 3); got != 0 {
		t.Fatalf("negative offset should clamp to 0, got %d", got)
	}
	if got := ClampOffset(0, 2, 3); got != 0 {
		t.Fatalf("fitted content should clamp to 0, got %d", got)
	}
}

func TestScrollStateUpdate(t *testing.T) {
	s := &ScrollState{Offset: 1 << 30, Follow: true}
	s.Update(100, 3)
	if s.MaxOffset != 97 {
		t.Fatalf("expected max offset 97, got %d", s.MaxOffset)
	}
	if s.Offset != 97 {
		t.Fatalf("expected offset 97 while following, got %d", s.Offset)
	}
	if !s.Follow {
		t.Fatal("expected follow while at the tail")
	}

	s = &ScrollState{Offset: 10, Follow: false}
	s.Update(100, 3)
	if s.Offset != 10 {
		t.Fatalf("expected offset 10 preserved, got %d", s.Offset)
	}

	s = &ScrollState{Offset: 200, Follow: false}
	s.Update(100, 3)
	if s.Offset != 97 {
		t.Fatalf("expected offset clamped to 97, got %d", s.Offset)
	}

	s = &ScrollState{Offset: 97, Follow: false}
	s.Update(100, 3)
	if !s.Follow {
		t.Fatal("expected follow restored at the max offset")
	}
}

func TestScrollStateScroll(t *testing.T) {
	s := &ScrollState{Offset: 10, MaxOffset: 97}
	s.Scroll(1)
	if s.Offset != 11 || s.Follow {
		t.Fatalf("expected offset 11 and no follow, got %d follow %v", s.Offset, s.Follow)
	}
	s.Scroll(100)
	if s.Offset != 97 || !s.Follow {
		t.Fatalf("expected offset 97 and follow, got %d follow %v", s.Offset, s.Follow)
	}
	s.Scroll(-100)
	if s.Offset != 0 || s.Follow {
		t.Fatalf("expected offset 0 and no follow, got %d follow %v", s.Offset, s.Follow)
	}
}

func TestScrollStateScrollTo(t *testing.T) {
	s := &ScrollState{Offset: 10, MaxOffset: 97}
	s.ScrollTo(1 << 30)
	if s.Offset != 97 || !s.Follow {
		t.Fatalf("expected tail offset 97 and follow, got %d follow %v", s.Offset, s.Follow)
	}
	s.ScrollTo(0)
	if s.Offset != 0 || s.Follow {
		t.Fatalf("expected offset 0 and no follow, got %d follow %v", s.Offset, s.Follow)
	}
}

func TestScrollStatePageScroll(t *testing.T) {
	s := &ScrollState{Offset: 0, MaxOffset: 90}
	s.PageScroll(1, 7)
	if s.Offset != 6 {
		t.Fatalf("expected offset 6 after page down, got %d", s.Offset)
	}
	s.PageScroll(-1, 7)
	if s.Offset != 0 {
		t.Fatalf("expected offset 0 after page up, got %d", s.Offset)
	}
}
