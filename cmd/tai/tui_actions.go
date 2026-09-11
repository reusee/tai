package main

import (
	"fmt"

	"github.com/gdamore/tcell/v3/color"
	"github.com/reusee/tai/taiui"
)

const TheoryOfSessionActions = `
Session action and chrome theory (cmd/tai):

- The action vocabulary is shared by the key dispatch and the pointer
  paths: a controlBarAction value is a semantic key name, and
  dispatchControlBar is the one dispatch both call, so a click and a
  keystroke mean the same thing.
- The session-level actions live on the Logs tab's toolbar: the split
  toggle, the mouse-reporting toggle, the help overlay, and quit. The
  per-tab actions live on their own tab's toolbar (see
  TheoryOfToolbars).
- Quit runs the two-press confirmation owned by
  taiui.TheoryOfSessionChrome: the first quit key or quit-button press
  arms it, the second completes it, and every other key or press
  cancels it, so an accidental quit press never loses the session. The
  quit button is therefore resolved before the cancel, unlike every
  other press.
- A press inside the help overlay closes it; the overlay's box comes
  from taiui.HelpOverlayBox.
- The chat input bar carries a submit glyph at its right end. A press
  there delivers the typed line by the Enter rule: only while a
  ChatInput call waits, the line kept otherwise. No focus needed. The
  glyph colors by that waiting state.
- The reusable menu bar mechanism lives in taiui (see
  taiui.TheoryOfMenu). This TUI reserves no screen row for it: every
  action is reachable through the tab toolbars instead.
`

// controlBarAction is one action of the TUI's controls: the values are
// the semantic key names the key dispatch already uses, so a click and
// a keystroke share one action vocabulary. See TheoryOfSessionActions.
type controlBarAction string

// The action vocabulary: one constant per operation a toolbar button
// or a key binding runs, so the key dispatch and the toolbars share
// one set of names. See TheoryOfToolbars and TheoryOfTreeTab.
const (
	controlPrevSections  controlBarAction = "prev-transition"
	controlNextSections  controlBarAction = "next-transition"
	controlCollapseAll   controlBarAction = "collapse-all"
	controlTreeViewCycle controlBarAction = "tree-view-cycle"
	controlCollapseTree  controlBarAction = "collapse-tree"
	controlSplitToggle   controlBarAction = "split"
	controlMouseToggle   controlBarAction = "mouse"
	controlHelpToggle    controlBarAction = "help"
	controlQuit          controlBarAction = "quit"
)

// submitGlyph returns the input bar's submit glyph and its color: the
// glyph brightens while a ChatInput call waits. The glyph carries no
// Emoji property, so every terminal renders it as a colorable
// character. See TheoryOfSessionActions.
func submitGlyph(waiting bool) (string, taiui.Color) {
	if waiting {
		return "↵", color.PaletteColor(10)
	}
	return "↵", color.PaletteColor(8)
}

func (t *TUI) dispatchControlBar(action controlBarAction) bool {
	switch action {
	case controlPrevSections:
		t.jumpToTransition(-1)
	case controlNextSections:
		t.jumpToTransition(1)
	case controlCollapseAll:
		t.collapseAllSections()
	case controlTreeViewCycle:
		t.cycleTreeView()
	case controlCollapseTree:
		t.collapseAllTreeNodes()
	case controlSplitToggle:
		t.toggleSplit()
	case controlMouseToggle:
		t.toggleMouse()
	case controlHelpToggle:
		t.toggleHelp()
	case controlQuit:
		return t.finishQuit()
	}
	return false
}

// finishQuit runs the two-press quit protocol; on confirmation it
// releases a waiting chat input and restores the cursor row. It is the
// exit path of both the quit key and the Logs toolbar's quit button.
// See TheoryOfTUI and TheoryOfSessionActions.
func (t *TUI) finishQuit() bool {
	if !t.handleQuitKey() {
		return false
	}
	// Release any chat input waiting on the input bar so the blocked
	// generation loop can wind down as the session ends. See
	// TheoryOfTUIChatInput.
	t.cancelChatInput()
	t.mu.Lock()
	height := t.height
	t.mu.Unlock()
	fmt.Fprintf(t.tty, "\x1b[%d;1H", height)
	return true
}

// helpPressLocked closes the help overlay when the press lands inside
// it. The caller holds t.mu. See TheoryOfSessionActions.
func (t *TUI) helpPressLocked(x, y int) bool {
	if !t.showHelp {
		return false
	}
	box := taiui.HelpOverlayBox(t.helpLines(), t.width, t.height)
	if x < box.Left || x >= box.Right || y < box.Top || y >= box.Bottom {
		return false
	}
	t.showHelp = false
	return true
}

// submitGlyphHitLocked reports whether the press lands on the input
// bar's submit glyph: the rightmost two cells of the bar's row. The
// caller holds t.mu. See TheoryOfSessionActions.
func (t *TUI) submitGlyphHitLocked(x, y int) bool {
	if !t.interactive || !t.tabs.Expanded[0] {
		return false
	}
	box := t.tabs.Boxes(t.width, t.height)[0]
	if box.Height() <= 1 || box.Width() < 2 {
		return false
	}
	return y == box.Bottom-1 && x >= box.Right-2 && x < box.Right
}

// submitInputLocked delivers the typed line while a ChatInput call
// waits, keeping the line otherwise — the Enter delivery rule,
// reachable through the submit glyph without focus. The caller holds
// t.mu. See TheoryOfTUIChatInput and TheoryOfSessionActions.
func (t *TUI) submitInputLocked() {
	if t.inputResult != nil {
		t.deliverInputLocked(chatInputResult{line: t.inputBar.Line(), ok: true})
	}
}
