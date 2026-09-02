package main

import (
	"fmt"

	"github.com/gdamore/tcell/v3/color"
	"github.com/reusee/tai/taiui"
)

const TheoryOfControlBar = `
Control bar and mouse-complete interaction theory (cmd/tai):

- The top screen row is a control bar. Every keyboard action that had
  no pointer path — section jumps, collapse all, split toggle,
  mouse-reporting toggle, help, and quit — renders there as one
  colored glyph per slot. With it, all TUI interactions are reachable
  by mouse alone. Event markers keep their emoji; UI glyphs are plain
  colorable characters, so the glyphs carry no Emoji property.
- The bar exists only while Tabs.TopInset reserves a row: the tab
  layout starts below the inset, the bar draws over the reserved row,
  and no coordinate is remapped. Every Boxes consumer, hit test, and
  panel keeps its coordinates.
- Each glyph owns a fixed two-cell slot. The hit test maps the press
  column onto the slot, not onto the glyph's own width, so
  width-ambiguous runes stay clickable. The layout is a pure function
  of the width and the mouse state; the renderer and the hit test
  share it, so they cannot disagree.
- Quit keeps the two-press protocol: the first ✕ press arms the
  confirmation bar, the second confirms, and any other press — bar
  glyph or pane — cancels.
- A press inside the help overlay closes it; the overlay's box comes
  from taiui.HelpOverlayBox.
- The chat input bar carries a submit glyph ↵ at its right end. A
  press there delivers the typed line by the Enter rule: only while a
  ChatInput call waits, the line kept otherwise. No focus needed. The
  glyph colors by that waiting state.
`

// controlBarAction is one control bar action. The values are the
// semantic key names the key dispatch already uses, so a click and a
// keystroke share one action vocabulary.
type controlBarAction string

const (
	controlPrevSections controlBarAction = "prev-transition"
	controlNextSections controlBarAction = "next-transition"
	controlCollapseAll  controlBarAction = "collapse-all"
	controlSplitToggle  controlBarAction = "split"
	controlMouseToggle  controlBarAction = "mouse"
	controlHelpToggle   controlBarAction = "help"
	controlQuit         controlBarAction = "quit"
)

// controlBarSlotWidth is one slot's cell count: a two-cell glyph plus
// one separator cell. See TheoryOfControlBar.
const controlBarSlotWidth = 3

// controlGlyph is one control bar slot: the action, its glyph and
// color, and the cell column range [x0, x1).
type controlGlyph struct {
	action controlBarAction
	glyph  string
	color  taiui.Color
	x0     int
	x1     int
}

// controlBarLayout lays the glyphs across the top row. It is pure:
// the renderer draws from it and the hit test maps presses through
// it, so the two cannot disagree. See TheoryOfControlBar.
func controlBarLayout(width int, mouseOn bool) []controlGlyph {
	var out []controlGlyph
	x := 0
	add := func(action controlBarAction, glyph string, c taiui.Color) {
		if x+2 > width {
			return
		}
		out = append(out, controlGlyph{action: action, glyph: glyph, color: c, x0: x, x1: x + 2})
		x += controlBarSlotWidth
	}
	add(controlPrevSections, "«", outputColorSystemLine)
	add(controlNextSections, "»", outputColorSystemLine)
	add(controlCollapseAll, "⊟", outputColorToolLine)
	add(controlSplitToggle, "⇅", outputColorThoughtLine)
	if mouseOn {
		add(controlMouseToggle, "◉", color.PaletteColor(10))
	} else {
		add(controlMouseToggle, "◎", color.PaletteColor(8))
	}
	add(controlHelpToggle, "?", outputColorSystemLine)
	add(controlQuit, "✕", outputColorLogLine)
	return out
}

// controlBarHit maps a press onto the bar's action: the bar occupies
// the top row while Tabs.TopInset reserves it. See TheoryOfControlBar.
func controlBarHit(topInset, width, x, y int, mouseOn bool) (controlBarAction, bool) {
	if topInset <= 0 || y != 0 {
		return "", false
	}
	for _, g := range controlBarLayout(width, mouseOn) {
		if x >= g.x0 && x < g.x1 {
			return g.action, true
		}
	}
	return "", false
}

// controlBarElement renders the top-row bar: one colored glyph per
// slot over a filled row. See TheoryOfControlBar.
func controlBarElement(width int, mouseOn bool) taiui.Element {
	children := []any{taiui.Rect(
		taiui.Box{Top: 0, Left: 0, Bottom: 1, Right: max(width, 1)},
		taiui.Fill(true),
		taiui.BGColor(taiui.HexColor(tabUnfocusBG)),
	)}
	for _, g := range controlBarLayout(width, mouseOn) {
		children = append(children, taiui.Text(g.glyph,
			taiui.Box{Top: 0, Left: g.x0, Bottom: 1, Right: g.x1},
			taiui.FGColor(g.color),
			taiui.Bold(true),
		))
	}
	return taiui.Overlay(children...)
}

// submitGlyph returns the input bar's submit glyph and its color: the
// glyph brightens while a ChatInput call waits. ↵ carries no Emoji
// property, so every terminal renders it as a colorable character.
// See TheoryOfControlBar.
func submitGlyph(waiting bool) (string, taiui.Color) {
	if waiting {
		return "↵", color.PaletteColor(10)
	}
	return "↵", color.PaletteColor(8)
}

// dispatchControlBar runs one bar action. It runs without t.mu held:
// the actions take the lock themselves. The bool reports a confirmed
// quit. See TheoryOfControlBar.
func (t *TUI) dispatchControlBar(action controlBarAction) bool {
	switch action {
	case controlPrevSections:
		t.jumpToTransition(-1)
	case controlNextSections:
		t.jumpToTransition(1)
	case controlCollapseAll:
		t.collapseAllSections()
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
// releases a waiting chat input and restores the cursor row. It is
// the exit path of both the quit key and the ✕ glyph. See TheoryOfTUI
// and TheoryOfControlBar.
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
// it. The caller holds t.mu. See TheoryOfControlBar.
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
// caller holds t.mu. See TheoryOfControlBar.
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
// t.mu. See TheoryOfTUIChatInput and TheoryOfControlBar.
func (t *TUI) submitInputLocked() {
	if t.inputResult != nil {
		t.deliverInputLocked(chatInputResult{line: t.inputBar.Line(), ok: true})
	}
}
