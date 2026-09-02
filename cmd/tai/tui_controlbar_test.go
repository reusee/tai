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

// TestMenuBarLayout pins the pure layout: ordered titles, hit-test
// agreement, gap and off-row inertness, narrow-width truncation, and
// the rendered titles. See TheoryOfControlBar.
func TestMenuBarLayout(t *testing.T) {
	slots := menuBarLayout(80)
	want := []string{"Sections", "View", "Help", "Quit"}
	if len(slots) != len(want) {
		t.Fatalf("expected %d slots, got %d", len(want), len(slots))
	}
	for i, slot := range slots {
		if menuBarEntries[slot.index].title != want[i] {
			t.Fatalf("slot %d is %q, want %q", i, menuBarEntries[slot.index].title, want[i])
		}
		if slot.x1-slot.x0 != len(want[i]) {
			t.Fatalf("slot %d width is %d, want %d", i, slot.x1-slot.x0, len(want[i]))
		}
		index, ok := menuBarHit(1, 80, slot.x0, 0)
		if !ok || index != slot.index {
			t.Fatalf("hit at %d resolved %d ok=%v, want %d", slot.x0, index, ok, slot.index)
		}
		index, _ = menuBarHit(1, 80, slot.x1-1, 0)
		if index != slot.index {
			t.Fatalf("hit at %d resolved %d, want %d", slot.x1-1, index, slot.index)
		}
	}
	if _, ok := menuBarHit(1, 80, slots[0].x1, 0); ok {
		t.Fatal("the gap between titles is inert")
	}
	if _, ok := menuBarHit(1, 80, 0, 1); ok {
		t.Fatal("row 1 is not the menu bar")
	}
	if _, ok := menuBarHit(0, 80, 0, 0); ok {
		t.Fatal("no inset means no menu bar")
	}
	if len(menuBarLayout(10)) != 1 {
		t.Fatal("a narrow width keeps only the titles that fit")
	}
	var buf bytes.Buffer
	taiui.Render(menuBarElement(80, 0), taiui.NewTerminalScreen(&buf, 80, 10))
	for _, want := range want {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Fatalf("the menu bar must render %q, got: %q", want, buf.String())
		}
	}
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

// TestTUIMenuBarClicks drives the menu bar through handleMouseKey, the
// path the session's key loop takes: opening, switching, item actions,
// closing, and the two-press quit. See TheoryOfControlBar.
func TestTUIMenuBarClicks(t *testing.T) {
	newBar := func() *TUI {
		tui := newTUIForTest()
		tui.width, tui.height = 80, 24
		tui.tabs.TopInset = 1
		return tui
	}

	t.Run("OpenAndClose", func(t *testing.T) {
		tui := newBar()
		tui.handleMouseKey("mouse-left@2,0")
		if tui.openMenu != 0 {
			t.Fatalf("the Sections title must open its menu, got %d", tui.openMenu)
		}
		tui.handleMouseKey("mouse-left@2,0")
		if tui.openMenu != -1 {
			t.Fatalf("pressing the title again must close the menu, got %d", tui.openMenu)
		}
	})

	t.Run("SwitchMenus", func(t *testing.T) {
		tui := newBar()
		tui.handleMouseKey("mouse-left@2,0")
		tui.handleMouseKey("mouse-left@12,0")
		if tui.openMenu != 1 {
			t.Fatalf("pressing another title must switch menus, got %d", tui.openMenu)
		}
	})

	t.Run("ItemRunsAction", func(t *testing.T) {
		tui := newBar()
		tui.handleMouseKey("mouse-left@12,0")
		tui.handleMouseKey("mouse-left@15,2")
		if !tui.tabs.SplitVertical {
			t.Fatal("the Toggle split item must toggle the split axis")
		}
		if tui.openMenu != -1 {
			t.Fatal("an item press must close the menu")
		}
	})

	t.Run("CollapseAll", func(t *testing.T) {
		tui := newBar()
		tui.writeOutputPart(generators.RoleUser, outputColorUserLine, false, "a\n")
		tui.writeOutputPart(generators.RoleModel, taiui.NoColor, false, "b\n")
		tui.handleMouseKey("mouse-left@2,0")
		tui.handleMouseKey("mouse-left@15,4")
		for i := range tui.outputSections {
			if !tui.outputSections[i].collapsed {
				t.Fatalf("section %d must be collapsed", i)
			}
		}
	})

	t.Run("QuitConfirmsOnSecondPress", func(t *testing.T) {
		tui := newBar()
		tui.tty = &fakeTtyForTest{}
		if tui.handleMouseKey("mouse-left@27,0") {
			t.Fatal("the first quit press must only arm the confirmation")
		}
		if !tui.quit.Pending() {
			t.Fatal("the first quit press must arm the confirmation")
		}
		if !tui.handleMouseKey("mouse-left@27,0") {
			t.Fatal("the second quit press must confirm the quit")
		}
	})

	t.Run("OutsidePressClosesMenu", func(t *testing.T) {
		tui := newBar()
		tui.handleMouseKey("mouse-left@2,0")
		tui.handleMouseKey("mouse-left@5,10")
		if tui.openMenu != -1 {
			t.Fatal("a press outside the dropdown must close the menu")
		}
	})

	t.Run("OtherPressCancelsQuit", func(t *testing.T) {
		tui := newBar()
		tui.handleMouseKey("mouse-left@27,0")
		tui.handleMouseKey("mouse-left@5,10")
		if tui.quit.Pending() {
			t.Fatal("a press elsewhere must cancel the confirmation")
		}
	})
}
