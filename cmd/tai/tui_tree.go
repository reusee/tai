package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/clipperhouse/displaywidth"
	"github.com/reusee/tai/taiui"
	"github.com/reusee/tai/tree"
)

const TheoryOfTreeTab = `
Tree tab theory (cmd/tai):
- The Tree tab renders the session tree pipeline.Run yields: setTree
  stores the latest *tree.Tree — the same tree the pipeline writes,
  never a separately maintained copy — and the display walks it
  depth-first from the root, so a goal run's loops appear as loop-N
  branches and a fresh run's nodes sit directly under the root. The
  walk always starts at the tree root, so every loop's state is
  displayed: projections filter by type and author, never by loop,
  and no display path reduces the view to the current loop.
- The tab renders a projection of the tree, cycled with the v key and
  selectable through the View menu's Tree view items: all shows every
  node; events shows the event nodes; summary the summary nodes;
  model, program, and user the nodes of that author. The projection
  keeps each shown node's ancestors (tree.Extract), so the outline
  stays readable.
- Every node renders one line by default: "name [type/author] first
  content line". A node whose content spans more than one line is
  expandable and collapsed by default; a press on any of its rows — or
  Enter for the last expandable node — reveals the remaining lines,
  and a press on an expanded node's header rows folds it, so clicking
  inside a long expanded body never collapses it by accident. Lines
  never wrap: each line renders as one display row truncated at the
  pane edge, so the full content stays behind expansion.
- Headers align as columns per indent level: the node name pads to
  the level's widest visible name and the [type/author] fragment to
  the level's widest visible meta, so entries of one level read as
  aligned columns; the pad widths derive from the projected entries.
- The expanded tab reserves a status column at its left edge, one Han
  character wide, like the Output tab's control column: every
  expandable node carries the fold glyph ▾ while expanded and ▸ while
  collapsed, rendered beside the node's first display row and clamped
  into the viewport. A press on the control's cells toggles the node,
  and expanding a collapsed node scrolls the view to the node's first
  row; single-line nodes carry no control.
- Node lines are colored by author and type: event and error nodes in
  the log color, user inputs in the user color, program nodes in the
  system color, model nodes in the default foreground. The tab
  alternates the two background shades per node: all display lines of
  one node share one shade, and consecutive nodes alternate.
- Each node's first display line right-aligns the elapsed timer
  ("+0:07") from the session start to the node's insert time; a pane
  too narrow for the timer omits it.
- The attempt-start event node's line ends with the 👉 jump marker: a
  left press on the marker's cells jumps the Output tab to the output
  section that attempt wrote (see TheoryOfTUIOutputSections). Display
  records every node's row range, so a press maps onto the node the
  way the rows render.
- The tab auto-expands on the first event node and follows the tail;
  the unseen-dot, focus, and scroll semantics are the taiui tab
  machine's.
`

// treeViewMode selects one projection of the session tree the Tree tab
// renders. The v key cycles the modes. See TheoryOfTreeTab.
type treeViewMode int

const (
	treeViewAll treeViewMode = iota
	treeViewEvents
	treeViewSummary
	treeViewModel
	treeViewProgram
	treeViewUser
	treeViewModeCount
)

var treeViewLabels = [...]string{"all", "events", "summary", "model", "program", "user"}

// label renders the mode's display label. See TheoryOfTreeTab.
func (m treeViewMode) label() string {
	if int(m) < len(treeViewLabels) {
		return treeViewLabels[m]
	}
	return "all"
}

// predicate returns the node predicate of the mode; nil means every
// node. See TheoryOfTreeTab.
func (m treeViewMode) predicate() func(*tree.Node) bool {
	switch m {
	case treeViewEvents:
		return func(n *tree.Node) bool { return n.Type == tree.TypeEvent }
	case treeViewSummary:
		return func(n *tree.Node) bool { return n.Type == tree.TypeSummary }
	case treeViewModel:
		return func(n *tree.Node) bool { return n.Author == tree.AuthorModel }
	case treeViewProgram:
		return func(n *tree.Node) bool { return n.Author == tree.AuthorProgram }
	case treeViewUser:
		return func(n *tree.Node) bool { return n.Author == tree.AuthorUser }
	default:
		return nil
	}
}

// treeIndentWidth is the per-depth indent of the Tree tab's outline,
// in terminal cells. See TheoryOfTreeTab.
const treeIndentWidth = 2

// treeCached caches one node's display lines keyed by the render
// parameters, so a frame re-renders only nodes that are new or
// repositioned; the alignment widths join the key, because a wider
// entry at the same indent level re-pads every cached sibling. See
// TheoryOfTreeTab.
type treeCached struct {
	width     int
	depth     int
	shade     taiui.Color
	expanded  bool
	nameWidth int
	metaWidth int
	lines     []taiui.Line
}

// treeRowRange maps one rendered node onto its display row range as
// recorded by the last treeDisplay, so a pointer press locates the
// node under the cursor. endRow is exclusive; the header is the
// first row, the row a press folds an expanded node through. See
// TheoryOfTreeTab.
type treeRowRange struct {
	name     string
	startRow int
	endRow   int
}

// treeAlignments carries the per-depth pad widths of the projected
// tree: at each indent level, the widest visible node name and the
// widest visible [type/author] fragment set the column widths, so
// entries of one level read as aligned columns. See TheoryOfTreeTab.
type treeAlignments struct {
	nameWidths map[int]int
	metaWidths map[int]int
}

// treeTabState is the Tree tab's interaction state: the projection
// mode, the per-node expansion toggles, the event nodes already
// consumed for the display signals, the wrapped-line cache, and the
// row ranges of the last display. Guarded by t.mu. See
// TheoryOfTreeTab.
type treeTabState struct {
	mode     treeViewMode
	expanded map[string]bool
	seen     map[string]bool
	cache    map[string]treeCached
	rows     []treeRowRange
}

// treeAlignmentsOf computes the alignments of the projected tree: the
// pad widths derive from the currently visible entries at each indent
// level. See TheoryOfTreeTab.
func treeAlignmentsOf(tr *tree.Tree, options displaywidth.Options) treeAlignments {
	align := treeAlignments{nameWidths: map[int]int{}, metaWidths: map[int]int{}}
	var walk func(n *tree.Node, depth int)
	walk = func(n *tree.Node, depth int) {
		align.nameWidths[depth] = max(align.nameWidths[depth], options.String(n.Name))
		align.metaWidths[depth] = max(align.metaWidths[depth], options.String(treeNodeMeta(n)))
		for _, c := range n.Children() {
			walk(c, depth+1)
		}
	}
	for _, c := range tr.Root().Children() {
		walk(c, 0)
	}
	return align
}

// attemptNumberOf parses the attempt number from an attempt event
// node's content ("attempt 3 start (1/3)"). See TheoryOfTreeTab.
func attemptNumberOf(content string) (int, bool) {
	var num int
	if _, err := fmt.Sscanf(content, "attempt %d", &num); err == nil {
		return num, true
	}
	return 0, false
}

// treeBodyLines returns the node's content lines after the first: the
// body a collapsed node hides. A single-line content yields no body.
// See TheoryOfTreeTab.
func treeBodyLines(n *tree.Node) []string {
	content := strings.TrimRight(n.Content, "\n")
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= 1 {
		return nil
	}
	return lines[1:]
}

// treeNodeExpandable reports whether the node hides a multi-line body
// behind its one-line header. See TheoryOfTreeTab.
func treeNodeExpandable(n *tree.Node) bool {
	return len(treeBodyLines(n)) > 0
}

// treeHeaderText renders the node's header text: the name padded to
// the level's widest visible name, the [type/author] fragment padded
// to the level's widest visible meta, the first content line, the
// expand hint on a collapsed multi-line node, and the jump marker on
// an attempt-start node. The pads align the columns of the same
// indent level. See TheoryOfTreeTab.
func treeHeaderText(n *tree.Node, expanded bool, options displaywidth.Options, nameWidth, metaWidth int) string {
	meta := treeNodeMeta(n)
	var b strings.Builder
	b.WriteString(n.Name)
	if pad := nameWidth - options.String(n.Name); pad > 0 {
		b.WriteString(strings.Repeat(" ", pad))
	}
	b.WriteString(" ")
	b.WriteString(meta)
	if pad := metaWidth - options.String(meta); pad > 0 {
		b.WriteString(strings.Repeat(" ", pad))
	}
	b.WriteString(" ")
	if first := tree.PreviewRunes(n.Content, 0); first != "" {
		b.WriteString(first + " ")
	}
	if body := treeBodyLines(n); len(body) > 0 && !expanded {
		b.WriteString(fmt.Sprintf("⤷ %d more lines", len(body)))
	}
	text := strings.TrimRight(b.String(), " ")
	if strings.HasPrefix(n.Name, "attempt-start") {
		text += " " + eventJumpMarker
	}
	return text
}

// treeNodeMeta renders the node's [type/author] fragment, the column
// the header alignment pads to. See TheoryOfTreeTab.
func treeNodeMeta(n *tree.Node) string {
	return fmt.Sprintf("[%s/%s]", n.Type, n.Author)
}

// treeLineColor maps a node to its display color: event and error
// nodes in the log color, user inputs in the user color, program
// nodes in the system color, model nodes in the default foreground.
// See TheoryOfTreeTab.
func treeLineColor(n *tree.Node) taiui.Color {
	switch {
	case n.Type == tree.TypeEvent || n.Type == tree.TypeError:
		return outputColorLogLine
	case n.Author == tree.AuthorUser:
		return outputColorUserLine
	case n.Author == tree.AuthorProgram:
		return outputColorSystemLine
	default:
		return taiui.NoColor
	}
}

// formatTreeElapsed renders an elapsed duration as a stopwatch
// fragment: "+0:07" under an hour, "+1:02:03" beyond it. See
// TheoryOfTreeTab.
func formatTreeElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int64(d / time.Second)
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60
	if hours > 0 {
		return fmt.Sprintf("+%d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("+%d:%02d", minutes, secs)
}

// setTree stores the latest session tree and consumes its new event
// nodes: an attempt-start node opens the output section the attempt's
// streamed content will fill, and a finish node ends the request's
// generating hint. The tab auto-expands on the first event node. See
// TheoryOfTreeTab and TheoryOfTUIOutputSections.
func (t *TUI) setTree(tr *tree.Tree) {
	if tr == nil {
		return
	}
	t.mu.Lock()
	if t.treeTab.expanded == nil {
		t.treeTab.expanded = make(map[string]bool)
	}
	if t.treeTab.seen == nil {
		t.treeTab.seen = make(map[string]bool)
	}
	eventNodes := tr.ByType(tree.TypeEvent)
	for _, n := range eventNodes {
		if t.treeTab.seen[n.Name] {
			continue
		}
		t.treeTab.seen[n.Name] = true
		if strings.HasPrefix(n.Name, "attempt-start") {
			if num, ok := attemptNumberOf(n.Content); ok {
				t.pendingOwner = &outputSectionOwner{attempt: num}
			}
		}
		if strings.HasPrefix(n.Name, "finish") {
			t.generating = false
		}
	}
	if len(eventNodes) > 0 {
		if t.tabs.AutoExpand(1) {
			t.scrolls[1].Follow = true
		}
	}
	t.treeView = tr
	t.mu.Unlock()
	t.notify()
}

// cycleTreeView advances the Tree tab's projection to the next mode,
// so the user can inspect the tree's internal representation — all
// nodes, only the events, only the summaries, or only one author's
// nodes. See TheoryOfTreeTab.
func (t *TUI) cycleTreeView() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.treeTab.mode = (t.treeTab.mode + 1) % treeViewModeCount
}

// setTreeView selects the Tree tab's projection; the View menu's Tree
// view items dispatch here, mirroring the v key's cycling. See
// TheoryOfTreeTab.
func (t *TUI) setTreeView(mode treeViewMode) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.treeTab.mode = mode
}

// treeTabLabel renders the Tree tab's label with the current
// projection. See TheoryOfTreeTab.
func (t *TUI) treeTabLabel() string {
	return "Tree (" + t.treeTab.mode.label() + ")"
}

// treeDisplay renders the Tree tab's display: the depth-first walk of
// the current projection, each node contributing its header row plus
// its body lines when expanded. The walk starts at the tree root, so
// every goal loop's nodes render. The walk records every node's row
// range, so a pointer press maps onto the node the way the rows
// render. The caller holds t.mu. See TheoryOfTreeTab.
func (t *TUI) treeDisplay(contentWidth int, base taiui.Color) []taiui.Line {
	tr := t.treeView
	if tr == nil {
		return nil
	}
	if t.treeTab.mode != treeViewAll {
		tr = tr.Extract(t.treeTab.mode.predicate())
	}
	alt := taiui.AltBG(base)
	options := taiui.DisplayWidthOptions()
	align := treeAlignmentsOf(tr, options)
	var out []taiui.Line
	t.treeTab.rows = t.treeTab.rows[:0]
	index := 0
	var walk func(n *tree.Node, depth int)
	walk = func(n *tree.Node, depth int) {
		shade := base
		if index%2 == 1 {
			shade = alt
		}
		index++
		lines := t.treeNodeLines(n, depth, shade, contentWidth, options, align)
		start := len(out)
		out = append(out, lines...)
		t.treeTab.rows = append(t.treeTab.rows, treeRowRange{
			name: n.Name, startRow: start, endRow: len(out),
		})
		for _, c := range n.Children() {
			walk(c, depth+1)
		}
	}
	for _, c := range tr.Root().Children() {
		walk(c, 0)
	}
	return out
}

// treeNodeLines renders one node's display lines: the header row and,
// when expanded, one truncated row per body line. Lines never wrap: a
// line longer than the pane truncates at the pane edge, and the full
// content stays behind expansion. Lines are cached per width, depth,
// shade, expansion, and alignment, so a frame re-renders only nodes
// that are new or repositioned. The caller holds t.mu. See
// TheoryOfTreeTab.
func (t *TUI) treeNodeLines(n *tree.Node, depth int, shade taiui.Color, contentWidth int, options displaywidth.Options, align treeAlignments) []taiui.Line {
	expanded := t.treeTab.expanded[n.Name]
	nameWidth := align.nameWidths[depth]
	metaWidth := align.metaWidths[depth]
	if c, ok := t.treeTab.cache[n.Name]; ok &&
		c.width == contentWidth && c.depth == depth && c.shade == shade && c.expanded == expanded &&
		c.nameWidth == nameWidth && c.metaWidth == metaWidth {
		return c.lines
	}
	elapsed := time.Since(t.startTime)
	if !n.InsertTime.After(t.startTime) {
		elapsed = 0
	} else {
		elapsed = n.InsertTime.Sub(t.startTime)
	}
	color := treeLineColor(n)
	indent := strings.Repeat(" ", treeIndentWidth*depth)
	wrapWidth := max(contentWidth-treeIndentWidth*depth, 1)
	timerText := formatTreeElapsed(elapsed)
	timerWidth := options.String(timerText)
	timerZone := timerWidth + 1

	header := treeHeaderText(n, expanded, options, nameWidth, metaWidth)
	body := treeBodyLines(n)
	if !expanded {
		body = nil
	}
	lines := make([]taiui.Line, 0, 1+len(body))
	if wrapWidth > timerZone {
		// The elapsed timer right-aligns on the header row: the
		// header truncates one timer zone narrower and the residual
		// columns pad to the timer. See TheoryOfTreeTab.
		avail := wrapWidth - timerZone
		text := displaywidth.TruncateString(header, avail, "…")
		pad := wrapWidth - timerWidth - options.String(text)
		if pad < 1 {
			pad = 1
		}
		lines = append(lines, taiui.Line{
			Text:    indent + text + strings.Repeat(" ", pad) + timerText,
			Color:   color,
			BGColor: shade,
		})
	} else {
		// A pane too narrow for the timer omits it. See
		// TheoryOfTreeTab.
		lines = append(lines, taiui.Line{
			Text:    indent + displaywidth.TruncateString(header, wrapWidth, "…"),
			Color:   color,
			BGColor: shade,
		})
	}
	for _, line := range body {
		lines = append(lines, taiui.Line{
			Text:    indent + displaywidth.TruncateString(line, wrapWidth, "…"),
			Color:   color,
			BGColor: shade,
		})
	}
	if t.treeTab.cache == nil {
		t.treeTab.cache = make(map[string]treeCached)
	}
	t.treeTab.cache[n.Name] = treeCached{
		width: contentWidth, depth: depth, shade: shade, expanded: expanded,
		nameWidth: nameWidth, metaWidth: metaWidth, lines: lines,
	}
	return lines
}

// treeNodeAtRow returns the node whose recorded row range contains
// row, as recorded by the last display; a row outside every range
// returns nil. See TheoryOfTreeTab.
func (t *TUI) treeNodeAtRow(row int) *tree.Node {
	if t.treeView == nil {
		return nil
	}
	for _, r := range t.treeTab.rows {
		if row >= r.startRow && row < r.endRow {
			if n, ok := t.treeView.Node(r.name); ok {
				return n
			}
		}
	}
	return nil
}

// toggleTreeNodeAtRow toggles the expansion of the expandable node
// whose row range contains row: any row of a collapsed node expands
// it, and the header row of an expanded node collapses it, so
// clicking inside a long expanded body never collapses it by
// accident. See TheoryOfTreeTab.
func (t *TUI) toggleTreeNodeAtRow(row int) {
	for _, r := range t.treeTab.rows {
		if row < r.startRow || row >= r.endRow {
			continue
		}
		if t.treeTab.expanded[r.name] && row > r.startRow {
			// A body row of an expanded node: inert, so reading inside
			// an expanded body never folds it.
			return
		}
		t.toggleTreeNodeByName(r.name)
		return
	}
}

// toggleTreeNodeByName flips one node's expansion, lazily creating
// the expansion map, because a click can arrive before any setTree
// call initialized it. See TheoryOfTreeTab.
func (t *TUI) toggleTreeNodeByName(name string) {
	if t.treeTab.expanded == nil {
		t.treeTab.expanded = make(map[string]bool)
	}
	t.treeTab.expanded[name] = !t.treeTab.expanded[name]
}

// toggleLastTreeExpandable expands or collapses the last expandable
// node of the session tree in depth-first order — typically the latest
// handoff or completion summary — so Enter works on the most recent
// collapsed body without a cursor. See TheoryOfTreeTab.
func (t *TUI) toggleLastTreeExpandable() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.treeView == nil {
		return
	}
	if t.treeTab.expanded == nil {
		t.treeTab.expanded = make(map[string]bool)
	}
	var last string
	var walk func(n *tree.Node)
	walk = func(n *tree.Node) {
		if treeNodeExpandable(n) {
			last = n.Name
		}
		for _, c := range n.Children() {
			walk(c)
		}
	}
	for _, c := range t.treeView.Root().Children() {
		walk(c)
	}
	if last != "" {
		t.treeTab.expanded[last] = !t.treeTab.expanded[last]
	}
}

// treeDisplayLine returns the Tree pane's display line at the given
// content row. The tab recomputes its display with the same width and
// shade the pane renders with, so the line's text and column layout
// match what is on screen. Click path only — never per frame.
func (t *TUI) treeDisplayLine(row int, box taiui.Box) (taiui.Line, bool) {
	base := panelStyle.BaseBG
	if t.tabs.Focus == 1 {
		base = panelStyle.FocusBG
	}
	display := t.treeDisplay(treeContentWidth(t.tabs.Expanded[1], box.Width()), base)
	if row < 0 || row >= len(display) {
		return taiui.Line{}, false
	}
	return display[row], true
}

// treeControlRow is one rendered fold control of the status column:
// the node the control acts on and the absolute screen row of its
// glyph. See TheoryOfTreeTab.
type treeControlRow struct {
	name string
	row  int
}

// treeNodeControls returns the controls of a tree node. An expandable
// node (multi-line content) carries the fold toggle, its glyph
// following the node's expansion; a single-line node carries no
// control. See TheoryOfTreeTab.
func (t *TUI) treeNodeControls(n *tree.Node) []outputControl {
	if !treeNodeExpandable(n) {
		return nil
	}
	glyph := sectionGlyphExpanded
	if !t.treeTab.expanded[n.Name] {
		glyph = sectionGlyphCollapsed
	}
	return []outputControl{{Glyph: glyph}}
}

// treeControlRows computes the status column's rows for the current
// view: one row per expandable node with a visible display row,
// pinned to the node's first display row or the viewport top when
// that row scrolled above it. Nodes occupy disjoint row ranges, so
// two controls never pin to the same row. The caller holds t.mu. See
// TheoryOfTreeTab.
func (t *TUI) treeControlRows(box taiui.Box, display []taiui.Line, offset int) []treeControlRow {
	if t.treeView == nil {
		return nil
	}
	paneHeight := t.tuiPaneHeight(1, box)
	var rows []treeControlRow
	for _, r := range t.treeTab.rows {
		n, ok := t.treeView.Node(r.name)
		if !ok || !treeNodeExpandable(n) {
			continue
		}
		if r.endRow <= offset || r.startRow >= offset+paneHeight {
			continue
		}
		rows = append(rows, treeControlRow{
			name: r.name,
			row:  box.Top + 1 + (max(r.startRow, offset) - offset),
		})
	}
	return rows
}

// toggleTreeControlAtClick toggles the node whose fold control the
// press hit: the press must land on a rendered control row within the
// status column. Expanding a collapsed node scrolls the view to the
// node's first display row, mirroring the Output tab's control
// behavior. It reports whether a control consumed the press. The
// caller holds t.mu. See TheoryOfTreeTab.
func (t *TUI) toggleTreeControlAtClick(x, y int) bool {
	if !t.tabs.Expanded[1] || t.treeView == nil {
		return false
	}
	box := t.tabs.Boxes(t.width, t.height)[1]
	if box.Width() <= controlColumnWidth || box.Height() <= 0 {
		return false
	}
	if y < box.Top+1 || y >= box.Bottom {
		return false
	}
	if x < box.Left || x >= box.Left+controlColumnWidth {
		return false
	}
	display := wrappedDisplay(t, 1, box)
	if len(display) == 0 {
		return false
	}
	offset := taiui.ClampOffset(t.scrolls[1].Offset, len(display), t.tuiPaneHeight(1, box))
	for _, row := range t.treeControlRows(box, display, offset) {
		if row.row != y {
			continue
		}
		wasCollapsed := !t.treeTab.expanded[row.name]
		t.toggleTreeNodeByName(row.name)
		if wasCollapsed {
			t.scrollToTreeNode(row.name)
		}
		return true
	}
	return false
}

// scrollToTreeNode scrolls the Tree pane's view so the node's first
// display row lands at the top of the pane, stopping the live tail.
// The caller holds t.mu. See TheoryOfTreeTab.
func (t *TUI) scrollToTreeNode(name string) {
	box := t.tabs.Boxes(t.width, t.height)[1]
	if box.Width() <= 0 || box.Height() <= 0 {
		return
	}
	display := wrappedDisplay(t, 1, box)
	if len(display) == 0 {
		return
	}
	for _, r := range t.treeTab.rows {
		if r.name != name {
			continue
		}
		t.scrolls[1].Offset = taiui.ClampOffset(r.startRow, len(display), t.tuiPaneHeight(1, box))
		t.scrolls[1].Follow = false
		return
	}
}

// treeAtClick handles a left press in the Tree pane: a press on an
// attempt-start node's jump marker jumps the Output tab to that
// attempt's output section, and any other press on a node toggles its
// expansion. Presses outside the pane's content area are no-ops.
// Called with t.mu held. See TheoryOfTreeTab and
// TheoryOfTUIOutputSections.
func (t *TUI) treeAtClick(x, y int) {
	if !t.tabs.Expanded[1] {
		return
	}
	box := t.tabs.Boxes(t.width, t.height)[1]
	if x < box.Left || x >= box.Right || y <= box.Top || y >= box.Bottom {
		return
	}
	row := t.scrolls[1].Offset + (y - box.Top - 1)
	node := t.treeNodeAtRow(row)
	if node == nil {
		return
	}
	// Only the attempt-start node's jump marker jumps: the press must
	// land on the marker's own columns in the header's first display
	// row. The status column indents the content rows when it renders,
	// so the press column subtracts the column's width there. See
	// TheoryOfTUIOutputSections and TheoryOfTreeTab.
	pressCol := x - box.Left
	if box.Width() > controlColumnWidth {
		pressCol -= controlColumnWidth
	}
	if strings.HasPrefix(node.Name, "attempt-start") {
		if line, ok := t.treeDisplayLine(row, box); ok {
			start, end, hasMarker := markerColumnRange(line.Text, taiui.DisplayWidthOptions())
			if hasMarker && pressCol >= start && pressCol < end {
				t.showOutputSection(t.sectionOfTreeNode(node))
				return
			}
		}
	}
	t.toggleTreeNodeAtRow(row)
}

// sectionOfTreeNode resolves the output section an attempt-start node
// maps to: the section owned by the node's attempt number. -1 when the
// attempt produced no section. See TheoryOfTreeTab and
// TheoryOfTUIOutputSections.
func (t *TUI) sectionOfTreeNode(node *tree.Node) int {
	num, ok := attemptNumberOf(node.Content)
	if !ok {
		return -1
	}
	return t.eventSections[outputSectionOwner{attempt: num}]
}
