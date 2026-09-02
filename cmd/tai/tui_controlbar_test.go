package main

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/gdamore/tcell/v3/tty"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/taiui"
)

// fakeTtyForTest stands in for the terminal where a test reaches the
// quit exit path: only the Write the path performs matters.
type fakeTtyForTest struct {
	buf bytes.Buffer
}

func (f *fakeTtyForTest) Start() error             { return nil }
func (f *fakeTtyForTest) Stop() error              { return nil }
func (f *fakeTtyForTest) Drain() error             { return nil }
func (f *fakeTtyForTest) NotifyResize(chan<- bool) {}
func (f *fakeTtyForTest) WindowSize() (tty.WindowSize, error) {
	return tty.WindowSize{Width: 80, Height: 24}, nil
}
func (f *fakeTtyForTest) Read([]byte) (int, error) { return 0, io.EOF }
func (f *fakeTtyForTest) Write(p []byte) (int, error) {
	return f.buf.Write(p)
}
func (f *fakeTtyForTest) Close() error { return nil }

// TestControlBarLayout pins the pure layout: ordered two-cell slots,
// hit-test agreement, off-bar inertness, and the mouse-state glyph.
// See TheoryOfControlBar.
func TestControlBarLayout(t *testing.T) {
	glyphs := controlBarLayout(80, true)
	want := []controlBarAction{
		controlPrevSections, controlNextSections, controlCollapseAll,
		controlSplitToggle, controlMouseToggle, controlHelpToggle, controlQuit,
	}
	if len(glyphs) != len(want) {
		t.Fatalf("expected %d glyphs, got %d", len(want), len(glyphs))
	}
	for i, g := range glyphs {
		if g.action != want[i] {
			t.Fatalf("glyph %d is %q, want %q", i, g.action, want[i])
		}
		if g.x1-g.x0 != 2 {
			t.Fatalf("glyph %d slot is %d cells, want 2", i, g.x1-g.x0)
		}
		action, ok := controlBarHit(1, 80, g.x0, 0, true)
		if !ok || action != g.action {
			t.Fatalf("hit at %d resolved %q ok=%v, want %q", g.x0, action, ok, g.action)
		}
		action, _ = controlBarHit(1, 80, g.x1-1, 0, true)
		if action != g.action {
			t.Fatalf("hit at %d resolved %q, want %q", g.x1-1, action, g.action)
		}
	}
	if _, ok := controlBarHit(1, 80, 2, 0, true); ok {
		t.Fatal("the separator column between slots is inert")
	}
	if _, ok := controlBarHit(1, 80, 0, 1, true); ok {
		t.Fatal("row 1 is not the control bar")
	}
	if _, ok := controlBarHit(0, 80, 0, 0, true); ok {
		t.Fatal("no inset means no control bar")
	}
	off := controlBarLayout(80, false)
	if glyphs[4].glyph != "◉" || off[4].glyph != "◎" {
		t.Fatalf("mouse glyphs: on %q off %q", glyphs[4].glyph, off[4].glyph)
	}
}

// TestTUIControlBarClicks drives each bar action through
// handleMouseKey, the path the session's key loop takes. See
// TheoryOfControlBar.
func TestTUIControlBarClicks(t *testing.T) {
	newBar := func() *TUI {
		tui := newTUIForTest()
		tui.width, tui.height = 80, 24
		tui.tabs.TopInset = 1
		return tui
	}

	t.Run("Split", func(t *testing.T) {
		tui := newBar()
		tui.handleMouseKey("mouse-left@9,0")
		if !tui.tabs.SplitVertical {
			t.Fatal("the split glyph must toggle the split axis")
		}
	})

	t.Run("Help", func(t *testing.T) {
		tui := newBar()
		tui.handleMouseKey("mouse-left@15,0")
		if !tui.showHelp {
			t.Fatal("the help glyph must open the help overlay")
		}
		tui.handleMouseKey("mouse-left@15,0")
		if tui.showHelp {
			t.Fatal("the help glyph must close the help overlay")
		}
	})

	t.Run("MouseToggle", func(t *testing.T) {
		tui := newBar()
		tui.mouseReporting = true
		tui.handleMouseKey("mouse-left@12,0")
		if tui.mouseReporting {
			t.Fatal("the mouse glyph must toggle reporting off")
		}
	})

	t.Run("CollapseAll", func(t *testing.T) {
		tui := newBar()
		tui.writeOutputPart(generators.RoleUser, outputColorUserLine, false, "a\n")
		tui.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "b\n")
		tui.handleMouseKey("mouse-left@6,0")
		for i := range tui.outputSections {
			if !tui.outputSections[i].collapsed {
				t.Fatalf("section %d must be collapsed", i)
			}
		}
	})

	t.Run("QuitConfirmsOnSecondPress", func(t *testing.T) {
		tui := newBar()
		tui.tty = &fakeTtyForTest{}
		if tui.handleMouseKey("mouse-left@18,0") {
			t.Fatal("the first quit press must only arm the confirmation")
		}
		if !tui.quit.Pending() {
			t.Fatal("the first quit press must arm the confirmation")
		}
		if !tui.handleMouseKey("mouse-left@18,0") {
			t.Fatal("the second quit press must confirm the quit")
		}
	})

	t.Run("OtherPressCancelsQuit", func(t *testing.T) {
		tui := newBar()
		tui.handleMouseKey("mouse-left@18,0")
		tui.handleMouseKey("mouse-left@5,5")
		if tui.quit.Pending() {
			t.Fatal("a press elsewhere must cancel the confirmation")
		}
	})
}

// TestTUISubmitGlyphClick pins the pointer submit path: a press on the
// bar's right-end glyph delivers the typed line while a ChatInput call
// waits and keeps the line otherwise. See TheoryOfControlBar.
func TestTUISubmitGlyphClick(t *testing.T) {
	tui := newTUIForTest()
	tui.width, tui.height = 80, 24
	tui.tabs.TopInset = 1
	tui.interactive = true
	tui.tabs.Expanded[0] = true
	tui.tabs.Focus = 0

	ch := make(chan chatInputResult, 1)
	tui.mu.Lock()
	tui.inputBar.Prompt = ">> "
	tui.inputResult = ch
	box := tui.tabs.Boxes(tui.width, tui.height)[0]
	tui.mu.Unlock()
	tui.handleMouseKey(fmt.Sprintf("mouse-left@%d,%d", box.Right-1, box.Bottom-1))
	res := <-ch
	if !res.ok || res.line != "" {
		t.Fatalf("expected an empty submitted line, got ok=%v line=%q", res.ok, res.line)
	}

	tui.mu.Lock()
	tui.inputFocused = true
	tui.inputBar.Insert('a')
	tui.mu.Unlock()
	tui.handleMouseKey(fmt.Sprintf("mouse-left@%d,%d", box.Right-1, box.Bottom-1))
	tui.mu.Lock()
	line := tui.inputBar.Line()
	tui.mu.Unlock()
	if line != "a" {
		t.Fatalf("the line must be kept without a waiting call, got %q", line)
	}
}

// TestTUIHelpClickCloses pins the overlay's click-to-close: a press
// inside the help box closes it, a press outside does not. See
// TheoryOfControlBar.
func TestTUIHelpClickCloses(t *testing.T) {
	tui := newTUIForTest()
	tui.width, tui.height = 80, 24
	tui.tabs.TopInset = 1
	tui.showHelp = true
	box := taiui.HelpOverlayBox(tui.helpLines(), tui.width, tui.height)
	tui.handleMouseKey(fmt.Sprintf("mouse-left@%d,%d", box.Left+2, box.Top+2))
	if tui.showHelp {
		t.Fatal("a press inside the help overlay must close it")
	}
	tui.showHelp = true
	tui.handleMouseKey(fmt.Sprintf("mouse-left@%d,%d", box.Left-1, box.Top+2))
	if !tui.showHelp {
		t.Fatal("a press outside the help overlay must not close it")
	}
}
