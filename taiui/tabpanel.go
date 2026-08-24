package taiui

import "fmt"

const TheoryOfTabPanel = `
taiui tab panel theory:
- TabPanel builds the element of one tab in a tabbed panel layout: a
  collapsed tab renders its CollapsedPanel strip keyed by its number
  ("1 Output"), an expanded tab renders its Panel with the current
  label, highlight, content lines, and scroll view. The key/title pair
  decouples the persistent strip label from the dynamic panel label,
  which may carry status suffixes such as "(generating...)".
- PaneHeight derives the scroll view height from the panel box: the
  one-row label strip pinned to the top leaves box height minus one row
  for content, never less than one.
`

// TabPanel builds the element of one tab: a collapsed strip or an
// expanded panel, laid out by Tabs.Boxes. key and title form the
// collapsed strip's label ("1 Output"); label is the expanded panel's
// title. It returns nil for a degenerate box, which layouts skip. See
// TheoryOfTabPanel.
func TabPanel(box Box, key int, title, label string, highlight, expanded, focus bool, lines []Line, scroll ScrollState, style PanelStyle) Element {
	if box.Width() <= 0 || box.Height() <= 0 {
		return nil
	}
	if !expanded {
		return CollapsedPanel(
			box,
			fmt.Sprintf("%d %s", key, title),
			focus,
			style,
		)
	}
	return Panel(
		box,
		label,
		highlight,
		lines,
		scroll.Offset,
		focus,
		scroll.Follow,
		style,
	)
}

// PaneHeight returns the scroll view height of a panel box: the panel's
// one-row label strip leaves box height minus one row for content,
// never less than one.
func PaneHeight(box Box) int {
	return max(box.Height()-1, 1)
}
