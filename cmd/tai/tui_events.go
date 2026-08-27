package main

import (
	"sort"
	"strings"

	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/taiui"
)

const TheoryOfEventTree = `
Events tab tree theory:
- The Events tab renders the pipeline event stream as a tree: every
  event carries Seq and Parent (pipeline.TheoryOfLoopEvents), a goal
  loop's loop-start event roots the loop's branch, an attempt nests
  under it, and the attempt's lifecycle events nest under its start.
- Nodes are stored as a forest keyed by the run's (loop, sequence)
  pair. A child whose parent has not arrived renders as a temporary
  root; the parent claims its orphans on arrival, so out-of-order
  arrival heals into the same tree as in-order arrival. Children are
  ordered by sequence number.
- Display order is a depth-first walk of the forest, independent of
  arrival order. Every display line is indented by one Han-character
  width (U+3000, two terminal columns) per depth: the node's lines are
  wrapped within the width left by the indent first, then the indent
  prefixes every wrapped line, so continuation lines keep the indent
  and no line overflows the pane.
- Consecutive events alternate the two log background shades by display
  order; every display line of one event shares one shade. Each node's
  wrapped lines are cached by width, depth, and shade, so a frame
  re-wraps only nodes that are new or repositioned.
- Follow-tail tracking is the pane's ScrollState: following sticks the
  view to the latest row, scrolling away stops it, and scrolling back
  to the latest row resumes it.
`

// eventSeqKey identifies one event across a session of goal loops: each
// loop run numbers its own events from 1, so the run's loop number and
// the event's sequence number together address one node. Events without
// a sequence number — goal verdicts, hand-constructed test events — are
// never indexed. See TheoryOfEventTree.
type eventSeqKey struct {
	loop int
	seq  int
}

// eventNode is one node of the Events tab's event tree: a pipeline
// event's rendered lines, its tree position, and its cached wrapped
// display lines. See TheoryOfEventTree.
type eventNode struct {
	loop      int
	seq       int
	parentSeq int
	lines     []taiui.Line
	children  []*eventNode

	cachedWidth int
	cachedDepth int
	cachedShade taiui.Color
	wrapped     []taiui.Line
}

// addEventNode files one rendered event into the tab's forest: under
// its parent when the parent has arrived, otherwise as a temporary root
// that the parent claims on arrival. Events without a sequence number
// are always roots. See TheoryOfEventTree.
func (t *TUI) addEventNode(ev pipeline.Event, lines []taiui.Line) {
	node := &eventNode{
		loop:      ev.Loop,
		seq:       ev.Seq,
		parentSeq: ev.Parent,
		lines:     lines,
	}
	if parent := t.eventBySeq[eventSeqKey{ev.Loop, ev.Parent}]; ev.Parent != 0 && parent != nil {
		parent.children = append(parent.children, node)
		sortChildren(parent)
	} else {
		t.eventRoots = append(t.eventRoots, node)
	}
	if ev.Seq != 0 {
		if t.eventBySeq == nil {
			t.eventBySeq = make(map[eventSeqKey]*eventNode)
		}
		t.eventBySeq[eventSeqKey{ev.Loop, ev.Seq}] = node
		// Claim the orphan roots whose intended parent is this node,
		// healing children that arrived before their parent.
		remaining := t.eventRoots[:0]
		for _, root := range t.eventRoots {
			if root != node && root.loop == ev.Loop && root.parentSeq == ev.Seq {
				node.children = append(node.children, root)
				sortChildren(node)
			} else {
				remaining = append(remaining, root)
			}
		}
		t.eventRoots = remaining
	}
}

// sortChildren orders a node's children by sequence number, so the
// display order is canonical however the events arrived.
func sortChildren(n *eventNode) {
	sort.Slice(n.children, func(i, j int) bool {
		return n.children[i].seq < n.children[j].seq
	})
}

// eventsDisplay walks the forest depth-first and returns the tab's
// display lines: each node's lines wrapped within the width left by its
// indent, shaded alternately by display order. Each node's wrapped
// lines are cached by width, depth, and shade, so a frame re-wraps only
// nodes that are new or repositioned. See TheoryOfEventTree.
func (t *TUI) eventsDisplay(contentWidth int, base taiui.Color) []taiui.Line {
	alt := taiui.AltBG(base)
	var out []taiui.Line
	index := 0
	var walk func(n *eventNode, depth int)
	walk = func(n *eventNode, depth int) {
		shade := base
		if index%2 == 1 {
			shade = alt
		}
		index++
		if n.cachedWidth != contentWidth || n.cachedDepth != depth || n.cachedShade != shade || n.wrapped == nil {
			n.wrapped = wrapEventNode(n, contentWidth, depth, shade)
			n.cachedWidth = contentWidth
			n.cachedDepth = depth
			n.cachedShade = shade
		}
		out = append(out, n.wrapped...)
		for _, child := range n.children {
			walk(child, depth+1)
		}
	}
	for _, root := range t.eventRoots {
		walk(root, 0)
	}
	return out
}

// wrapEventNode wraps one node's lines for display: each line is wrapped
// at the width left by the depth indent first, then the indent prefixes
// every wrapped display line, so wrapped continuation lines keep the
// indent. See TheoryOfEventTree.
func wrapEventNode(n *eventNode, contentWidth, depth int, shade taiui.Color) []taiui.Line {
	indent := strings.Repeat("\u3000", depth)
	wrapWidth := max(contentWidth-2*depth, 1)
	out := make([]taiui.Line, 0, len(n.lines))
	for _, line := range n.lines {
		wrapped := taiui.WrapLinesColored([]taiui.Line{{Text: line.Text, Color: line.Color}}, wrapWidth)
		for i := range wrapped {
			wrapped[i].Text = indent + wrapped[i].Text
			wrapped[i].BGColor = shade
		}
		out = append(out, wrapped...)
	}
	return out
}
