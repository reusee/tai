package main

import (
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestReadKeys(t *testing.T) {
	ch := make(chan string, 8)
	go readKeys(strings.NewReader("\x1b[Aqm \t\x1b[B"), ch)
	var got []string
	for len(got) < 6 {
		select {
		case k := <-ch:
			got = append(got, k)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for keys")
		}
	}
	want := []string{"up", "quit", "modal", "space", "tab", "down"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestReadKeysLoneEsc(t *testing.T) {
	ch := make(chan string, 8)
	go readKeys(strings.NewReader("\x1b"), ch)
	// A lone ESC that never grows into a sequence is discarded: the
	// incomplete sequence never resolves to a key.
	select {
	case k := <-ch:
		t.Fatalf("expected no key, got %q", k)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestReadKeysEscThenKey(t *testing.T) {
	ch := make(chan string, 8)
	go readKeys(strings.NewReader("\x1bq"), ch)
	// ESC followed by a non-sequence byte: the ESC is discarded and the
	// following key is processed, so 'q' quits the demo.
	select {
	case k := <-ch:
		if k != "quit" {
			t.Fatalf("expected quit, got %q", k)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for quit")
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
