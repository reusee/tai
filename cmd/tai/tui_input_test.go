package main

import (
	"io"
	"testing"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/taiui"
)

// newChatInputTestTUI returns a minimally initialized TUI for input-bar
// tests: no tty or session, just the state the input bar and the key
// dispatch read. See TheoryOfTUIChatInput.
func newChatInputTestTUI() *TUI {
	return &TUI{
		tabs:     taiui.NewTabs(3),
		updateCh: make(chan struct{}, 1),
	}
}

// waitChatInputWaiting polls until a ChatInput call is waiting on the
// bar (inputResult registered) and the bar holds the keyboard focus,
// bounding the wait so a failed activation fails the test instead of
// hanging. Waiting on the waiter itself — not just the focus — matters
// because the bar can already be focused (focus persists across a
// submit), and Enter must not run before the channel is registered.
func waitChatInputWaiting(t *testing.T, tu *TUI) {
	t.Helper()
	for i := 0; i < 1000; i++ {
		tu.mu.Lock()
		focused := tu.inputFocused
		waiting := tu.inputResult != nil
		tu.mu.Unlock()
		if focused && waiting {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("chat input did not activate")
}

func TestTUIChatInputSubmitsTypedLine(t *testing.T) {
	tu := newChatInputTestTUI()
	type result struct {
		line string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		line, err := tu.ChatInput(">> ")
		done <- result{line, err}
	}()
	waitChatInputWaiting(t, tu)

	// "he", cursor left, insert "x" → "hxe"; a trailing space is typed
	// and removed again so both keys are exercised.
	for _, key := range []string{"h", "e", "left", "x", "space", "backspace"} {
		if tu.handleKey(key) {
			t.Fatalf("input mode must not quit on key %q", key)
		}
	}
	tu.handleKey("enter")

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatal(res.err)
		}
		if res.line != "hxe" {
			t.Fatalf("got line %q, want %q", res.line, "hxe")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ChatInput did not return after enter")
	}
}

// TestTUIChatInputCtrlCUnfocusesWithoutCancelling pins the non-modal
// release semantics: Ctrl-C only releases the keyboard back to
// navigation; the blocked ChatInput keeps waiting, and io.EOF reaches
// it only through the quit path (cancelChatInput). See
// TheoryOfTUIChatInput.
func TestTUIChatInputCtrlCUnfocusesWithoutCancelling(t *testing.T) {
	tu := newChatInputTestTUI()
	done := make(chan error, 1)
	go func() {
		_, err := tu.ChatInput(">> ")
		done <- err
	}()
	waitChatInputWaiting(t, tu)
	tu.handleKey("ctrl-c")
	tu.mu.Lock()
	focused := tu.inputFocused
	waiting := tu.inputResult != nil
	tu.mu.Unlock()
	if focused {
		t.Fatal("ctrl-c must unfocus the input bar")
	}
	if !waiting {
		t.Fatal("ctrl-c must not cancel the waiting ChatInput call")
	}
	select {
	case err := <-done:
		t.Fatalf("ChatInput must stay blocked after ctrl-c, got %v", err)
	default:
	}
	// The quit path is the only release that returns io.EOF.
	tu.cancelChatInput()
	select {
	case err := <-done:
		if err != io.EOF {
			t.Fatalf("got err %v, want io.EOF", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelChatInput did not release the waiter")
	}
}

// TestTUIChatInputTypingKeepsNavigation pins the non-modal key routing:
// while the bar is focused, number keys type into the line, navigation
// keys (arrows, tab) still drive the TUI, and Esc hands the keyboard
// back to navigation without cancelling the waiting ChatInput. See
// TheoryOfTUIChatInput.
func TestTUIChatInputTypingKeepsNavigation(t *testing.T) {
	tu := newChatInputTestTUI()
	// An expanded, focused Output tab with a scrollable view makes the
	// arrow keys observable, and an expanded Logs tab makes the tab
	// key's focus cycling observable.
	tu.tabs.FocusTab(0)
	tu.tabs.Expanded[2] = true
	tu.tabs.HasContent[2] = true
	tu.scrolls[0].MaxOffset = 100
	tu.scrolls[0].Offset = 50

	done := make(chan struct{})
	go func() {
		_, _ = tu.ChatInput(">> ")
		close(done)
	}()
	waitChatInputWaiting(t, tu)

	// Number keys type into the line instead of toggling tabs.
	if quit := tu.handleKey("1"); quit {
		t.Fatal("handleKey must not quit while typing")
	}
	tu.mu.Lock()
	line := string(tu.inputLine)
	tu.mu.Unlock()
	if line != "1" {
		t.Fatalf("key 1 should type into the input line, got %q", line)
	}

	// Navigation keys keep working while typing: arrows scroll the
	// focused pane.
	tu.handleKey("up")
	tu.mu.Lock()
	offset := tu.scrolls[0].Offset
	tu.mu.Unlock()
	if offset != 49 {
		t.Fatalf("up must scroll the focused pane while typing, got offset %d", offset)
	}
	tu.handleKey("down")
	tu.mu.Lock()
	offset = tu.scrolls[0].Offset
	tu.mu.Unlock()
	if offset != 50 {
		t.Fatalf("down must scroll the focused pane while typing, got offset %d", offset)
	}

	// The tab key cycles the tab focus instead of typing a character.
	tu.handleKey("tab")
	tu.mu.Lock()
	focus := tu.tabs.Focus
	line = string(tu.inputLine)
	tu.mu.Unlock()
	if focus != 2 {
		t.Fatalf("tab must cycle the tab focus while typing, got %d", focus)
	}
	if line != "1" {
		t.Fatalf("tab must not type into the input line, got %q", line)
	}

	// Esc releases the keyboard: number keys drive the TUI again and
	// the waiting ChatInput stays blocked.
	tu.handleKey("esc")
	tu.handleKey("2")
	tu.mu.Lock()
	expanded := tu.tabs.Expanded[1]
	waiting := tu.inputResult != nil
	tu.mu.Unlock()
	if !expanded {
		t.Fatal("key 2 must expand the Events tab after esc unfocused the input")
	}
	if !waiting {
		t.Fatal("esc must not cancel the waiting ChatInput call")
	}
	select {
	case <-done:
		t.Fatal("ChatInput must stay blocked after esc")
	default:
	}
	tu.cancelChatInput()
	<-done
}

// TestTUIChatInputEnterWaitsForIdle pins the generation-window
// semantics: typing works while no ChatInput call waits (the model is
// generating) but Enter is a no-op that keeps the line, and the line is
// submitted with the first Enter after the model goes idle. See
// TheoryOfTUIChatInput.
func TestTUIChatInputEnterWaitsForIdle(t *testing.T) {
	tu := newChatInputTestTUI()
	tu.width, tu.height = 80, 24
	tu.tabs.FocusTab(0)

	// Click the input row — the bottom row of the expanded Output tab's
	// box; the two collapsed strips leave rows 0..21, so the input row
	// is 21 — to focus the bar without a waiting ChatInput, the state
	// during generation.
	tu.handleMouseKey("mouse-left@5,21")
	tu.mu.Lock()
	focused := tu.inputFocused
	tu.mu.Unlock()
	if !focused {
		t.Fatal("a press on the input row must focus the input bar")
	}
	for _, key := range []string{"h", "i"} {
		tu.handleKey(key)
	}
	tu.handleKey("enter")
	tu.mu.Lock()
	line := string(tu.inputLine)
	waiting := tu.inputResult != nil
	tu.mu.Unlock()
	if waiting {
		t.Fatal("no ChatInput call should be waiting during generation")
	}
	if line != "hi" {
		t.Fatalf("enter without a waiter must keep the line, got %q", line)
	}

	// The model goes idle: ChatInput auto-focuses the bar, the typed
	// line survives, and Enter sends it.
	type result struct {
		line string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		line, err := tu.ChatInput(">> ")
		done <- result{line, err}
	}()
	waitChatInputWaiting(t, tu)
	tu.handleKey("enter")
	select {
	case res := <-done:
		if res.err != nil {
			t.Fatal(res.err)
		}
		if res.line != "hi" {
			t.Fatalf("got line %q, want %q", res.line, "hi")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ChatInput did not return after enter")
	}
	// The bar keeps focus after the submit, so typing continues into
	// the next round.
	tu.mu.Lock()
	focused = tu.inputFocused
	tu.mu.Unlock()
	if !focused {
		t.Fatal("the input bar must keep focus after a submit")
	}
}

// TestTUIChatInputMouseFocusAndBlur pins the pointer-driven focus: a
// press on the input row focuses the bar, a press elsewhere releases
// it, and wheel events never change the focus. See TheoryOfTUIChatInput
// and TheoryOfMouseSupport.
func TestTUIChatInputMouseFocusAndBlur(t *testing.T) {
	tu := newChatInputTestTUI()
	tu.width, tu.height = 80, 24
	tu.tabs.FocusTab(0)

	// A press on the input row focuses the bar and typing goes into
	// the line.
	tu.handleMouseKey("mouse-left@5,21")
	tu.handleKey("a")
	tu.mu.Lock()
	focused := tu.inputFocused
	line := string(tu.inputLine)
	tu.mu.Unlock()
	if !focused || line != "a" {
		t.Fatalf("expected a focused bar with line %q, got focused=%v line=%q", "a", focused, line)
	}

	// A press in the pane's scroll area releases the focus, and typed
	// keys no longer edit the line.
	tu.handleMouseKey("mouse-left@5,5")
	tu.handleKey("b")
	tu.mu.Lock()
	focused = tu.inputFocused
	line = string(tu.inputLine)
	tu.mu.Unlock()
	if focused {
		t.Fatal("a press outside the input row must release the input focus")
	}
	if line != "a" {
		t.Fatalf("keys after the blur must not edit the line, got %q", line)
	}

	// Wheel events never change the focus in either direction.
	tu.handleMouseKey("mouse-wheel-up@5,21")
	tu.mu.Lock()
	focused = tu.inputFocused
	tu.mu.Unlock()
	if focused {
		t.Fatal("wheel events must not focus the input bar")
	}
	tu.handleMouseKey("mouse-left@5,21")
	tu.handleMouseKey("mouse-wheel-up@5,5")
	tu.mu.Lock()
	focused = tu.inputFocused
	tu.mu.Unlock()
	if !focused {
		t.Fatal("wheel events must not blur the input bar")
	}
}

// TestTUIChatInputBarBottomRowOfOutputTab pins the layout: the input
// bar is the bottom row inside the Output tab's box — the panel above
// it shrinks by one row — and only a focused bar carries the terminal
// cursor. See TheoryOfTUIChatInput.
func TestTUIChatInputBarBottomRowOfOutputTab(t *testing.T) {
	tui := newTUIForTest()
	tui.tabs.Expanded = []bool{true, false, false}
	tui.tabs.HasContent = []bool{true, false, false}
	tui.tabs.Focus = 0

	renderRoot := func(focused bool) taiui.Frame {
		tui.inputFocused = focused
		screen := &panelTestScreen{width: 40, height: 10}
		taiui.Render(buildRoot(tui, 40, 10, [3][]taiui.Line{}), screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		return screen.frames[len(screen.frames)-1]
	}

	// The two collapsed strips leave the Output tab rows 0..7, so the
	// input bar is the tab's bottom row (row 7) and carries the prompt.
	frame := renderRoot(false)
	cell := frame.Cells[7*frame.Width+0]
	if !cell.Set || cell.Rune != '>' {
		t.Fatalf("expected the input bar prompt at (0,7), got %+v", cell)
	}
	if frame.CursorSet {
		t.Fatal("an unfocused input bar must not carry the terminal cursor")
	}

	// A focused bar carries the terminal cursor on its own row, at the
	// position after the prompt.
	focusedFrame := renderRoot(true)
	if !focusedFrame.CursorSet {
		t.Fatal("a focused input bar must carry the terminal cursor")
	}
	if focusedFrame.CursorY != 7 {
		t.Fatalf("expected the cursor on the input bar row 7, got %d", focusedFrame.CursorY)
	}
	if focusedFrame.CursorX != 3 {
		t.Fatalf("expected the cursor after the %q prompt, got x %d", ">> ", focusedFrame.CursorX)
	}
}

func TestTUIChatInputQuitReleasesWaiter(t *testing.T) {
	tu := newChatInputTestTUI()
	done := make(chan error, 1)
	go func() {
		_, err := tu.ChatInput(">> ")
		done <- err
	}()
	waitChatInputWaiting(t, tu)
	tu.cancelChatInput()
	select {
	case err := <-done:
		if err != io.EOF {
			t.Fatalf("got err %v, want io.EOF", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelChatInput did not release the waiter")
	}
}

// TestForkTUIDisplayRoutesChatInput pins the TUI fork: pipeline.ChatInput
// resolved from the display scope must be the TUI's input bar, not the
// liner default — the liner default would open a second raw-mode reader
// on the tty the TUI already owns, which is the reported tab-freezing
// bug. See TheoryOfTUIChatInput and pipeline.TheoryOfChatInput.
func TestForkTUIDisplayRoutesChatInput(t *testing.T) {
	tui := newChatInputTestTUI()
	scope := forkTUIDisplay(
		dscope.New(modes.ForTest(t), new(pipeline.Module)),
		tui,
	)
	scope.Call(func(chatInput pipeline.ChatInput) {
		go chatInput(">> ")
	})
	waitChatInputWaiting(t, tui)
	tui.cancelChatInput()
}
