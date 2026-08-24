package taiui

import (
	"strings"
	"testing"
)

func TestQuitConfirm(t *testing.T) {
	var q QuitConfirm
	if q.Pending() {
		t.Fatal("a fresh confirm must not be pending")
	}
	if q.QuitKeyPressed() {
		t.Fatal("the first quit key press must not quit")
	}
	if !q.Pending() {
		t.Fatal("the first quit key press must arm the confirmation")
	}
	if !q.QuitKeyPressed() {
		t.Fatal("the second quit key press must confirm the quit")
	}
	q.Cancel()
	if q.Pending() {
		t.Fatal("Cancel must disarm the confirmation")
	}
	if q.QuitKeyPressed() {
		t.Fatal("a quit key after cancellation must not quit immediately")
	}
	if !q.Pending() {
		t.Fatal("a quit key after cancellation must re-arm the confirmation")
	}
}

func TestQuitConfirmBar(t *testing.T) {
	var sb strings.Builder
	Render(QuitConfirmBar(40, 10), NewTerminalScreen(&sb, 40, 10))
	if !strings.Contains(sb.String(), "Quit?") {
		t.Fatalf("expected the confirmation bar, got %q", sb.String())
	}
}

func TestHelpOverlay(t *testing.T) {
	lines := []string{"q\tquit", "s\tsplit"}
	var sb strings.Builder
	Render(HelpOverlay(lines, 16, 40, 10), NewTerminalScreen(&sb, 40, 10))
	out := sb.String()
	for _, want := range []string{"Help", "quit", "split"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in the help overlay, got %q", want, out)
		}
	}
	// A tiny screen clamps the overlay without panicking.
	Render(HelpOverlay(lines, 16, 4, 3), NewTerminalScreen(&strings.Builder{}, 4, 3))
}
