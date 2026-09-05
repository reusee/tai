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
  branches and a fresh run's nodes sit directly under the root.
- The tab renders a projection of the tree, cycled with the v key:
  all shows every node; events shows the event nodes; summary the
  summary nodes; model, program, and user the nodes of that author.
  The projection keeps each shown node's ancestors (tree.Extract), so
  the outline stays readable.
- Every node renders one line by default: "name [type/author] first
  content line". A node whose content spans more than one line is
  expandable and collapsed by default; a press on any of its rows — or
  Enter for the last expandable node — reveals the remaining lines,
  and a press on an expanded node's header rows folds it, so clicking
  inside a long expanded body never collapses it by accident.
- Node lines are colored by author and type: event and error nodes in
  the log color, user inputs in the user color, program nodes in the
  system color, model nodes in the default foreground. The tab
  alternates the two background shades per node: all display lines of
  one node share one shade, and consecutive nodes alternate.
- Each node's first display line right-aligns the elapsed timer
  ("+0:07") from the session start to the node's insert time; a header
  too narrow for the timer omits it, and a wrapped header never
  collides with the timer.
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

// treeCached caches one node's wrapped display lines keyed by the
// render parameters, so a frame re-wraps only nodes that are new or
// repositioned. See TheoryOfTreeTab.
type treeCached struct {
	width      int
	depth      int
	shade      taiui.Color
	expanded   bool
	headerRows int
	lines      []taiui.Line
}

// treeRowRange maps one rendered node onto its display row range as
// recorded by the last treeDisplay, so a pointer press locates the
// node under the cursor. endRow is exclusive; headerRows counts the
// wrapped header lines, the rows a press folds an expanded node
// through. See TheoryOfTreeTab.
type treeRowRange struct {
	name       string
	startRow   int
	endRow     int
	headerRows int
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

// treeHeaderText renders the node's header text: the name, the type
// and author, the first content line, the expand hint on a collapsed
// multi-line node, and the jump marker on an attempt-start node. See
// TheoryOfTreeTab.
func treeHeaderText(n *tree.Node, expanded bool) string {
	header := fmt.Sprintf("%s [%s/%s]", n.Name, n.Type, n.Author)
	if first := tree.PreviewRunes(n.Content, 0); first != "" {
		header += " " + first
	}
	if body := treeBodyLines(n); len(body) > 0 && !expanded {
		header += fmt.Sprintf(" ⤷ %d more lines", len(body))
	}
	if strings.HasPrefix(n.Name, "attempt-start") {
		header += " " + eventJumpMarker
	}
	return header
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

// wrapTreeNode wraps one node's header and body for display: each line
// wraps at the width left by the depth indent first, then the indent
// prefixes every wrapped display line. The elapsed timer right-aligns
// on the header's first display line: the header wraps one timer-zone
// narrower and the residual columns are padded with spaces, so a
// wrapped header never collides with the timer; a pane too narrow for
// the timer omits it. The returned headerRows counts the wrapped
// header lines. See TheoryOfTreeTab.
func wrapTreeNode(header string, body []string, elapsed time.Duration, depth int, shade taiui.Color, contentWidth int, options displaywidth.Options, color taiui.Color) ([]taiui.Line, int) {
	indent := strings.Repeat(" ", treeIndentWidth*depth)
	wrapWidth := max(contentWidth-treeIndentWidth*depth, 1)
	timerText := formatTreeElapsed(elapsed)
	timerWidth := options.String(timerText)
	timerZone := timerWidth + 1

	wrapAt := wrapWidth
	withTimer := wrapWidth > timerZone
	if withTimer {
		wrapAt = wrapWidth - timerZone
	}
	wrappedHeader := taiui.WrapLinesColored([]taiui.Line{{Text: header, Color: color}}, wrapAt)
	if withTimer && len(wrappedHeader) > 0 {
		pad := wrapWidth - timerWidth - options.String(wrappedHeader[0].Text)
		if pad < 1 {
			pad = 1
		}
		wrappedHeader[0].Text += strings.Repeat(" ", pad) + timerText
	}
	for i := range wrappedHeader {
		wrappedHeader[i].Text = indent + wrappedHeader[i].Text
		wrappedHeader[i].BGColor = shade
	}
	out := wrappedHeader
	for _, line := range body {
		wrapped := taiui.WrapLinesColored([]taiui.Line{{Text: line, Color: color}}, wrapWidth)
		for j := range wrapped {
			wrapped[j].Text = indent + wrapped[j].Text
			wrapped[j].BGColor = shade
		}
		out = append(out, wrapped...)
	}
	return out, len(wrappedHeader)
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

// treeTabLabel renders the Tree tab's label with the current
// projection. See TheoryOfTreeTab.
func (t *TUI) treeTabLabel() string {
	return "Tree (" + t.treeTab.mode.label() + ")"
}

// treeDisplay renders the Tree tab's display: the depth-first walk of
// the current projection, each node contributing its wrapped header
// line plus its body lines when expanded. The walk records every
// node's row range, so a pointer press maps onto the node the way the
// rows render. The caller holds t.mu. See TheoryOfTreeTab.
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
		lines, headerRows := t.treeNodeLines(n, depth, shade, contentWidth, options)
		start := len(out)
		out = append(out, lines...)
		t.treeTab.rows = append(t.treeTab.rows, treeRowRange{
			name: n.Name, startRow: start, endRow: len(out), headerRows: headerRows,
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

// treeNodeLines renders one node's display lines, cached per width,
// depth, shade, and expansion, so a frame re-wraps only nodes that are
// new or repositioned. The caller holds t.mu. See TheoryOfTreeTab.
func (t *TUI) treeNodeLines(n *tree.Node, depth int, shade taiui.Color, contentWidth int, options displaywidth.Options) ([]taiui.Line, int) {
	expanded := t.treeTab.expanded[n.Name]
	if c, ok := t.treeTab.cache[n.Name]; ok &&
		c.width == contentWidth && c.depth == depth && c.shade == shade && c.expanded == expanded {
		return c.lines, c.headerRows
	}
	elapsed := time.Since(t.startTime)
	if !n.InsertTime.After(t.startTime) {
		elapsed = 0
	} else {
		elapsed = n.InsertTime.Sub(t.startTime)
	}
	body := treeBodyLines(n)
	if !expanded {
		body = nil
	}
	lines, headerRows := wrapTreeNode(
		treeHeaderText(n, expanded), body, elapsed, depth, shade, contentWidth, options, treeLineColor(n))
	if t.treeTab.cache == nil {
		t.treeTab.cache = make(map[string]treeCached)
	}
	t.treeTab.cache[n.Name] = treeCached{
		width: contentWidth, depth: depth, shade: shade, expanded: expanded,
		headerRows: headerRows, lines: lines,
	}
	return lines, headerRows
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
// it, and a header row of an expanded node collapses it, so clicking
// inside a long expanded body never collapses it by accident. The
// expansion map is lazily initialized, because a click can arrive
// before any setTree call initialized it. See TheoryOfTreeTab.
func (t *TUI) toggleTreeNodeAtRow(row int) {
	if t.treeTab.expanded == nil {
		t.treeTab.expanded = make(map[string]bool)
	}
	for _, r := range t.treeTab.rows {
		if row < r.startRow || row >= r.endRow {
			continue
		}
		if t.treeTab.expanded[r.name] && row >= r.startRow+r.headerRows {
			// A body row of an expanded node: inert, so reading inside
			// an expanded body never folds it.
			return
		}
		t.treeTab.expanded[r.name] = !t.treeTab.expanded[r.name]
		return
	}
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

// treeDisplayLine returns the Tree pane's wrapped display line at the
// given content row. The tab recomputes its display with the same
// width and shade the pane renders with, so the line's text and column
// layout match what is on screen. Click path only — never per frame.
func (t *TUI) treeDisplayLine(row int, box taiui.Box) (taiui.Line, bool) {
	contentWidth := max(box.Width()-1, 1)
	base := panelStyle.BaseBG
	if t.tabs.Focus == 1 {
		base = panelStyle.FocusBG
	}
	display := t.treeDisplay(contentWidth, base)
	if row < 0 || row >= len(display) {
		return taiui.Line{}, false
	}
	return display[row], true
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
	// row. See TheoryOfTUIOutputSections.
	if strings.HasPrefix(node.Name, "attempt-start") {
		if line, ok := t.treeDisplayLine(row, box); ok {
			start, end, hasMarker := markerColumnRange(line.Text, taiui.DisplayWidthOptions())
			pressCol := x - box.Left
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
