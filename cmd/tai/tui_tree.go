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
  content line". A node is expandable when its content spans more
  than one line, or when its one-line header truncates at the pane
  width; a press on any of its rows — or Enter for the last multi-line
  node — reveals the full content, and a press on an expanded node's
  header rows folds it, so clicking inside a long expanded body never
  collapses it by accident. When expanded, the content starts on the
  row below the header: the expanded header drops the inline preview
  and carries only the structural columns, and the body renders the
  full content, first line included, wrapped at the pane width instead
  of truncating — breaking at space runs and hard-breaking
  unbreakable runs at cluster boundaries. The collapsed header stays
  the one-row index form, truncating at the pane edge; its ⤷ hint
  marks multi-line content, and a truncated single-line node signals
  its hidden content through the status column instead. Expandability
  always measures the collapsed form — the index row whose truncation
  hides content — because the expanded header carries no content.
- Headers align as columns per indent level: the node name pads to
  the level's widest visible name and the [type/author] fragment to
  the level's widest visible meta, so entries of one level read as
  aligned columns; the pad widths derive from the projected entries.
- The expanded tab reserves a status column at its left edge, one Han
  character wide, like the Output tab's control column: every
  expandable node carries the fold glyph ▾ while expanded and ▸ while
  collapsed, rendered beside the node's first display row and clamped
  into the viewport. A press on the control's cells toggles the node;
  a node that is neither multi-line nor truncated — single-line
  content fitting the pane — carries no control.
- Every expansion of a node — a press on its rows, a press on its
  fold control, or Enter on the last multi-line node — scrolls the
  view to the node's first display row, so the expanded content opens
  at its beginning. The scroll lives in toggleTreeNodeByName, the one
  method every expansion path routes through, so the invariant cannot
  regress; collapsing never scrolls.
- The c key, pressed while the Tree tab holds the focus, folds the
  nodes the way the Output tab's c key folds its sections (see
  TheoryOfOutputControls): the first press snapshots the expanded
  nodes, folds every node to its one-line header, and scrolls the view
  to the top, so the collapsed list starts at the row below the title
  and doubles as the outline index; the next press restores the
  snapshotted expansions, and an expanded node's content renders from
  the row below its header. A manual expand breaks the all-collapsed
  state, so the next press folds and re-snapshots again. Nodes that
  arrive after the snapshot keep the default collapsed form on
  restore. Every other focus folds the Output tab's sections.
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
// repositioned; the alignment widths and the expandability derived
// from them join the cached state, because a wider entry at the same
// indent level re-pads every cached sibling. See TheoryOfTreeTab.
type treeCached struct {
	width      int
	depth      int
	shade      taiui.Color
	expanded   bool
	nameWidth  int
	metaWidth  int
	expandable bool
	lines      []taiui.Line
}

// treeRowRange maps one rendered node onto its display row range as
// recorded by the last treeDisplay, so a pointer press locates the
// node under the cursor. endRow is exclusive; the header is the
// first row, the row a press folds an expanded node through.
// Expandable records the node's expandability at the rendering width:
// multi-line content, or a header truncated at that width, so the
// status column and the press paths share one fact. See
// TheoryOfTreeTab.
type treeRowRange struct {
	name       string
	startRow   int
	endRow     int
	expandable bool
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
// consumed for the display signals, the wrapped-line cache, the row
// ranges of the last display, and the c-key fold snapshot. Guarded by
// t.mu. See TheoryOfTreeTab.
type treeTabState struct {
	mode     treeViewMode
	expanded map[string]bool
	seen     map[string]bool
	cache    map[string]treeCached
	rows     []treeRowRange
	// collapseAllSaved carries the nodes the last c-key fold had
	// expanded, so pressing the key again while every node is
	// collapsed restores them. Nil until the first fold. See
	// TheoryOfTreeTab.
	collapseAllSaved map[string]bool
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

// treeFirstLine returns the node's first content line, with trailing
// newlines stripped; empty content yields "". The expanded body starts
// from this line, so the content begins on the row below the header.
// See TheoryOfTreeTab.
func treeFirstLine(n *tree.Node) string {
	content := strings.TrimRight(n.Content, "\n")
	if content == "" {
		return ""
	}
	if i := strings.IndexByte(content, '\n'); i >= 0 {
		return content[:i]
	}
	return content
}

// treeNodeExpandable reports whether the node hides a multi-line body
// behind its one-line header. See TheoryOfTreeTab.
func treeNodeExpandable(n *tree.Node) bool {
	return len(treeBodyLines(n)) > 0
}

// treeHeaderText renders the node's header text: the name padded to
// the level's widest visible name, the [type/author] fragment padded
// to the level's widest visible meta, and — collapsed only — the
// first content line as the index preview with the ⤷ more-lines hint
// on multi-line content. The expanded header drops the preview, so
// the content starts on the row below the header; the jump marker on
// an attempt-start node stays on the header. The pads align the
// columns of the same indent level. See TheoryOfTreeTab.
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
	if !expanded {
		b.WriteString(" ")
		if first := tree.PreviewRunes(n.Content, 0); first != "" {
			b.WriteString(first + " ")
		}
		if body := treeBodyLines(n); len(body) > 0 {
			b.WriteString(fmt.Sprintf("⤷ %d more lines", len(body)))
		}
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
// its wrapped body lines when expanded. The walk starts at the tree
// root, so every goal loop's nodes render. The walk records every
// node's row range together with the node's expandability at the
// rendering width, so a pointer press maps onto the node the way the
// rows render and the status column reads one fact. The caller holds
// t.mu. See TheoryOfTreeTab.
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
		lines, expandable := t.treeNodeLines(n, depth, shade, contentWidth, options, align)
		start := len(out)
		out = append(out, lines...)
		t.treeTab.rows = append(t.treeTab.rows, treeRowRange{
			name: n.Name, startRow: start, endRow: len(out), expandable: expandable,
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

// treeNodeLines renders one node's display lines: the one-row header
// and, when expanded, the full content starting on the row below the
// header. The collapsed header carries the first content line as its
// preview; the expanded header drops the preview, so the content —
// first line included — always begins on the next row. Body lines wrap
// at the pane width instead of truncating. Wrapped rows keep the
// node's depth indent. The second result reports the node's
// expandability at this width: multi-line content, or a collapsed
// header truncated on non-empty content. Lines are cached per width,
// depth, shade, expansion, and alignment, so a frame re-renders only
// nodes that are new or repositioned. The caller holds t.mu. See
// TheoryOfTreeTab.
func (t *TUI) treeNodeLines(n *tree.Node, depth int, shade taiui.Color, contentWidth int, options displaywidth.Options, align treeAlignments) ([]taiui.Line, bool) {
	expanded := t.treeTab.expanded[n.Name]
	nameWidth := align.nameWidths[depth]
	metaWidth := align.metaWidths[depth]
	if c, ok := t.treeTab.cache[n.Name]; ok &&
		c.width == contentWidth && c.depth == depth && c.shade == shade && c.expanded == expanded &&
		c.nameWidth == nameWidth && c.metaWidth == metaWidth {
		return c.lines, c.expandable
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
	collapsedHeader := header
	if expanded {
		collapsedHeader = treeHeaderText(n, false, options, nameWidth, metaWidth)
	}
	first := treeFirstLine(n)
	bodyBase := treeBodyLines(n)
	// Expandability measures the collapsed one-line form at the width
	// the collapsed row renders into: the expanded header carries no
	// content, so its own truncation says nothing about hidden
	// content. See TheoryOfTreeTab.
	measureWidth := wrapWidth
	if wrapWidth > timerZone {
		measureWidth = wrapWidth - timerZone
	}
	expandable := treeNodeExpandable(n) ||
		(options.String(collapsedHeader) > measureWidth && first != "")
	var body []string
	if expanded && first != "" {
		// The content starts on the row below the header: the body
		// carries the full content, first line included. See
		// TheoryOfTreeTab.
		body = append(body, first)
		body = append(body, bodyBase...)
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
		// Body lines wrap at the pane width instead of truncating, so
		// the expanded display carries the full content.
		wrapped := taiui.WrapLinesColored([]taiui.Line{{Text: line, Color: color, BGColor: shade}}, wrapWidth)
		for _, w := range wrapped {
			lines = append(lines, taiui.Line{
				Text:    indent + w.Text,
				Color:   w.Color,
				BGColor: w.BGColor,
			})
		}
	}
	if t.treeTab.cache == nil {
		t.treeTab.cache = make(map[string]treeCached)
	}
	t.treeTab.cache[n.Name] = treeCached{
		width: contentWidth, depth: depth, shade: shade, expanded: expanded,
		nameWidth: nameWidth, metaWidth: metaWidth, expandable: expandable, lines: lines,
	}
	return lines, expandable
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
// call initialized it. Expanding the node scrolls the view to its
// first display row, so the expanded content opens at its beginning;
// every expansion path routes through this method, so the invariant
// cannot regress. Collapsing never scrolls. The caller holds t.mu.
// See TheoryOfTreeTab.
func (t *TUI) toggleTreeNodeByName(name string) {
	if t.treeTab.expanded == nil {
		t.treeTab.expanded = make(map[string]bool)
	}
	wasExpanded := t.treeTab.expanded[name]
	t.treeTab.expanded[name] = !wasExpanded
	if !wasExpanded {
		t.scrollToTreeNode(name)
	}
}

// toggleLastTreeExpandable expands or collapses the last expandable
// node of the session tree in depth-first order — typically the latest
// handoff or completion summary — so Enter works on the most recent
// collapsed body without a cursor. The flip routes through
// toggleTreeNodeByName, so expanding scrolls the view to the node's
// first display row. See TheoryOfTreeTab.
func (t *TUI) toggleLastTreeExpandable() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.treeView == nil {
		return
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
		t.toggleTreeNodeByName(last)
	}
}

// collapseAllTreeNodes toggles the c key's fold of the tree nodes,
// mirroring collapseAllSections's two states: when not every node is
// collapsed, it snapshots the expanded nodes, folds every node to its
// one-line header, and scrolls the view to the top, so the collapsed
// header list starts at the row below the title; when every node is
// collapsed, it restores the nodes the last fold had expanded. Nodes
// that arrive after the snapshot keep the default collapsed form on
// restore. A manual expand breaks the all-collapsed state, so the next
// press folds and re-snapshots rather than restoring. See
// TheoryOfTreeTab and TheoryOfOutputControls.
func (t *TUI) collapseAllTreeNodes() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.treeView == nil {
		return
	}
	expanded := make(map[string]bool)
	allCollapsed := true
	var walk func(n *tree.Node)
	walk = func(n *tree.Node) {
		// The expansion map is the authority, not the multi-line
		// predicate: a node whose one-line header truncates at the
		// pane width is expandable in the display too, and a manual
		// expand of it must break the all-collapsed state like any
		// other expand.
		if t.treeTab.expanded[n.Name] {
			expanded[n.Name] = true
			allCollapsed = false
		}
		for _, c := range n.Children() {
			walk(c)
		}
	}
	for _, c := range t.treeView.Root().Children() {
		walk(c)
	}
	if allCollapsed && t.treeTab.collapseAllSaved != nil {
		// Restore the nodes the last fold had expanded. The snapshot
		// carries only expanded entries, so nodes that arrived after
		// it keep the default collapsed form.
		t.treeTab.expanded = make(map[string]bool)
		for name := range t.treeTab.collapseAllSaved {
			t.treeTab.expanded[name] = true
		}
		return
	}
	// Fold branch: snapshot the currently expanded nodes, fold
	// everything, and scroll the view to the top, so the collapsed
	// header list starts at the row below the title.
	t.treeTab.collapseAllSaved = expanded
	if len(expanded) > 0 {
		t.treeTab.expanded = make(map[string]bool)
		box := t.tabs.Boxes(t.width, t.height)[1]
		if box.Width() > 0 && box.Height() > 0 {
			t.scrolls[1].Offset = 0
			t.scrolls[1].Follow = false
		}
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

// treeFoldGlyph returns the status column's fold glyph of a node: ▾
// while expanded, ▸ while collapsed. See TheoryOfTreeTab.
func treeFoldGlyph(expanded bool) string {
	if expanded {
		return sectionGlyphExpanded
	}
	return sectionGlyphCollapsed
}

// treeControlRows computes the status column's rows for the current
// view: one row per expandable node with a visible display row,
// pinned to the node's first display row or the viewport top when
// that row scrolled above it. Expandability is the node's recorded
// row-range fact: multi-line content, or a header truncated at the
// rendering width. Nodes occupy disjoint row ranges, so two controls
// never pin to the same row. The caller holds t.mu. See
// TheoryOfTreeTab.
func (t *TUI) treeControlRows(box taiui.Box, display []taiui.Line, offset int) []treeControlRow {
	if t.treeView == nil {
		return nil
	}
	paneHeight := t.tuiPaneHeight(1, box)
	var rows []treeControlRow
	for _, r := range t.treeTab.rows {
		if !r.expandable {
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
		t.toggleTreeNodeByName(row.name)
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
