package main

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/gdamore/tcell/v3/tty"
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

// TestTUISubmitGlyphClick pins the pointer submit path: a press on the
// bar's right-end glyph delivers the typed line while a ChatInput call
// waits and keeps the line otherwise. See TheoryOfSessionActions.
func TestTUISubmitGlyphClick(t *testing.T) {
	tui := newTUIForTest()
	tui.width, tui.height = 80, 24
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
// TheoryOfSessionActions.
func TestTUIHelpClickCloses(t *testing.T) {
	tui := newTUIForTest()
	tui.width, tui.height = 80, 24
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

// TestTUILogsToolbarQuit pins the Logs toolbar's quit button: the
// rightmost button arms the confirmation on the first press and
// confirms on the second, the same two-press protocol the quit key
// uses. See TheoryOfToolbars.
func TestTUILogsToolbarQuit(t *testing.T) {
	tui := newTUIForTest()
	tui.interactive = false
	tui.width, tui.height = 60, 10
	tui.tabs.Expanded = []bool{false, false, true}
	tui.tabs.HasContent = []bool{false, false, true}
	tui.tabs.Focus = 2
	tui.tty = &fakeTtyForTest{}
	box := tui.tabs.Boxes(60, 10)[2]
	// The buttons lay out right to left; the quit button is the
	// rightmost, occupying [Right-4, Right-2).
	quitX := box.Right - 3
	if tui.handleMouseKey(fmt.Sprintf("mouse-left@%d,%d", quitX, box.Top)) {
		t.Fatal("the first press must only arm the confirmation")
	}
	if !tui.quit.Pending() {
		t.Fatal("the first press on the quit button must arm the confirmation")
	}
	if !tui.handleMouseKey(fmt.Sprintf("mouse-left@%d,%d", quitX, box.Top)) {
		t.Fatal("the second press must confirm the quit")
	}
}
