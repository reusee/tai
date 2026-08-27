package taiui

import (
	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3/vt"
)

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
- MaxSizes caps the split-axis extent of expanded, unfocused tabs:
  MaxSizes[i] bounds tab i's share of the split axis (rows in the
  stacked layout, columns in vertical split); zero, negative, or a
  missing entry leaves the tab uncapped, and the focused tab always
  ignores its cap. The extent a capped tab gives up is redistributed
  among the uncapped expanded tabs by weight, so the boxes still tile
  the screen exactly.
- Panel renders an expanded tab: a one-row label strip pinned to the
  top and a scroll view spanning the remaining rows. Panel is a first-class
  Element (_Panel) that renders only the visible lines in O(window) time,
  avoiding virtual-column overhead when content is large. The scrollbar is
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
	// MaxSizes caps the split-axis extent of each expanded, unfocused
	// tab: MaxSizes[i] bounds tab i's share of the split axis (rows in
	// the stacked layout, columns in vertical split). Zero, negative,
	// or a missing entry leaves the tab uncapped, and the focused tab
	// always ignores its cap. See TheoryOfTabs.
	MaxSizes   []int
	focusOrder int
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
			totalWeight += t.weightOf(i)
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
		sizes := t.expandedSizes(expandedWidth, expandedIndices, totalWeight)
		edge := 0
		pos := 0
		for i := 0; i < t.Count; i++ {
			if t.Expanded[i] {
				boxes[i] = Box{Top: 0, Left: edge, Bottom: height, Right: edge + sizes[pos]}
				edge += sizes[pos]
				pos++
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
	sizes := t.expandedSizes(expandedHeight, expandedIndices, totalWeight)
	edge := 0
	pos := 0
	for i := 0; i < t.Count; i++ {
		if t.Expanded[i] {
			boxes[i] = Box{Top: edge, Left: 0, Bottom: edge + sizes[pos], Right: width}
			edge += sizes[pos]
			pos++
		} else {
			boxes[i] = Box{Top: edge, Left: 0, Bottom: edge + 1, Right: width}
			edge++
		}
	}
	return boxes
}

// weightOf returns the flex weight of tab idx: the focused tab weighs 3,
// every other expanded tab 1. See TheoryOfTabs.
func (t *Tabs) weightOf(idx int) int {
	if idx == t.Focus {
		return 3
	}
	return 1
}

// maxSizeOf returns the split-axis cap of tab idx: zero when the tab is
// uncapped — no MaxSizes entry, a non-positive value, or the focused
// tab, which always ignores its cap. See TheoryOfTabs.
func (t *Tabs) maxSizeOf(idx int) int {
	if idx == t.Focus || len(t.MaxSizes) != t.Count {
		return 0
	}
	return max(t.MaxSizes[idx], 0)
}

// expandedSizes computes the split-axis extent of each expanded tab, in
// expandedIndices order: weights split the extent, capped tabs are
// clamped, and the extent they free is redistributed among the uncapped
// expanded tabs by weight, with the last uncapped tab absorbing the
// rounding remainder. When every expanded tab is capped, the freed
// extent stays unused and the boxes underfill the screen.
func (t *Tabs) expandedSizes(extent int, expandedIndices []int, totalWeight int) []int {
	sizes := make([]int, len(expandedIndices))
	used := 0
	for pos, idx := range expandedIndices {
		if pos == len(expandedIndices)-1 {
			sizes[pos] = extent - used
		} else {
			sizes[pos] = extent * t.weightOf(idx) / totalWeight
		}
		used += sizes[pos]
	}
	freed := 0
	uncappedWeight := 0
	for pos, idx := range expandedIndices {
		if sizeCap := t.maxSizeOf(idx); sizeCap > 0 && sizes[pos] > sizeCap {
			freed += sizes[pos] - sizeCap
			sizes[pos] = sizeCap
		}
		if t.maxSizeOf(idx) == 0 {
			uncappedWeight += t.weightOf(idx)
		}
	}
	if freed == 0 || uncappedWeight == 0 {
		return sizes
	}
	lastUncapped := -1
	for pos, idx := range expandedIndices {
		if t.maxSizeOf(idx) == 0 {
			lastUncapped = pos
		}
	}
	redistributed := 0
	for pos, idx := range expandedIndices {
		if pos == lastUncapped || t.maxSizeOf(idx) > 0 {
			continue
		}
		add := freed * t.weightOf(idx) / uncappedWeight
		sizes[pos] += add
		redistributed += add
	}
	sizes[lastUncapped] += freed - redistributed
	return sizes
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

var _ Element = _Panel{}

// _Panel is an expanded tab panel rendered in O(window) time.
type _Panel struct {
	box       Box
	label     string
	highlight bool
	lines     []Line
	offset    int
	focus     bool
	follow    bool
	style     PanelStyle
}

func (_Panel) element() {}

// Panel renders an expanded tab: a one-row label strip pinned to the
// top and a scroll view spanning the remaining rows. It renders visible
// items directly in O(window) time. See TheoryOfTabs.
func Panel(box Box, label string, highlight bool, lines []Line, offset int, focus, follow bool, style PanelStyle) _Panel {
	return _Panel{
		box:       box,
		label:     label,
		highlight: highlight,
		lines:     lines,
		offset:    offset,
		focus:     focus,
		follow:    follow,
		style:     style,
	}
}

func renderPanel(p _Panel, box Box, style Style, draw drawFunc, cursor cursorFunc, options displaywidth.Options) {
	// The constructor box is authoritative, like a Box spec override: a
	// degenerate box (zero width or height) renders nothing. Falling back
	// to the parent-assigned box would draw the panel over regions the
	// caller did not assign to it.
	box = p.box
	if box.Width() <= 0 || box.Height() <= 0 {
		return
	}

	base := p.style.BaseBG
	if p.focus {
		base = p.style.FocusBG
	}
	labelFg := p.style.LabelFG
	if p.highlight {
		labelFg = p.style.ActiveLabelFG
	} else if p.focus {
		labelFg = p.style.FocusLabelFG
	}

	iter := getGraphemeIter()
	defer putGraphemeIter(iter)

	// Header row
	headerStyle := style.WithBg(base).WithFg(labelFg)
	if p.focus {
		headerStyle = withAttrOn(headerStyle, true, vt.Bold)
	}
	renderListLine("  "+p.label+"  ", Box{
		Top:    box.Top,
		Left:   box.Left,
		Bottom: box.Top + 1,
		Right:  box.Right,
	}, headerStyle, true, draw, options, iter)

	scrollHeight := box.Height() - 1
	if scrollHeight <= 0 {
		return
	}

	contentHeight := len(p.lines)
	fromY := p.offset
	if fromY < 0 {
		fromY = 0
	}
	maxFromY := max(contentHeight-scrollHeight, 0)
	if fromY > maxFromY {
		fromY = maxFromY
	}

	showScrollbar := !p.follow && contentHeight > scrollHeight
	contentRight := box.Right
	if showScrollbar {
		contentRight = box.Right - 1
	}

	toY := min(fromY+scrollHeight, contentHeight)
	for i := fromY; i < toY; i++ {
		row := box.Top + 1 + (i - fromY)
		line := p.lines[i]
		lineStyle := style.WithBg(base)
		if line.BGColor != NoColor && line.BGColor != 0 {
			lineStyle = lineStyle.WithBg(line.BGColor)
		}
		if line.Color != NoColor && line.Color != 0 {
			lineStyle = lineStyle.WithFg(line.Color)
		}
		renderListLine(line.Text, Box{
			Top:    row,
			Left:   box.Left,
			Bottom: row + 1,
			Right:  contentRight,
		}, lineStyle, true, draw, options, iter)
	}

	// Empty rows below content
	emptyStyle := style.WithBg(base)
	for y := box.Top + 1 + max(0, contentHeight-fromY); y < box.Bottom; y++ {
		for x := box.Left; x < contentRight; x++ {
			draw(x, y, ' ', nil, emptyStyle)
		}
	}

	// Scrollbar
	if showScrollbar {
		thumbSize := max(1, scrollHeight*scrollHeight/contentHeight)
		thumbY := fromY * (scrollHeight - thumbSize) / (contentHeight - scrollHeight)
		thumbStyle := withAttrOn(DarkerOrLighterStyle(style.WithBg(base), 15), true, vt.Bold)
		trackStyle := style.WithBg(base)
		for row := 0; row < scrollHeight; row++ {
			y := box.Top + 1 + row
			if row >= thumbY && row < thumbY+thumbSize {
				draw(box.Right-1, y, '█', nil, thumbStyle)
			} else {
				draw(box.Right-1, y, ' ', nil, trackStyle)
			}
		}
	}
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
