package taiui

const TheoryOfMenu = `
taiui menu bar theory:
- The menu bar is a reusable desktop-style widget for any taiui
  application: the top screen row carries category titles, each with a
  dropdown, plus optional top-level entries that run an action
  directly. A press on a category title opens its dropdown and arms the
  bar, pressing the open title again closes it and disarms, a press on
  another title switches menus, an item press runs the action and
  closes and disarms, a press on a top-level entry closes and disarms,
  and a press anywhere else closes the menu, disarms, and is not
  consumed. While armed, pointer motion over a category title pops up
  that title's menu, motion over a top-level entry hides the open
  dropdown without disarming, and motion elsewhere keeps it, like a
  desktop menu bar; hiding by hover never disarms, so hovering back to a
  category title re-pops its menu. Top-level entries are never triggered
  by hover, and closing otherwise stays press-driven.
- The bar occupies the row the caller reserves at the top of the screen
  (Tabs.TopInset); the widget never remaps coordinates, so every layout
  consumer, hit test, and panel keeps its coordinates. The row carries
  only the titles, with no background of its own, so it keeps the
  terminal default background.
- Pointer hover is affordance only: the category title and the open
  dropdown's item under the pointer render reversed, so the pointer
  target is visible before any press. The highlight runs no action and
  opens no menu by itself.
- The layout is a pure function of the entries and the width: the
  renderer and the hit tests share Layout and DropdownBox, so they
  cannot disagree. Titles and labels are single-line text.
- The dropdown is a borderless, filled box holding exactly the item
  rows: one cell of side padding, no vertical padding, no border and no
  title.
- Actions are opaque strings: the widget reports the action a press
  resolves to, and the application maps it to its own dispatch, so the
  menu is reusable across applications.
`

// MenuItem is one entry of a dropdown menu: its display text and the
// action the menu bar reports when the item is clicked. The action is
// opaque to the widget. See TheoryOfMenu.
type MenuItem struct {
	Label  string
	Action string
}

// MenuEntry is one entry of the menu bar: a dropdown category carries
// items, a top-level entry carries only its action. See TheoryOfMenu.
type MenuEntry struct {
	Title  string
	Action string
	Items  []MenuItem
}

// IsTopLevel reports whether the entry runs its action directly instead
// of opening a dropdown. See TheoryOfMenu.
func (e MenuEntry) IsTopLevel() bool {
	return e.Action != ""
}

// MenuBarGap is the blank cells between two menu bar titles.
const MenuBarGap = 3

// MenuDropdownPadding is the blank cells between an item label and the
// dropdown edge on each side. There is no vertical padding: the box
// holds exactly the item rows.
const MenuDropdownPadding = 1

// MenuBarSlot is one menu bar slot: the entry index and the title's
// cell column range [X0, X1). See TheoryOfMenu.
type MenuBarSlot struct {
	Index  int
	X0, X1 int
}

// MenuBar is the state of the reusable menu bar widget: the entries,
// the index of the open dropdown, and the armed hover mode. Use
// NewMenuBar to build one; the zero value has no entries and no open
// menu. See TheoryOfMenu.
type MenuBar struct {
	Entries []MenuEntry
	// Open is the index of the open dropdown, or -1 when none is open.
	Open int
	// Armed records whether the hover mode is armed: armed by the press
	// that opens a dropdown, released by a terminating press. While
	// armed, hovering a category title pops up its menu even when a
	// previous hover over a top-level entry hid the open dropdown.
	Armed bool
}

// NewMenuBar returns a menu bar over the given entries with no menu
// open. See TheoryOfMenu.
func NewMenuBar(entries []MenuEntry) MenuBar {
	return MenuBar{Entries: entries, Open: -1}
}

// Layout lays the menu titles across the top row: each title occupies
// its own text width and a gap separates the titles. It is pure: the
// renderer draws from it and the hit tests map presses through it, so
// the two cannot disagree. Titles past the width are dropped. See
// TheoryOfMenu.
func (m *MenuBar) Layout(width int) []MenuBarSlot {
	var out []MenuBarSlot
	x := 0
	for i, entry := range m.Entries {
		end := x + len(entry.Title)
		if end > width {
			break
		}
		out = append(out, MenuBarSlot{Index: i, X0: x, X1: end})
		x = end + MenuBarGap
	}
	return out
}

// Hit maps a press on the menu bar row onto the entry index it hits.
// The bar occupies the row topInset reserves; a press in the gaps
// between titles hits nothing. See TheoryOfMenu.
func (m *MenuBar) Hit(topInset, width, x, y int) (index int, ok bool) {
	if topInset <= 0 || y != 0 {
		return -1, false
	}
	for _, slot := range m.Layout(width) {
		if x >= slot.X0 && x < slot.X1 {
			return slot.Index, true
		}
	}
	return -1, false
}

// Element renders the menu bar row: the category titles as plain text,
// with no background of its own so the row keeps the terminal default.
// The open menu's title is bold; the title under the pointer renders
// reversed. Hover is affordance only and never triggers actions. See
// TheoryOfMenu.
func (m *MenuBar) Element(width, hoverTitle int) Element {
	var children []any
	for _, slot := range m.Layout(width) {
		box := Box{Top: 0, Left: slot.X0, Bottom: 1, Right: slot.X1}
		specs := []any{m.Entries[slot.Index].Title, box}
		if slot.Index == m.Open {
			specs = append(specs, Bold(true))
		}
		if slot.Index == hoverTitle {
			specs = append(specs, Reverse(true))
		}
		children = append(children, Text(specs...))
	}
	return Overlay(children...)
}

// DropdownBox returns the box of the open menu's dropdown: the item
// rows directly under the menu's title slot, one cell of side padding
// and no vertical padding, shifted left and clamped so it stays on the
// screen. The renderer and the item hit test share it, so they cannot
// disagree. See TheoryOfMenu.
func (m *MenuBar) DropdownBox(width, height int) (Box, bool) {
	if m.Open < 0 || m.Open >= len(m.Entries) {
		return Box{}, false
	}
	var slot MenuBarSlot
	found := false
	for _, s := range m.Layout(width) {
		if s.Index == m.Open {
			slot = s
			found = true
		}
	}
	if !found {
		return Box{}, false
	}
	entry := m.Entries[m.Open]
	itemWidth := 0
	for _, item := range entry.Items {
		itemWidth = max(itemWidth, len(item.Label))
	}
	w := itemWidth + MenuDropdownPadding*2
	left := min(slot.X0, max(width-w, 0))
	h := len(entry.Items)
	box := Box{
		Top:    1,
		Left:   left,
		Bottom: min(1+h, max(height, 1)),
		Right:  min(left+w, max(width, 1)),
	}
	if box.Width() <= 0 || box.Height() <= 0 {
		return Box{}, false
	}
	return box, true
}

// Dropdown renders the open menu's dropdown over the content: a
// borderless box holding exactly the item rows, one per row, with one
// cell of side padding; the fill paints baseBG (the terminal default
// when unset), erasing what is under it. The item under the pointer
// renders reversed. It returns nil when no menu is open or the box
// degenerates. See TheoryOfMenu.
func (m *MenuBar) Dropdown(width, height, hoverItem int, baseBG Color) Element {
	box, ok := m.DropdownBox(width, height)
	if !ok {
		return nil
	}
	entry := m.Entries[m.Open]
	children := []any{Rect(
		box,
		Fill(true),
		BGColor(baseBG),
	)}
	for i, item := range entry.Items {
		specs := []any{item.Label,
			Box{Top: box.Top + i, Left: box.Left + MenuDropdownPadding, Bottom: box.Top + 1 + i, Right: box.Right - MenuDropdownPadding},
		}
		if i == hoverItem {
			specs = append(specs, Reverse(true))
		}
		children = append(children, Text(specs...))
	}
	return Overlay(children...)
}

// TitleAt returns the entry index the pointer is over, or -1. The
// highlight is affordance only: hover never opens menus or runs
// actions. See TheoryOfMenu.
func (m *MenuBar) TitleAt(topInset, width, x, y int) int {
	index, ok := m.Hit(topInset, width, x, y)
	if !ok {
		return -1
	}
	return index
}

// ItemAt returns the row of the open menu's dropdown item the pointer
// is over, or -1. The item highlights only where a press would run it:
// inside the item's text columns, not the dropdown's side padding. See
// TheoryOfMenu.
func (m *MenuBar) ItemAt(width, height, x, y int) int {
	box, ok := m.DropdownBox(width, height)
	if !ok {
		return -1
	}
	if y < box.Top || y >= box.Bottom ||
		x < box.Left+MenuDropdownPadding || x >= box.Right-MenuDropdownPadding {
		return -1
	}
	row := y - box.Top
	if row >= len(m.Entries[m.Open].Items) {
		return -1
	}
	return row
}

// Motion handles pointer motion: while armed, motion over a category
// title pops up that title's menu, motion over a top-level entry hides
// the open dropdown without disarming, and motion elsewhere keeps it.
// Hovering back to a category title re-pops its menu. No hover runs an
// action. See TheoryOfMenu.
func (m *MenuBar) Motion(topInset, width, x, y int) {
	if !m.Armed {
		return
	}
	index, ok := m.Hit(topInset, width, x, y)
	if !ok {
		return
	}
	if index == m.Open {
		return
	}
	if m.Entries[index].IsTopLevel() {
		m.Open = -1
		return
	}
	m.Open = index
}

// Press handles a left press at the given cell and reports the action
// to run, or "" when the press triggers none. consumed reports whether
// the menu bar or its open dropdown consumed the press: an unconsumed
// press runs the caller's ordinary handling. A consumed press closes
// the menu and disarms. See TheoryOfMenu.
func (m *MenuBar) Press(topInset, width, height, x, y int) (action string, consumed bool) {
	if index, ok := m.Hit(topInset, width, x, y); ok {
		entry := m.Entries[index]
		if entry.IsTopLevel() {
			m.Close()
			return entry.Action, true
		}
		// A category title press opens, switches, or closes the
		// dropdown. Pressing the open title again closes it and
		// disarms; any other title press opens or switches and arms.
		if m.Open == index {
			m.Close()
			return "", true
		}
		m.Open = index
		m.Armed = true
		return "", true
	}
	if box, ok := m.DropdownBox(width, height); ok &&
		x >= box.Left && x < box.Right && y >= box.Top && y < box.Bottom {
		open := m.Open
		m.Close()
		row := y - box.Top
		if row >= 0 && row < len(m.Entries[open].Items) &&
			x >= box.Left+MenuDropdownPadding && x < box.Right-MenuDropdownPadding {
			return m.Entries[open].Items[row].Action, true
		}
		return "", true
	}
	m.Close()
	return "", false
}

// Close closes the open dropdown and disarms the hover mode. See
// TheoryOfMenu.
func (m *MenuBar) Close() {
	m.Open = -1
	m.Armed = false
}
