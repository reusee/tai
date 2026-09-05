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
  the menu and runs the ordinary press handling. While a dropdown is
  open, pointer motion over another category title pops up that
  title's menu, like a desktop menu bar; motion elsewhere keeps the
  open menu, the top-level quit entry is never triggered by hover,
  and closing stays press-driven. With the menus, all TUI
  interactions are reachable by mouse alone.
- Pointer hover is affordance only: the category title and the open
  dropdown's item under the pointer render reversed, so the pointer
  target is visible before any press. The highlight runs no action
  and opens no menu by itself; the motion-driven menu switch while a
  dropdown is open, the press-driven closing, and the quit entry's
  hover inertness are unchanged.
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
- The dropdown is a borderless, filled box holding exactly the item
  rows: one cell of side padding, no vertical padding, no border and
  no title, drawn over the tabs; the element tree places it after the
  panels and the help overlay, so an open menu covers both.
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

var menuBarEntries = []menuEntry{
	{title: "Sections", items: []menuItem{
		{label: "Prev section", action: controlPrevSections},
		{label: "Next section", action: controlNextSections},
		{label: "Collapse all", action: controlCollapseAll},
	}},
	{title: "View", items: []menuItem{
		{label: "Toggle split", action: controlSplitToggle},
		{label: "Toggle mouse", action: controlMouseToggle},
		{label: "Tree view: all", action: controlTreeViewAll},
		{label: "Tree view: events", action: controlTreeViewEvents},
		{label: "Tree view: summary", action: controlTreeViewSummary},
		{label: "Tree view: model", action: controlTreeViewModel},
		{label: "Tree view: program", action: controlTreeViewProgram},
		{label: "Tree view: user", action: controlTreeViewUser},
	}},
	{title: "Help", items: []menuItem{
		{label: "Keyboard help", action: controlHelpToggle},
	}},
	{title: "Quit", action: controlQuit},
}

// menuDropdownPadding is the blank cells between an item label and the
// dropdown edge on each side. There is no vertical padding: the box
// holds exactly the item rows.
const menuDropdownPadding = 1

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
// title is bold; the title under the pointer renders reversed. Hover
// is affordance only and never triggers actions. See
// TheoryOfControlBar.
func menuBarElement(width int, openMenu int, hoverMenu int) taiui.Element {
	var children []any
	for _, slot := range menuBarLayout(width) {
		box := taiui.Box{Top: 0, Left: slot.x0, Bottom: 1, Right: slot.x1}
		title := menuBarEntries[slot.index].title
		specs := []any{title, box}
		if slot.index == openMenu {
			specs = append(specs, taiui.Bold(true))
		}
		if slot.index == hoverMenu {
			specs = append(specs, taiui.Reverse(true))
		}
		children = append(children, taiui.Text(specs...))
	}
	return taiui.Overlay(children...)
}

// menuDropdownBox returns the box of the open menu's dropdown: the
// item rows directly under the menu's title slot, one cell of side
// padding and no vertical padding, shifted left and clamped so it
// stays on the screen. The renderer and the item hit test share it,
// so they cannot disagree. See TheoryOfControlBar.
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
	w := itemWidth + menuDropdownPadding*2
	left := min(slot.x0, max(width-w, 0))
	h := len(entry.items)
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
// a borderless box holding exactly the item rows, one per row, with
// one cell of side padding; the fill paints the panel background (the
// terminal default when unconfigured), erasing the tabs under it. The
// item under the pointer renders reversed. It returns nil when no
// menu is open or the box degenerates. See TheoryOfControlBar.
func menuDropdownElement(width, height, openMenu, hoverItem int) taiui.Element {
	box, ok := menuDropdownBox(width, height, openMenu)
	if !ok {
		return nil
	}
	entry := menuBarEntries[openMenu]
	children := []any{taiui.Rect(
		box,
		taiui.Fill(true),
		taiui.BGColor(panelStyle.BaseBG),
	)}
	for i, item := range entry.items {
		specs := []any{item.label,
			taiui.Box{Top: box.Top + i, Left: box.Left + menuDropdownPadding, Bottom: box.Top + 1 + i, Right: box.Right - menuDropdownPadding},
		}
		if i == hoverItem {
			specs = append(specs, taiui.Reverse(true))
		}
		children = append(children, taiui.Text(specs...))
	}
	return taiui.Overlay(children...)
}

// The Tree tab's projection actions: one View menu item per
// projection, so the pointer selects the Tree tab's view the way the
// v key cycles it. See TheoryOfTreeTab.
const (
	controlTreeViewAll     controlBarAction = "tree-view-all"
	controlTreeViewEvents  controlBarAction = "tree-view-events"
	controlTreeViewSummary controlBarAction = "tree-view-summary"
	controlTreeViewModel   controlBarAction = "tree-view-model"
	controlTreeViewProgram controlBarAction = "tree-view-program"
	controlTreeViewUser    controlBarAction = "tree-view-user"
)

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
	case controlTreeViewAll:
		t.setTreeView(treeViewAll)
	case controlTreeViewEvents:
		t.setTreeView(treeViewEvents)
	case controlTreeViewSummary:
		t.setTreeView(treeViewSummary)
	case controlTreeViewModel:
		t.setTreeView(treeViewModel)
	case controlTreeViewProgram:
		t.setTreeView(treeViewProgram)
	case controlTreeViewUser:
		t.setTreeView(treeViewUser)
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
// press on an item reports its action, a press on the dropdown's side
// padding closes the menu without an action, and a press outside the
// dropdown is not consumed. A consumed press closes the menu. The
// caller holds t.mu. See TheoryOfControlBar.
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
	row := y - box.Top
	if row >= 0 && row < len(menuBarEntries[open].items) &&
		x >= box.Left+menuDropdownPadding && x < box.Right-menuDropdownPadding {
		action = menuBarEntries[open].items[row].action
	}
	return action, consumed
}

// menuHoverItemLocked returns the row of the open menu's dropdown
// item the pointer hovers, or -1. The item highlights only where a
// press would run it: inside the item's text columns, not the
// dropdown's side padding. The caller holds t.mu. See
// TheoryOfControlBar.
func (t *TUI) menuHoverItemLocked() int {
	if !t.ctlHover || t.openMenu < 0 || t.openMenu >= len(menuBarEntries) {
		return -1
	}
	box, ok := menuDropdownBox(t.width, t.height, t.openMenu)
	if !ok {
		return -1
	}
	if t.ctlHoverY < box.Top || t.ctlHoverY >= box.Bottom ||
		t.ctlHoverX < box.Left+menuDropdownPadding || t.ctlHoverX >= box.Right-menuDropdownPadding {
		return -1
	}
	row := t.ctlHoverY - box.Top
	if row >= len(menuBarEntries[t.openMenu].items) {
		return -1
	}
	return row
}

// menuHoverTitleLocked returns the menu bar entry the pointer hovers,
// from the pointer position the latest motion or release event
// recorded, or -1. The highlight is affordance only: hover never
// opens menus or runs actions. The caller holds t.mu. See
// TheoryOfControlBar.
func (t *TUI) menuHoverTitleLocked() int {
	if !t.ctlHover {
		return -1
	}
	index, ok := menuBarHit(t.tabs.TopInset, t.width, t.ctlHoverX, t.ctlHoverY)
	if !ok {
		return -1
	}
	return index
}

// menuHoverLocked pops up the menu of the category title under the
// pointer while a menu is open: motion over another category title
// switches the open dropdown to that title, like a desktop menu bar;
// motion elsewhere keeps the open menu, and the top-level quit entry
// is never triggered by hover. The caller holds t.mu. See
// TheoryOfControlBar.
func (t *TUI) menuHoverLocked(x, y int) {
	if t.openMenu < 0 {
		return
	}
	index, ok := menuBarHit(t.tabs.TopInset, t.width, x, y)
	if !ok {
		return
	}
	if entry := menuBarEntries[index]; entry.isTopLevel() || index == t.openMenu {
		return
	}
	t.openMenu = index
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
