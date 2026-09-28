package main

import (
	"testing"
)

func TestHandleKeyScrollClamp(t *testing.T) {
	s := &State{Toggle: true, W1Weight: 1}
	changed, quit := s.HandleKey("up")
	if quit {
		t.Fatal("up must not quit the demo")
	}
	if changed {
		t.Fatalf("expected no change for clamped up, got %v", changed)
	}
	changed, quit = s.HandleKey("down")
	if quit {
		t.Fatal("down must not quit the demo")
	}
	if !changed {
		t.Fatal("expected a change for down")
	}
	if s.Scroll != 1 {
		t.Fatalf("expected scroll 1 after down, got %d", s.Scroll)
	}
	changed, quit = s.HandleKey("space")
	if quit {
		t.Fatal("space must not quit the demo")
	}
	if !changed {
		t.Fatal("expected a change for space")
	}
	if s.Toggle {
		t.Fatal("expected toggle flipped by space")
	}
	_, quit = s.HandleKey("quit")
	if !quit {
		t.Fatal("quit must stop the demo")
	}
}

func TestHandleKeyW1Weight(t *testing.T) {
	s := &State{Toggle: true, W1Weight: 1}
	changed, quit := s.HandleKey("left")
	if quit {
		t.Fatal("left must not quit the demo")
	}
	if changed {
		t.Fatalf("expected no change for clamped left, got %v", changed)
	}
	changed, quit = s.HandleKey("right")
	if quit {
		t.Fatal("right must not quit the demo")
	}
	if !changed {
		t.Fatal("expected a change for right")
	}
	if s.W1Weight != 2 {
		t.Fatalf("expected w1 weight 2 after right, got %d", s.W1Weight)
	}
	for i := 0; i < maxW1Weight; i++ {
		s.HandleKey("right")
	}
	changed, quit = s.HandleKey("right")
	if quit {
		t.Fatal("right must not quit the demo")
	}
	if changed {
		t.Fatalf("expected no change at upper clamp, got %v", changed)
	}
}

func TestHandleKeyModal(t *testing.T) {
	s := &State{Toggle: true, W1Weight: 1}
	changed, quit := s.HandleKey("modal")
	if quit {
		t.Fatal("modal must not quit the demo")
	}
	if !changed {
		t.Fatal("expected a change for modal")
	}
	if !s.Modal {
		t.Fatal("expected modal toggled by m")
	}
	changed, quit = s.HandleKey("modal")
	if quit {
		t.Fatal("modal must not quit the demo")
	}
	if !changed {
		t.Fatal("expected a change for modal")
	}
	if s.Modal {
		t.Fatal("expected modal toggled back by m")
	}
}

func TestMapDemoKey(t *testing.T) {
	for in, want := range map[string]string{
		"q":                  "quit",
		"Q":                  "quit",
		"ctrl-c":             "quit",
		" ":                  "space",
		"m":                  "modal",
		"M":                  "modal",
		"\t":                 "tab",
		"up":                 "up",
		"mouse-left@1,2":     "mouse-left@1,2",
		"mouse-wheel-up@1,2": "mouse-wheel-up@1,2",
	} {
		if got := mapDemoKey(in); got != want {
			t.Fatalf("mapDemoKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHandleKeyRotation(t *testing.T) {
	s := &State{Toggle: true, W1Weight: 1}
	changed, quit := s.HandleKey("tab")
	if quit {
		t.Fatal("tab must not quit the demo")
	}
	if !changed {
		t.Fatal("expected a change for tab")
	}
	if s.Rotation != 1 {
		t.Fatalf("expected rotation 1 after tab, got %d", s.Rotation)
	}
	// Three more presses wrap the rotation back to 0.
	for i := 0; i < 3; i++ {
		changed, quit = s.HandleKey("tab")
		if quit {
			t.Fatal("tab must not quit the demo")
		}
		if !changed {
			t.Fatal("expected a change for tab")
		}
	}
	if s.Rotation != 0 {
		t.Fatalf("expected rotation wrapped to 0, got %d", s.Rotation)
	}
}

func TestRotatedPanelIndex(t *testing.T) {
	// Position p shows the panel originally at (p - rotation) mod 4, in
	// clockwise order: 0 top-left, 1 top-right, 2 bottom-right,
	// 3 bottom-left.
	cases := []struct{ p, rotation, want int }{
		{0, 0, 0}, {1, 0, 1}, {2, 0, 2}, {3, 0, 3},
		{0, 1, 3}, {1, 1, 0}, {2, 1, 1}, {3, 1, 2},
		{0, 2, 2}, {1, 2, 3}, {2, 2, 0}, {3, 2, 1},
		{0, 3, 1}, {1, 3, 2}, {2, 3, 3}, {3, 3, 0},
		{0, 4, 0}, // four presses return to the original arrangement
	}
	for _, c := range cases {
		if got := rotatedPanelIndex(c.p, c.rotation); got != c.want {
			t.Fatalf("rotatedPanelIndex(%d, %d) = %d, want %d", c.p, c.rotation, got, c.want)
		}
	}
}

func TestRuneWidthEnv(t *testing.T) {
	t.Setenv("RUNEWIDTH_EASTASIAN", "")
	if got := runeWidthEnv(); got != "EA=narrow" {
		t.Fatalf("expected EA=narrow, got %q", got)
	}
	t.Setenv("RUNEWIDTH_EASTASIAN", "1")
	if got := runeWidthEnv(); got != "EA=wide" {
		t.Fatalf("expected EA=wide, got %q", got)
	}
}

func TestBounce(t *testing.T) {
	// bounce maps a counter onto a 0..span sawtooth, so the ball appears
	// to bounce between the canvas edges.
	cases := []struct {
		v, want int
	}{
		{0, 0}, {1, 1}, {2, 2}, {3, 3}, {4, 2}, {5, 1}, {6, 0},
	}
	for _, c := range cases {
		if got := bounce(c.v, 3); got != c.want {
			t.Fatalf("bounce(%d, 3) = %d, want %d", c.v, got, c.want)
		}
	}
}

func TestHandlePointer(t *testing.T) {
	s := &State{Toggle: true, W1Weight: 1}
	// The wheel scrolls the pane and clamps at the content start.
	if changed, quit := s.HandleKey("mouse-wheel-up@1,2"); changed || quit {
		t.Fatal("a wheel up at the content start must change nothing")
	}
	if changed, quit := s.HandleKey("mouse-wheel-down@1,2"); !changed || quit || s.Scroll != 1 {
		t.Fatalf("expected the wheel down to scroll, got changed=%v quit=%v scroll=%d", changed, quit, s.Scroll)
	}
	// A tap fires on the release, not on the press.
	s.HandleKey("mouse-left@5,5")
	if !s.Toggle {
		t.Fatal("the tap action must fire on the release, not on the press")
	}
	s.HandleKey("mouse-release@5,5")
	if s.Toggle {
		t.Fatal("the tap must toggle the state")
	}
	// A swipe scrolls with the pointer, anchored to the press origin,
	// and cancels the pending tap.
	s.Toggle = true
	s.Scroll = 0
	s.HandleKey("mouse-left@5,10")
	s.HandleKey("mouse-leftdrag@5,7")
	if s.Scroll != 3 {
		t.Fatalf("expected the swipe to scroll to 3, got %d", s.Scroll)
	}
	s.HandleKey("mouse-release@5,7")
	if !s.Toggle {
		t.Fatal("a swipe must not toggle the state")
	}
	// The swipe clamps at the content start.
	s.HandleKey("mouse-left@5,3")
	s.HandleKey("mouse-leftdrag@5,53")
	if s.Scroll != 0 {
		t.Fatalf("expected the swipe to clamp at 0, got %d", s.Scroll)
	}
}

func TestBuildRoot(t *testing.T) {
	// BuildRoot turns the current state into a root element tree,
	// including the small-screen banner and the modal overlay.
	if root := BuildRoot(State{Width: 80, Height: 24, Toggle: true, W1Weight: 1}); root == nil {
		t.Fatal("expected a root element")
	}
	root := BuildRoot(State{Width: 80, Height: 24, Toggle: true, W1Weight: 1, Modal: true})
	if root == nil {
		t.Fatal("expected a root element with the modal open")
	}
	root = BuildRoot(State{Width: 10, Height: 10, Toggle: true, W1Weight: 1})
	if root == nil {
		t.Fatal("expected a root element for a small screen")
	}
}
