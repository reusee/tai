package main

import (
	"testing"

	"github.com/reusee/tai/taiui"
)

// TestTUIChatInputBarRaisedOnDemand pins the on-demand bar: a session
// that did not declare itself interactive — the Go module command's goal
// run — raises the bar when its first ChatInput call arrives, because
// the post-goal discussion reads the user's lines through it; the pane
// gives up the bar's row and a click on the row focuses the bar. A
// session whose ChatInput never runs keeps the bar hidden and the pane
// at full height. See TheoryOfTUIChatInput and
// pipeline.TheoryOfGoalDiscussion.
func TestTUIChatInputBarRaisedOnDemand(t *testing.T) {
	tui := newTUIForTest()
	tui.interactive = false
	tui.tabs.Expanded = []bool{false, true, false}
	tui.tabs.HasContent = []bool{false, true, false}
	tui.tabs.Focus = 1
	tui.width, tui.height = 40, 10

	barCell := func() (taiui.FrameCell, bool) {
		screen := &panelTestScreen{width: 40, height: 10}
		taiui.Render(buildRoot(tui, 40, 10, nil), screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		frame := screen.frames[len(screen.frames)-1]
		for _, cell := range frame.Cells {
			if cell.Set && cell.Rune == '>' {
				return cell, true
			}
		}
		return taiui.FrameCell{}, false
	}

	// The collapsed Tree strip holds row 0 and the collapsed Logs strip
	// row 9, so the Output tab occupies rows 1..8 and a raised bar sits
	// on row 8. See TheoryOfTUI.
	box := tui.tabs.Boxes(40, 10)[1]
	if _, ok := barCell(); ok {
		t.Fatal("a session without a waiting chat input must hide the bar")
	}
	if got := tui.tuiPaneHeight(1, box); got != taiui.PaneHeight(box) {
		t.Fatalf("a hidden bar must leave the pane the full height %d, got %d", taiui.PaneHeight(box), got)
	}

	done := make(chan error, 1)
	go func() {
		_, err := tui.ChatInput(">> ")
		done <- err
	}()
	waitChatInputWaiting(t, tui)

	if _, ok := barCell(); !ok {
		t.Fatal("the first ChatInput call must raise the bar")
	}
	if got := tui.tuiPaneHeight(1, box); got != taiui.PaneHeight(box)-1 {
		t.Fatalf("a raised bar must reserve its row (%d), got %d", taiui.PaneHeight(box)-1, got)
	}
	tui.handleMouseKey("mouse-left@5,8")
	tui.mu.Lock()
	focused := tui.inputFocused
	tui.mu.Unlock()
	if !focused {
		t.Fatal("a press on the raised bar's row must focus it")
	}

	tui.cancelChatInput()
	<-done
}
