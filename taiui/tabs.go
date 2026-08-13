package taiui

const TheoryOfTabs = `
taiui tabs theory:
- Tabs is the tab state machine of a terminal UI: each tab is expanded
  or collapsed, carries a has-content flag, and records the order in
  which it last gained focus. All tabs are collapsed by default; a
  collapsed tab expands automatically the FIRST time content for it
  arrives, without changing an existing focus. Only when no tab is
  focused does the first auto-expanded tab become the focus, so keyboard
  navigation remains usable. Subsequent content arrivals do not
  re-expand a tab the user collapsed; AutoExpand reports whether the tab
  was newly expanded so the caller can resume following the tail.
- Toggle implements the number-key semantics: pressing a focused tab's
  key collapses it to a thin strip and moves the focus to the expanded
  tab that was last focused; pressing a non-focused or collapsed tab's
  key expands it (if collapsed) and takes the focus. Re-expanding a
  collapsed tab resumes following the live tail; switching to an
  already-expanded tab keeps its current view.
- Boxes lays out the tabs: in vertical split (side by side), collapsed
  tabs take one column each and expanded tabs share the remaining width
  proportionally to their weights (the focused tab has weight 3, every
  other expanded tab weight 1); in horizontal split (stacked), collapsed
  tabs take one row each and expanded tabs share the remaining height.
  The last expanded tab absorbs the rounding remainder. Tabs are laid
  out in index order, so a collapsed tab stays in its original position
  rather than being pushed to the edge.
- Panel renders an expanded tab: a one-row label strip pinned to the
  top and a scroll view spanning the remaining rows. The scrollbar is
  hidden while the pane follows the tail, because at the latest position
  there is nothing left to scroll toward. CollapsedPanel renders the
  thin strip: the label is written vertically in a narrow column and
  horizontally in a short row.
`

// Tabs is the tab state machine of a terminal UI. See TheoryOfTabs.
type Tabs struct {
	Count         int
	Expanded      []bool
	HasContent    []bool
	LastFocus     []int
	Focus         int
	SplitVertical bool
	focusOrder    int
}

// NewTabs creates a Tabs with all tabs collapsed and no focus.
func NewTabs(count int) *Tabs {
	return &Tabs{
		Count:      count,
		Expanded:   make([]bool, count),
		HasContent: make([]bool, count),
		LastFocus:  make([]int, count),
		Focus:      -1,
	}
}

// AutoExpand expands a collapsed tab the first time content for it
// arrives. It never changes an existing focus: a tab popping open cannot
// steal attention from the pane the user is reading. Only when no tab is
// focused does the first auto-expanded tab become the focus. Subsequent
// content arrivals do not re-expand a tab the user collapsed. It reports
// whether the tab was newly expanded, so the caller can resume following
// the tail.
func (t *Tabs) AutoExpand(idx int) bool {
	if idx < 0 || idx >= t.Count {
		return false
	}
	if t.HasContent[idx] {
		return false
	}
	t.HasContent[idx] = true
	if t.Expanded[idx] {
		return false
	}
	t.Expanded[idx] = true
	if t.Focus == -1 {
		t.Focus = idx
		t.LastFocus[idx] = t.focusOrder
		t.focusOrder++
	}
	return true
}

// Toggle implements the number-key semantics: a focused tab collapses
// and the focus moves to the expanded tab that was last focused; a
// non-focused or collapsed tab expands (if collapsed) and becomes the
// focus. It reports whether the tab was newly expanded, so the caller
// can resume following the tail.
func (t *Tabs) Toggle(idx int) (newlyExpanded bool) {
	if idx < 0 || idx >= t.Count {
		return false
	}
	if t.Focus == idx {
		t.Expanded[idx] = false
		t.FocusLastExpanded()
		return false
	}
	if !t.Expanded[idx] {
		t.Expanded[idx] = true
		newlyExpanded = true
	}
	t.Focus = idx
	t.LastFocus[idx] = t.focusOrder
	t.focusOrder++
	return
}

// FocusLastExpanded moves the focus to the expanded tab that was last
// focused. Tabs that were never focused tie-break by index order. When
// no tab is expanded, the focus becomes -1.
func (t *Tabs) FocusLastExpanded() {
	best := -1
	bestOrder := -2
	for i := 0; i < t.Count; i++ {
		if !t.Expanded[i] {
			continue
		}
		if t.LastFocus[i] > bestOrder {
			bestOrder = t.LastFocus[i]
			best = i
		}
	}
	t.Focus = best
}

// CycleFocus advances the focus to the next expanded tab after the
// current one, wrapping around. Collapsed tabs are skipped. When no tab
// is expanded, the focus becomes -1.
func (t *Tabs) CycleFocus() {
	if t.Focus >= 0 {
		for i := 1; i <= t.Count; i++ {
			f := (t.Focus + i) % t.Count
			if t.Expanded[f] {
				t.Focus = f
				t.LastFocus[f] = t.focusOrder
				t.focusOrder++
				return
			}
		}
	}
	for i := 0; i < t.Count; i++ {
		if t.Expanded[i] {
			t.Focus = i
			t.LastFocus[i] = t.focusOrder
			t.focusOrder++
			return
		}
	}
	t.Focus = -1
}

// Boxes computes the panel box of each tab. See TheoryOfTabs.
func (t *Tabs) Boxes(width, height int) []Box {
	boxes := make([]Box, t.Count)

	var expandedIndices []int
	totalWeight := 0
	for i := 0; i < t.Count; i++ {
		if t.Expanded[i] {
			expandedIndices = append(expandedIndices, i)
			weight := 1
			if i == t.Focus {
				weight = 3
			}
			totalWeight += weight
		}
	}
	collapsedCount := t.Count - len(expandedIndices)
	if totalWeight <= 0 {
		totalWeight = 1
	}

	if t.SplitVertical {
		expandedWidth := width - collapsedCount
		if expandedWidth < 0 {
			expandedWidth = 0
		}
		edge := 0
		expandedEdge := 0
		expandedPos := 0
		for i := 0; i < t.Count; i++ {
			if t.Expanded[i] {
				weight := 1
				if i == t.Focus {
					weight = 3
				}
				var size int
				if expandedPos == len(expandedIndices)-1 {
					size = expandedWidth - expandedEdge
				} else {
					size = expandedWidth * weight / totalWeight
				}
				boxes[i] = Box{Top: 0, Left: edge, Bottom: height, Right: edge + size}
				edge += size
				expandedEdge += size
				expandedPos++
			} else {
				boxes[i] = Box{Top: 0, Left: edge, Bottom: height, Right: edge + 1}
				edge++
			}
		}
		return boxes
	}

	expandedHeight := height - collapsedCount
	if expandedHeight < 0 {
		expandedHeight = 0
	}
	edge := 0
	expandedEdge := 0
	expandedPos := 0
	for i := 0; i < t.Count; i++ {
		if t.Expanded[i] {
			weight := 1
			if i == t.Focus {
				weight = 3
			}
			var size int
			if expandedPos == len(expandedIndices)-1 {
				size = expandedHeight - expandedEdge
			} else {
				size = expandedHeight * weight / totalWeight
			}
			boxes[i] = Box{Top: edge, Left: 0, Bottom: edge + size, Right: width}
			edge += size
			expandedEdge += size
			expandedPos++
		} else {
			boxes[i] = Box{Top: edge, Left: 0, Bottom: edge + 1, Right: width}
			edge++
		}
	}
	return boxes
}

// PanelStyle styles the tab panels. BaseBG is the background of every
// unfocused tab, FocusBG of the focused tab. LabelFG is the label color
// of an unfocused tab, FocusLabelFG of the focused tab, and ActiveLabelFG
// highlights a label whose tab carries an active state (e.g., an
// in-flight generation request).
type PanelStyle struct {
	BaseBG        Color
	FocusBG       Color
	LabelFG       Color
	FocusLabelFG  Color
	ActiveLabelFG Color
}

// Panel renders an expanded tab: a one-row label strip pinned to the
// top and a scroll view spanning the remaining rows. The scrollbar is
// hidden while the pane follows the tail. See TheoryOfTabs.
func Panel(box Box, label string, highlight bool, lines []Line, offset int, focus, follow bool, style PanelStyle) Element {
	base := style.BaseBG
	if focus {
		base = style.FocusBG
	}
	labelFg := style.LabelFG
	if highlight {
		labelFg = style.ActiveLabelFG
	} else if focus {
		labelFg = style.FocusLabelFG
	}
	headerBox := box
	headerBox.Bottom = box.Top + 1
	scrollBox := box
	scrollBox.Top = box.Top + 1
	scrollSpecs := []any{Box(scrollBox), Fill(true), BGColor(base)}
	if !follow {
		scrollSpecs = append(scrollSpecs, Scrollbar(true))
	}
	return Overlay(
		Text(
			"  "+label+"  ",
			Box(headerBox),
			Fill(true),
			BGColor(base),
			Bold(focus),
			FGColor(labelFg),
		),
		VerticalScroll(
			LinesElement(lines, scrollBox),
			offset,
			scrollSpecs...,
		),
	)
}

// CollapsedPanel renders a collapsed tab as a thin strip showing the
// tab's key and title. In a narrow column the label is written
// vertically; in a short row it is written horizontally.
func CollapsedPanel(box Box, label string, focus bool, style PanelStyle) Element {
	base := style.BaseBG
	if focus {
		base = style.FocusBG
	}
	labelFg := style.LabelFG
	if focus {
		labelFg = style.FocusLabelFG
	}
	if box.Width() < box.Height() {
		var lines []string
		for _, r := range label {
			lines = append(lines, string(r))
		}
		return Rect(
			Box(box),
			Fill(true),
			BGColor(base),
			Text(
				lines,
				Bold(focus),
				FGColor(labelFg),
			),
		)
	}
	return Rect(
		Box(box),
		Fill(true),
		BGColor(base),
		Text(
			"  "+label+"  ",
			Bold(focus),
			FGColor(labelFg),
		),
	)
}
