package main

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/apps"
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
		// No menu is open when a test starts: the zero index would be a
		// valid open menu. See TheoryOfControlBar.
		openMenu: -1,
		// The input-bar tests exercise interactive sessions, the only
		// ones that render the bar. See TheoryOfTUIChatInput.
		interactive: true,
	}
}

// waitChatInputWaiting polls until a ChatInput call is waiting on the
// bar (inputResult registered), bounding the wait so a failed
// activation fails the test instead of hanging. A waiting call no
// longer takes the keyboard — focus is click-driven — so tests that
// need typing click the input row themselves. See TheoryOfTUIChatInput.
func waitChatInputWaiting(t *testing.T, tu *TUI) {
	t.Helper()
	for i := 0; i < 1000; i++ {
		tu.mu.Lock()
		waiting := tu.inputResult != nil
		tu.mu.Unlock()
		if waiting {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("chat input did not activate")
}

func TestTUIChatInputSubmitsTypedLine(t *testing.T) {
	tu := newChatInputTestTUI()
	tu.width, tu.height = 80, 24
	tu.tabs.FocusTab(0)
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
	// Focus is click-driven: click the bar's row before typing.
	tu.handleMouseKey("mouse-left@5,21")

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
	tu.width, tu.height = 80, 24
	tu.tabs.FocusTab(0)
	done := make(chan error, 1)
	go func() {
		_, err := tu.ChatInput(">> ")
		done <- err
	}()
	waitChatInputWaiting(t, tu)
	// Focus is click-driven: click the bar's row so Ctrl-C has a focus
	// to release.
	tu.handleMouseKey("mouse-left@5,21")
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
// while the bar is focused, number keys type into the line; a
// navigation key that changes other elements (an arrow that scrolls)
// still drives the TUI and releases the input focus — the cursor hides
// and only a click regains it; Esc releases the keyboard back to
// navigation without cancelling the waiting ChatInput. See
// TheoryOfTUIChatInput.
func TestTUIChatInputTypingKeepsNavigation(t *testing.T) {
	tu := newChatInputTestTUI()
	tu.width, tu.height = 80, 24
	// An expanded, focused Output tab with a scrollable view makes the
	// arrow keys observable. The clicks below run while only the Output
	// tab is expanded, so its box spans the full height minus the two
	// collapsed strips and the input row is the fixed row 21; the Logs
	// tab is expanded afterwards to make the tab key's focus cycling
	// observable.
	tu.tabs.FocusTab(0)
	tu.scrolls[0].MaxOffset = 100
	tu.scrolls[0].Offset = 50

	done := make(chan struct{})
	go func() {
		_, _ = tu.ChatInput(">> ")
		close(done)
	}()
	waitChatInputWaiting(t, tu)
	// Focus is click-driven: click the bar's row before typing.
	tu.handleMouseKey("mouse-left@5,21")

	// Number keys type into the line instead of toggling tabs.
	if quit := tu.handleKey("1"); quit {
		t.Fatal("handleKey must not quit while typing")
	}
	tu.mu.Lock()
	line := tu.inputBar.Line()
	tu.mu.Unlock()
	if line != "1" {
		t.Fatalf("key 1 should type into the input line, got %q", line)
	}

	// Navigation keys keep working while typing: an arrow scrolls the
	// focused pane, and because it changes other elements it releases
	// the input focus. See TheoryOfTUIChatInput.
	tu.handleKey("up")
	tu.mu.Lock()
	offset := tu.scrolls[0].Offset
	focused := tu.inputFocused
	tu.mu.Unlock()
	if offset != 49 {
		t.Fatalf("up must scroll the focused pane while typing, got offset %d", offset)
	}
	if focused {
		t.Fatal("a view-changing navigation key must release the input focus")
	}

	// A click regains the focus, and Esc releases it again without
	// cancelling the waiting ChatInput.
	tu.handleMouseKey("mouse-left@5,21")
	tu.handleKey("esc")
	tu.mu.Lock()
	focused = tu.inputFocused
	waiting := tu.inputResult != nil
	tu.mu.Unlock()
	if focused {
		t.Fatal("esc must unfocus the input bar")
	}
	if !waiting {
		t.Fatal("esc must not cancel the waiting ChatInput call")
	}

	// With the focus released, keys drive the TUI: down scrolls back,
	// tab cycles the focus to the expanded Logs tab, and 2 expands the
	// Events tab instead of typing.
	tu.tabs.Expanded[2] = true
	tu.tabs.HasContent[2] = true
	tu.handleKey("down")
	tu.mu.Lock()
	offset = tu.scrolls[0].Offset
	tu.mu.Unlock()
	if offset != 50 {
		t.Fatalf("down must scroll the focused pane, got offset %d", offset)
	}
	tu.handleKey("tab")
	tu.mu.Lock()
	focus := tu.tabs.Focus
	tu.mu.Unlock()
	if focus != 2 {
		t.Fatalf("tab must cycle the tab focus, got %d", focus)
	}
	tu.handleKey("2")
	tu.mu.Lock()
	expanded := tu.tabs.Expanded[1]
	tu.mu.Unlock()
	if !expanded {
		t.Fatal("key 2 must expand the Events tab once the input is unfocused")
	}
	select {
	case <-done:
		t.Fatal("ChatInput must stay blocked after the navigation keys")
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
	line := tu.inputBar.Line()
	waiting := tu.inputResult != nil
	tu.mu.Unlock()
	if waiting {
		t.Fatal("no ChatInput call should be waiting during generation")
	}
	if line != "hi" {
		t.Fatalf("enter without a waiter must keep the line, got %q", line)
	}

	// The model goes idle: the waiting call leaves the click-gained
	// focus in place, the typed line survives, and Enter sends it.
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
	line := tu.inputBar.Line()
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
	line = tu.inputBar.Line()
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

// TestTUIChatInputNavBlurOnViewChange pins the view-change release
// rule: while the bar is focused, a fall-through navigation key that
// changes other elements — a page key that moves the scroll offset —
// releases the input focus, and a key that changes nothing — a page-up
// already at the top — keeps it. See TheoryOfTUIChatInput.
func TestTUIChatInputNavBlurOnViewChange(t *testing.T) {
	tu := newChatInputTestTUI()
	tu.width, tu.height = 80, 24
	tu.tabs.FocusTab(0)
	tu.scrolls[0].MaxOffset = 100
	tu.scrolls[0].Offset = 50
	tu.scrolls[0].Follow = false

	// Click the input row — the bottom row of the expanded Output tab's
	// box; the two collapsed strips leave rows 0..21, so the input row
	// is 21 — to focus the bar.
	tu.handleMouseKey("mouse-left@5,21")

	// A page-up that moves the offset changes other elements and must
	// release the focus.
	tu.handleKey("pageup")
	tu.mu.Lock()
	offset := tu.scrolls[0].Offset
	focused := tu.inputFocused
	tu.mu.Unlock()
	if offset == 50 {
		t.Fatal("pageup must move the scroll offset")
	}
	if focused {
		t.Fatal("a view-changing page key must release the input focus")
	}

	// A page-up already at the top changes nothing and keeps the focus.
	tu.handleMouseKey("mouse-left@5,21")
	tu.mu.Lock()
	tu.scrolls[0].Offset = 0
	tu.scrolls[0].Follow = false
	tu.mu.Unlock()
	tu.handleKey("pageup")
	tu.mu.Lock()
	offset = tu.scrolls[0].Offset
	focused = tu.inputFocused
	tu.mu.Unlock()
	if offset != 0 {
		t.Fatalf("pageup at the top must not move the offset, got %d", offset)
	}
	if !focused {
		t.Fatal("a page key that changes nothing must keep the input focus")
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

// TestTUIHelpLinesNonInteractive pins the non-interactive help overlay:
// it must not describe the input bar that is not rendered. See
// TheoryOfTUIChatInput.
func TestTUIHelpLinesNonInteractive(t *testing.T) {
	tui := newTUIForTest()
	if lines := tui.helpLines(); !slices.Equal(lines, tuiHelpLines) {
		t.Fatal("an interactive session must show the full help lines")
	}
	tui.interactive = false
	lines := tui.helpLines()
	for _, line := range lines {
		if strings.HasPrefix(line, "input bar\t") {
			t.Fatalf("non-interactive help must not describe the input bar, got %q", line)
		}
		if strings.Contains(line, "input row") {
			t.Fatalf("non-interactive help must not describe the input row, got %q", line)
		}
	}
	if !slices.Contains(lines, "enter\ttoggle the latest tree node's expansion") {
		t.Fatal("non-interactive help must document Enter's tree-node binding")
	}
	if !slices.Contains(lines, "tree row\tclick 👉 on an attempt-start line to jump the Output tab to its output section; click a node to expand or collapse it") {
		t.Fatal("help must document the tree-row click jump")
	}
}

// TestTUIChatInputBarBackgroundFollowsTabFocus pins the input bar's
// background: the bar uses the Output tab's focused background while
// the tab holds the focus, and the unfocused tab background — the same
// one the panel above it uses — when another tab is focused. See
// TheoryOfTUIChatInput.
func TestTUIChatInputBarBackgroundFollowsTabFocus(t *testing.T) {
	renderBarCell := func(focus int) taiui.FrameCell {
		tui := newTUIForTest()
		tui.tabs.Expanded = []bool{true, true, false}
		tui.tabs.HasContent = []bool{true, true, false}
		tui.tabs.Focus = focus
		screen := &panelTestScreen{width: 40, height: 10}
		taiui.Render(buildRoot(tui, 40, 10, [3][]taiui.Line{}), screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		frame := screen.frames[len(screen.frames)-1]
		for _, cell := range frame.Cells {
			if cell.Set && cell.Rune == '>' {
				return cell
			}
		}
		t.Fatal("expected the input bar prompt in the frame")
		return taiui.FrameCell{}
	}

	focusedCell := renderBarCell(0)
	wantR, wantG, wantB := panelStyle.FocusBG.RGB()
	if r, g, b := focusedCell.Style.Bg().RGB(); r != wantR || g != wantG || b != wantB {
		t.Fatalf("expected the focused tab background on the input bar, got %#x %#x %#x", r, g, b)
	}

	unfocusedCell := renderBarCell(1)
	wantR, wantG, wantB = panelStyle.BaseBG.RGB()
	if r, g, b := unfocusedCell.Style.Bg().RGB(); r != wantR || g != wantG || b != wantB {
		t.Fatalf("expected the unfocused tab background on the input bar, got %#x %#x %#x", r, g, b)
	}
}

// TestTUINonInteractivePaneAndInput pins the non-interactive pane
// arithmetic and input handling: the Output pane keeps the full height
// (only the label strip is reserved), and a press on the tab's bottom
// row drives ordinary tab interaction instead of focusing the absent
// input bar. See TheoryOfTUIChatInput.
func TestTUINonInteractivePaneAndInput(t *testing.T) {
	tui := newTUIForTest()
	tui.tabs.Expanded = []bool{true, false, false}
	tui.tabs.HasContent = []bool{true, false, false}
	tui.tabs.Focus = 0
	tui.width, tui.height = 80, 24

	box := tui.tabs.Boxes(80, 24)[0]
	tui.interactive = false
	if got := tui.tuiPaneHeight(0, box); got != taiui.PaneHeight(box) {
		t.Fatalf("non-interactive Output pane must keep the full height %d, got %d", taiui.PaneHeight(box), got)
	}
	tui.interactive = true
	if got := tui.tuiPaneHeight(0, box); got != taiui.PaneHeight(box)-1 {
		t.Fatalf("interactive Output pane must reserve the bar row (%d), got %d", taiui.PaneHeight(box)-1, got)
	}

	// The two collapsed strips leave the Output tab rows 0..21, so row
	// 21 is the tab's bottom row — the input bar's row in interactive
	// sessions. In a non-interactive session the press anchors an
	// ordinary drag scroll and never focuses the input.
	tui.interactive = false
	tui.scrolls[0].MaxOffset = 100
	tui.scrolls[0].Offset = 10
	tui.handleMouseKey("mouse-left@5,21")
	tui.mu.Lock()
	focused := tui.inputFocused
	tui.mu.Unlock()
	if focused {
		t.Fatal("a press on the bottom row must not focus the absent input bar")
	}
	tui.handleMouseKey("mouse-leftdrag@5,15")
	if tui.scrolls[0].Offset != 16 {
		t.Fatalf("expected the bottom-row press to anchor an ordinary drag scroll to offset 16, got %d", tui.scrolls[0].Offset)
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

// TestTUINonInteractiveHidesInputBar pins the non-interactive layout:
// without the bar the Output pane keeps its full height, the tab's
// bottom row shows the control column at the left edge and the scroll
// content beside it, and an interactive session shows the input prompt
// there instead. See TheoryOfTUIChatInput and TheoryOfOutputControls.
func TestTUINonInteractiveHidesInputBar(t *testing.T) {
	tui := newTUIForTest()
	tui.tabs.Expanded = []bool{true, false, false}
	tui.tabs.HasContent = []bool{true, false, false}
	tui.tabs.Focus = 0
	tui.width, tui.height = 40, 10

	display := make([]taiui.Line, 10)
	for i := range display {
		display[i] = taiui.Line{Text: fmt.Sprintf("%d", i)}
	}
	// The bottom row is read in two columns: column 0 is the control
	// column, column 2 the panel's first content column. See
	// TheoryOfOutputControls.
	renderBottomRow := func(interactive bool) [2]taiui.FrameCell {
		tui.interactive = interactive
		// Ten display lines: the non-interactive pane (7 rows) shows
		// lines 3..9 and the interactive pane (6 rows) lines 4..9, so
		// the pane's bottom row holds line 9 in both layouts.
		offset := 4
		if !interactive {
			offset = 3
		}
		tui.scrolls[0] = taiui.ScrollState{Offset: offset}
		screen := &panelTestScreen{width: 40, height: 10}
		taiui.Render(buildRoot(tui, 40, 10, [3][]taiui.Line{display, nil, nil}), screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		frame := screen.frames[len(screen.frames)-1]
		// The two collapsed strips leave the Output tab rows 0..7, so
		// row 7 is the tab's bottom row.
		return [2]taiui.FrameCell{
			frame.Cells[7*frame.Width+0],
			frame.Cells[7*frame.Width+controlColumnWidth],
		}
	}

	// The interactive bottom row carries the input bar prompt: the bar
	// spans the tab's full width and draws over the control column.
	interactiveCells := renderBottomRow(true)
	if !interactiveCells[0].Set || interactiveCells[0].Rune != '>' {
		t.Fatalf("expected the input bar prompt at the interactive bottom row, got %+v", interactiveCells[0])
	}

	// The non-interactive bottom row shows the control column
	// background at the left edge and the scroll content beside it.
	nonInteractiveCells := renderBottomRow(false)
	if !nonInteractiveCells[0].Set || nonInteractiveCells[0].Rune != ' ' {
		t.Fatalf("expected the control column background at the non-interactive bottom row, got %+v", nonInteractiveCells[0])
	}
	if !nonInteractiveCells[1].Set || nonInteractiveCells[1].Rune != '9' {
		t.Fatalf("expected scroll content at the non-interactive bottom row, got %+v", nonInteractiveCells[1])
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

// TestCommandInteractiveFlags pins the interactivity declaration: only
// the apps that call pipeline.ChatInput while running — the ai app's
// idle handler and the next app's chat phase — render the chat input
// bar in TUI mode; every other app hides it. Interactive apps fork
// apps.Interactive(true) into their Defs, and the test reads the value
// from each app's scope. See TheoryOfTUIChatInput and apps.TheoryOfApps.
func TestCommandInteractiveFlags(t *testing.T) {
	// The base must satisfy every app's Defs: Fork analyzes each
	// definition's dependencies, so a provider def such as the ai app's
	// AISystemPrompt fork fails on a base lacking its dependencies. The
	// full cmd/tai Module provides them all, matching main().
	base := dscope.New(dscope.Methods(new(Module))...)
	for name, app := range map[string]apps.App{
		"ai":   AICommand,
		"next": NextCommand,
	} {
		if !bool(app.Scope(base).Get[apps.Interactive]()) {
			t.Fatalf("command %s must declare itself interactive", name)
		}
	}
	for name, app := range map[string]apps.App{
		"default (go module)": GoModuleCommand,
		"default (any text)":  AnyTextCommand,
		"patch":               PatchCommand,
		"ping":                PingCommand,
		"record":              RecordCommand,
	} {
		if bool(app.Scope(base).Get[apps.Interactive]()) {
			t.Fatalf("command %s must not declare itself interactive", name)
		}
	}
}
