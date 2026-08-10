package main

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3/vt"
	"github.com/reusee/tai/taiui"
)

func TestSgrResetsState(t *testing.T) {
	// A style carrying only a background must clear attributes a previous
	// cell may have set: the SGR sequence starts with the reset parameter.
	// Without it, an underlined or overlined run would bleed into every
	// following cell that shares the same background.
	style := vt.BaseStyle.WithBg(taiui.HexColor(0x141414))
	seq := sgr(style)
	if !strings.HasPrefix(seq, "\x1b[0;") {
		t.Fatalf("expected reset prefix, got %q", seq)
	}
	if !strings.Contains(seq, "48") {
		t.Fatalf("expected background parameter, got %q", seq)
	}
}

func TestSgrUnderlineColor(t *testing.T) {
	// The demo styles a dashed underline with an underline color; the
	// presenter must emit it so the terminal renders the specified color.
	style := vt.BaseStyle.
		WithAttr(vt.DashedUnderline).
		WithUc(taiui.HexColor(0x00ffff))
	seq := sgr(style)
	if !strings.Contains(seq, "4:5") {
		t.Fatalf("expected dashed underline parameter, got %q", seq)
	}
	if !strings.Contains(seq, "58") {
		t.Fatalf("expected underline color parameter, got %q", seq)
	}
}

func TestHandleKeyScrollClamp(t *testing.T) {
	scroll := 0
	toggle := true
	changed, quit := handleKey(&scroll, &toggle, "up")
	if quit {
		t.Fatal("up must not quit the demo")
	}
	if scroll != 0 {
		t.Fatalf("expected scroll clamped at 0, got %d", scroll)
	}
	if len(changed) != 0 {
		t.Fatalf("expected no provider for clamped up, got %d", len(changed))
	}
	changed, quit = handleKey(&scroll, &toggle, "down")
	if quit {
		t.Fatal("down must not quit the demo")
	}
	if scroll != 1 {
		t.Fatalf("expected scroll 1 after down, got %d", scroll)
	}
	if len(changed) != 1 {
		t.Fatalf("expected one provider after down, got %d", len(changed))
	}
	changed, quit = handleKey(&scroll, &toggle, "space")
	if quit {
		t.Fatal("space must not quit the demo")
	}
	if toggle {
		t.Fatal("expected toggle flipped by space")
	}
	if len(changed) != 1 {
		t.Fatalf("expected one provider after space, got %d", len(changed))
	}
	_, quit = handleKey(&scroll, &toggle, "quit")
	if !quit {
		t.Fatal("quit must stop the demo")
	}
}
