package taiui

import (
	"strconv"
	"strings"
)

const TheoryOfMouseInteraction = `
taiui mouse interaction theory:
- ParseMouseKey is the consumption counterpart of the mouse key emission
  in ReadKeys: it splits a mouse key name ("mouse-left@12,34") into its
  event kind and 0-based cell coordinates. Applications route the parsed
  event into their interaction model.
- TabAt locates the tab whose panel box contains a cell, so an
  application can map pointer coordinates onto its tab layout.
- TabMouse is the standard pointer interaction over a tabbed panel
  layout: wheel events scroll the tab under the cursor without changing
  the focus; a left press on a collapsed strip expands and focuses the
  tab; a press on an expanded tab's label strip toggles it like its
  number key; a press inside the scroll area focuses the tab and anchors
  a drag-scroll. The drag is anchored to the press origin, so the
  content moves with the pointer even when motion events are skipped.
  The zero value is inert.
`

// ParseMouseKey splits a mouse key name emitted by ReadKeys into its
// event kind and 0-based cell coordinates: "mouse-left@12,34" returns
// ("left", 12, 34, true). See TheoryOfMouseInteraction and
// TheoryOfMouseInput.
func ParseMouseKey(key string) (event string, x, y int, ok bool) {
	name, coord, found := strings.Cut(key, "@")
	if !found || name == "" {
		return "", 0, 0, false
	}
	event = strings.TrimPrefix(name, MouseKeyPrefix)
	if event == "" || event == name {
		return "", 0, 0, false
	}
	xStr, yStr, found := strings.Cut(coord, ",")
	if !found {
		return "", 0, 0, false
	}
	var err error
	if x, err = strconv.Atoi(xStr); err != nil {
		return "", 0, 0, false
	}
	if y, err = strconv.Atoi(yStr); err != nil {
		return "", 0, 0, false
	}
	return event, x, y, true
}

// TabAt returns the index of the tab whose panel box contains the given
// 0-based cell coordinates, or -1 when the point is outside every panel.
// The tab boxes tile the screen (expanded panels and collapsed strips
// are laid out without gaps), so a point normally falls in exactly one
// panel. See TheoryOfMouseInteraction.
func TabAt(tabs *Tabs, width, height, x, y int) int {
	boxes := tabs.Boxes(width, height)
	for idx, box := range boxes {
		if x >= box.Left && x < box.Right && y >= box.Top && y < box.Bottom {
			return idx
		}
	}
	return -1
}

// TabMouse tracks an in-progress drag-scroll over a tabbed panel layout.
// Its zero value is inert. It holds no locks: the caller serializes
// access, typically in the single-threaded event loop. See
// TheoryOfMouseInteraction.
type TabMouse struct {
	dragging        bool
	dragTab         int
	dragStartY      int
	dragStartOffset int
}

// Press handles a left-button press at the given cell. A press on a
// collapsed tab's strip expands it and takes the focus, resuming the
// live tail. A press on an expanded tab's label strip toggles it like
// its number key: pressing the focused tab collapses it, pressing
// another tab's strip takes the focus without collapsing. A press
// inside an expanded tab's scroll area focuses the tab and records the
// drag origin for drag-scrolling. See TheoryOfMouseInteraction.
func (m *TabMouse) Press(tabs *Tabs, scrolls []ScrollState, width, height, x, y int) {
	m.dragging = false
	idx := TabAt(tabs, width, height, x, y)
	if idx < 0 {
		return
	}
	box := tabs.Boxes(width, height)[idx]
	if !tabs.Expanded[idx] {
		tabs.Toggle(idx)
		scrolls[idx].Follow = true
		return
	}
	if y == box.Top {
		tabs.Toggle(idx)
		return
	}
	if tabs.Focus != idx {
		tabs.Toggle(idx)
	}
	m.dragging = true
	m.dragTab = idx
	m.dragStartY = y
	m.dragStartOffset = scrolls[idx].Offset
}

// Wheel scrolls the tab whose panel is under the given cell by delta
// rows in response to a wheel event. The wheel targets the pane under
// the cursor without changing the focus, so the user can read any pane
// while keyboard navigation stays put; scrolling a collapsed tab is a
// no-op. See TheoryOfMouseInteraction.
func (m *TabMouse) Wheel(tabs *Tabs, scrolls []ScrollState, width, height, x, y, delta int) {
	idx := TabAt(tabs, width, height, x, y)
	if idx < 0 || !tabs.Expanded[idx] {
		return
	}
	scrolls[idx].Scroll(delta)
}

// Drag scrolls the tab that the press started in by the pointer's
// movement since the press: dragging up reveals earlier content,
// dragging down reveals the tail. The scroll offset is anchored to the
// press origin so the content follows the pointer even when motion
// events are skipped. See TheoryOfMouseInteraction.
func (m *TabMouse) Drag(tabs *Tabs, scrolls []ScrollState, y int) {
	if !m.dragging || !tabs.Expanded[m.dragTab] {
		return
	}
	scrolls[m.dragTab].ScrollTo(m.dragStartOffset + (m.dragStartY - y))
}

// Release ends an in-progress drag-scroll. See TheoryOfMouseInteraction.
func (m *TabMouse) Release() {
	m.dragging = false
}
