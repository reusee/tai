package taiui

const TheoryOfTabPanel = `
taiui tab panel theory:
- TabPanel builds the element of one tab in a tabbed panel layout: a
  collapsed tab renders its CollapsedPanel strip with the tab title,
  an expanded tab renders its Panel with the current label, highlight,
  content lines, and scroll view. The title/label pair decouples the
  persistent strip label from the dynamic panel label, which may carry
  status suffixes such as "(generating...)".
- An unseen collapsed tab renders a red dot glyph right after its
  label, marking content that arrived while the tab was collapsed; the
  glyph is a plain colorable character, so every terminal renders it.
  The one-column vertical strip falls back to a red background cell,
  because the horizontal label cannot fit there.
- The panel's title row centers its label across the full box width,
  filled edge to edge, and a collapsed strip's title centers along the
  strip's long axis the same way: vertically in a narrow column,
  horizontally in a short row. An optional ContentIndent spec indents the
  content rows from the box's left edge, reserving the strip for
  callers that draw controls beside the content; the title row never
  carries the indent.
- PaneHeight derives the scroll view height from the panel box: the
  one-row label strip pinned to the top leaves box height minus one row
  for content, never less than one.
`

// TabPanel builds the element of one tab: a collapsed strip or an
// expanded panel, laid out by Tabs.Boxes. title is the collapsed
// strip's label; label is the expanded panel's title. unseen paints
// the red-circle unseen emoji on the collapsed strip. The specs apply
// to the expanded panel; a collapsed strip ignores them. It returns
// nil for a degenerate box, which layouts skip. See TheoryOfTabPanel.
func TabPanel(box Box, title, label string, highlight, expanded, focus, unseen bool, lines []Line, scroll ScrollState, style PanelStyle, specs ...any) Element {
	if box.Width() <= 0 || box.Height() <= 0 {
		return nil
	}
	if !expanded {
		return CollapsedPanel(
			box,
			title,
			focus,
			unseen,
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
		specs...,
	)
}

// PaneHeight returns the scroll view height of a panel box: the panel's
// one-row label strip leaves box height minus one row for content,
// never less than one.
func PaneHeight(box Box) int {
	return max(box.Height()-1, 1)
}
