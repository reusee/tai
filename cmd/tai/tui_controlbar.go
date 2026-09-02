package main

import (
	"fmt"

	"github.com/gdamore/tcell/v3/color"
	"github.com/reusee/tai/taiui"
)

const TheoryOfControlBar = `
Menu bar and mouse-complete interaction theory (cmd/tai):

- The top screen row is a text menu bar in the style of a desktop
  menu bar: the Sections, View, and Help categories group the
  keyboard actions that had no pointer path, and Quit is a top-level
  entry. A press on a category title opens its dropdown, pressing it
  again closes it, a press on another title switches menus, an item
  press runs the action and closes, and a press anywhere else closes
  the menu and runs the ordinary press handling. With the menus, all
  TUI interactions are reachable by mouse alone.
- The menu bar exists only while Tabs.TopInset reserves a row: the
  tab layout starts below the inset, the bar draws over the reserved
  row, and no coordinate is remapped. Every Boxes consumer, hit test,
  and panel keeps its coordinates. The row carries only the category
  titles, with no background of its own, so it keeps the terminal
  default background.
- The layout is a pure function of the width and the open menu: the
  renderer and the hit tests share menuBarLayout and menuDropdownBox,
  so they cannot disagree. Titles and labels are ASCII constants, so
  their byte length is their cell width.
- The dropdown is a bordered Rect titled with the category name,
  drawn over the tabs; the element tree places it after the panels
  and the help overlay, so an open menu covers both.
- Quit keeps the two-press protocol: the first Quit press arms the
  confirmation bar, the second confirms, and any other press — menu
  entry or pane — cancels.
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

// menuItem is one entry of a dropdown menu: its display text and the
// control bar action dispatchControlBar runs when it is clicked.
type menuItem struct {
	label  string
	action controlBarAction
}

// menuEntry is one entry of the menu bar: a dropdown menu carries
// items, a top-level entry carries only its action.
type menuEntry struct {
	title  string
	action controlBarAction
	items  []menuItem
}

// isTopLevel reports whether the entry runs its action directly
// instead of opening a dropdown.
func (e menuEntry) isTopLevel() bool {
	return e.action != ""
}

// menuBarEntries defines the menu bar, left to right: the Sections and
// View categories group related actions into dropdowns, Help is a
// single-item menu, and Quit is a top-level entry. Titles and labels
// are ASCII constants, so their byte length is their cell width. See
// TheoryOfControlBar.
var menuBarEntries = []menuEntry{
	{title: "Sections", items: []menuItem{
		{label: "Prev section", action: controlPrevSections},
		{label: "Next section", action: controlNextSections},
		{label: "Collapse all", action: controlCollapseAll},
	}},
	{title: "View", items: []menuItem{
		{label: "Toggle split", action: controlSplitToggle},
		{label: "Toggle mouse", action: controlMouseToggle},
	}},
	{title: "Help", items: []menuItem{
		{label: "Keyboard help", action: controlHelpToggle},
	}},
	{title: "Quit", action: controlQuit},
}

// menuDropdownPadding is the blank cells between an item label and the
// dropdown border on each side.
const menuDropdownPadding = 2

// menuBarGap is the blank cells between two menu bar titles.
const menuBarGap = 3

// menuBarSlot is one menu bar slot: the menuBarEntries index and the
// title's cell column range [x0, x1).
type menuBarSlot struct {
	index  int
	x0, x1 int
}

// menuBarLayout lays the menu titles across the top row: each title
// occupies its own text width and a gap separates the titles. It is
// pure: the renderer draws from it and the hit test maps presses
// through it, so the two cannot disagree. Titles past the width are
// dropped. See TheoryOfControlBar.
func menuBarLayout(width int) []menuBarSlot {
	var out []menuBarSlot
	x := 0
	for i, entry := range menuBarEntries {
		end := x + len(entry.title)
		if end > width {
			break
		}
		out = append(out, menuBarSlot{index: i, x0: x, x1: end})
		x = end + menuBarGap
	}
	return out
}

// menuBarHit maps a press on the menu bar row onto the entry index it
// hits. The bar occupies the row Tabs.TopInset reserves; a press in
// the gaps between titles hits nothing. See TheoryOfControlBar.
func menuBarHit(topInset, width, x, y int) (index int, ok bool) {
	if topInset <= 0 || y != 0 {
		return -1, false
	}
	for _, slot := range menuBarLayout(width) {
		if x >= slot.x0 && x < slot.x1 {
			return slot.index, true
		}
	}
	return -1, false
}

// menuBarElement renders the menu bar row: the category titles as
// plain text on the row Tabs.TopInset reserves, with no background of
// its own so the row keeps the terminal default. The open menu's
// title is bold. See TheoryOfControlBar.
func menuBarElement(width int, openMenu int) taiui.Element {
	var children []any
	for _, slot := range menuBarLayout(width) {
		box := taiui.Box{Top: 0, Left: slot.x0, Bottom: 1, Right: slot.x1}
		title := menuBarEntries[slot.index].title
		if slot.index == openMenu {
			children = append(children, taiui.Text(title, box, taiui.Bold(true)))
		} else {
			children = append(children, taiui.Text(title, box))
		}
	}
	return taiui.Overlay(children...)
}

// menuDropdownBox returns the box of the open menu's dropdown: a
// bordered box of item rows under the menu's title slot, shifted left
// and clamped so it stays on the screen. The renderer and the item hit
// test share it, so they cannot disagree. See TheoryOfControlBar.
func menuDropdownBox(width, height, openMenu int) (taiui.Box, bool) {
	if openMenu < 0 || openMenu >= len(menuBarEntries) {
		return taiui.Box{}, false
	}
	var slot menuBarSlot
	found := false
	for _, s := range menuBarLayout(width) {
		if s.index == openMenu {
			slot = s
			found = true
		}
	}
	if !found {
		return taiui.Box{}, false
	}
	entry := menuBarEntries[openMenu]
	itemWidth := 0
	for _, item := range entry.items {
		itemWidth = max(itemWidth, len(item.label))
	}
	w := itemWidth + menuDropdownPadding*2 + 2 // item padding plus the border
	left := min(slot.x0, max(width-w, 0))
	h := len(entry.items) + 2 // item rows plus the border
	box := taiui.Box{
		Top:    1,
		Left:   left,
		Bottom: min(1+h, max(height, 1)),
		Right:  min(left+w, max(width, 1)),
	}
	if box.Width() <= 0 || box.Height() <= 0 {
		return taiui.Box{}, false
	}
	return box, true
}

// menuDropdownElement renders the open menu's dropdown over the tabs:
// a bordered box titled with the category name, one item per row. It
// returns nil when no menu is open or the box degenerates. See
// TheoryOfControlBar.
func menuDropdownElement(width, height, openMenu int) taiui.Element {
	box, ok := menuDropdownBox(width, height, openMenu)
	if !ok {
		return nil
	}
	entry := menuBarEntries[openMenu]
	children := []any{taiui.Rect(
		box,
		taiui.Fill(true),
		taiui.BGColor(taiui.HexColor(tabUnfocusBG)),
		taiui.Border(true),
		taiui.Title(entry.title),
	)}
	for i, item := range entry.items {
		children = append(children, taiui.Text(item.label,
			taiui.Box{Top: box.Top + 1 + i, Left: box.Left + 1, Bottom: box.Top + 2 + i, Right: box.Right - 1},
		))
	}
	return taiui.Overlay(children...)
}

const (
	controlPrevSections controlBarAction = "prev-transition"
	controlNextSections controlBarAction = "next-transition"
	controlCollapseAll  controlBarAction = "collapse-all"
	controlSplitToggle  controlBarAction = "split"
	controlMouseToggle  controlBarAction = "mouse"
	controlHelpToggle   controlBarAction = "help"
	controlQuit         controlBarAction = "quit"
)

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

// menuDropdownPressLocked handles a left press while a menu is open: a
// press on an item reports its action, a press inside the dropdown but
// off the items reports none, and a press outside the dropdown is not
// consumed. A consumed press closes the menu. The caller holds t.mu.
// See TheoryOfControlBar.
func (t *TUI) menuDropdownPressLocked(x, y int) (action controlBarAction, consumed bool) {
	open := t.openMenu
	if open < 0 || open >= len(menuBarEntries) {
		return "", false
	}
	box, ok := menuDropdownBox(t.width, t.height, open)
	if !ok || x < box.Left || x >= box.Right || y < box.Top || y >= box.Bottom {
		return "", false
	}
	t.openMenu = -1
	consumed = true
	row := y - (box.Top + 1)
	if row >= 0 && row < len(menuBarEntries[open].items) &&
		x >= box.Left+1 && x < box.Right-1 {
		action = menuBarEntries[open].items[row].action
	}
	return action, consumed
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
