package main

import (
	"testing"

	vt "github.com/gdamore/tcell/v3/vt"
	"github.com/reusee/tai/taiui"
)

// menuHoverCaptureScreen records the frames Render presents, so tests
// inspect rendered cell styles.
type menuHoverCaptureScreen struct {
	w, h   int
	frames []taiui.Frame
}

func (s *menuHoverCaptureScreen) Width() int  { return s.w }
func (s *menuHoverCaptureScreen) Height() int { return s.h }
func (s *menuHoverCaptureScreen) Present(f taiui.Frame) {
	s.frames = append(s.frames, f)
}

// menuHoverReversedRunes returns row y's runes whose cell style
// carries the reverse attribute.
func menuHoverReversedRunes(f *taiui.Frame, y int) string {
	var out []rune
	for x := 0; x < f.Width; x++ {
		cell := f.Cells[y*f.Width+x]
		if cell.Set && cell.Style != nil && cell.Style.Attr()&vt.Reverse != 0 {
			out = append(out, cell.Rune)
		}
	}
	return string(out)
}

// TestTUIMenuHoverHighlight pins the pointer hover highlight: the
// hovered title and the hovered dropdown item render reversed, other
// entries stay plain, no pointer position highlights nothing, and the
// motion path feeds the highlight through buildRoot. Hover runs no
// action. See TheoryOfControlBar.
func TestTUIMenuHoverHighlight(t *testing.T) {
	lastFrame := func(e taiui.Element) *taiui.Frame {
		screen := &menuHoverCaptureScreen{w: 80, h: 24}
		taiui.Render(e, screen)
		return &screen.frames[len(screen.frames)-1]
	}

	t.Run("Title", func(t *testing.T) {
		if got := menuHoverReversedRunes(lastFrame(menuBarElement(80, -1, 0)), 0); got != "Sections" {
			t.Fatalf("the hovered title must render reversed, got %q", got)
		}
		if got := menuHoverReversedRunes(lastFrame(menuBarElement(80, -1, -1)), 0); got != "" {
			t.Fatalf("no hover must highlight nothing, got %q", got)
		}
	})

	t.Run("Item", func(t *testing.T) {
		frame := lastFrame(menuDropdownElement(80, 24, 0, 1))
		if got := menuHoverReversedRunes(frame, 2); got != "Next section" {
			t.Fatalf("the hovered item must render reversed, got %q", got)
		}
		if got := menuHoverReversedRunes(frame, 1); got != "" {
			t.Fatalf("the unhovered item must stay plain, got %q", got)
		}
	})

	t.Run("PointerState", func(t *testing.T) {
		tui := &TUI{width: 80, height: 24}
		tui.tabs = taiui.NewTabs(3)
		tui.tabs.TopInset = 1
		if tui.menuHoverTitleLocked() != -1 {
			t.Fatal("no pointer position must highlight nothing")
		}
		tui.ctlHover, tui.ctlHoverX, tui.ctlHoverY = true, 2, 0
		if tui.menuHoverTitleLocked() != 0 {
			t.Fatal("the pointer on the Sections title must hover it")
		}
		tui.ctlHoverX = 12
		if tui.menuHoverTitleLocked() != 1 {
			t.Fatal("the pointer on the View title must hover it")
		}
		tui.ctlHoverX, tui.ctlHoverY = 5, 10
		if tui.menuHoverTitleLocked() != -1 {
			t.Fatal("the pointer off the menu bar must highlight nothing")
		}
		tui.openMenu = 0
		tui.ctlHoverX, tui.ctlHoverY = 4, 1
		if tui.menuHoverItemLocked() != 0 {
			t.Fatal("the pointer on the first item row must hover it")
		}
		tui.ctlHoverX = 0
		if tui.menuHoverItemLocked() != -1 {
			t.Fatal("the pointer on the side padding must highlight no item")
		}
		tui.ctlHoverX, tui.ctlHoverY = 4, 4
		if tui.menuHoverItemLocked() != -1 {
			t.Fatal("the pointer below the items must highlight no item")
		}
	})

	t.Run("MotionPath", func(t *testing.T) {
		tui := newTUIForTest()
		tui.width, tui.height = 80, 24
		tui.tabs.TopInset = 1
		tui.handleMouseKey("mouse-motion@27,0")
		if tui.menuHoverTitleLocked() != 3 {
			t.Fatal("motion over the Quit title must hover it")
		}
		screen := &menuHoverCaptureScreen{w: 80, h: 24}
		taiui.Render(buildRoot(tui, 80, 24, [3][]taiui.Line{}), screen)
		if got := menuHoverReversedRunes(&screen.frames[len(screen.frames)-1], 0); got != "Quit" {
			t.Fatalf("buildRoot must highlight the hovered title, got %q", got)
		}
	})
}
