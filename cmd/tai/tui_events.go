package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/clipperhouse/displaywidth"
	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/taiui"
)

// eventIndentHanWidth is the Events tab's tree indent per depth, in
// Han characters: two U+3000 characters, four terminal columns.
const eventIndentHanWidth = 2

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
  arrival order. Every display line is indented by two Han-character
  widths (U+3000, four terminal columns) per depth: the node's lines
  are wrapped within the width left by the indent first, then the
  indent prefixes every wrapped line, so continuation lines keep the
  indent and no line overflows the pane.
- Every event carries an elapsed-time timer: the node records the
  duration from the TUI session's start (TUI.startTime) to the event's
  arrival, and the node's first display line right-aligns the stopwatch
  fragment ("+0:07", "+1:02:03") at the pane's right edge. The first
  source line wraps one timer-zone narrower, so a wrapped header never
  collides with the timer, and a pane too narrow for the timer omits
  it. The value is static per event — it marks when the event happened,
  not a live countdown — so the wrapped-line cache stays valid.
- Consecutive events alternate the two log background shades by display
  order; every display line of one event shares one shade. Each node's
  wrapped lines are cached by width, depth, and shade, so a frame
  re-wraps only nodes that are new or repositioned.
- Handoff summary events (pipeline.EventHandoff) collapse by default
  to their header plus an expand hint, so a long recovery note does
  not flood the tail view. Enter toggles the most recent handoff node,
  and a mouse press on a collapsed node's rows expands it; a press on
  an expanded node's header row collapses it, so clicking inside a
  long expanded summary never collapses it by accident.
`

// handoffRowRange maps one expandable handoff node onto its display row
// range in the Events tab's last-rendered display, so a mouse press can
// find the node under the cursor. endRow is exclusive. See
// TheoryOfEventTree.
type handoffRowRange struct {
	node     *eventNode
	startRow int
	endRow   int
}

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
// display lines. A handoff summary node is expandable and collapses to
// its header until the user expands it. See TheoryOfEventTree.
type eventNode struct {
	loop      int
	seq       int
	parentSeq int
	lines     []taiui.Line
	children  []*eventNode

	// expandable marks a handoff summary whose body hides until the
	// user expands it; expanded records the toggle state, collapsed
	// by default. See TheoryOfEventTree.
	expandable bool
	expanded   bool

	// elapsed is the duration from the TUI session's start to the
	// event's arrival, rendered as the right-aligned timer of the
	// node's first display line. See TheoryOfEventTree.
	elapsed time.Duration

	cachedWidth int
	cachedDepth int
	cachedShade taiui.Color
	wrapped     []taiui.Line
}

// displayLines returns the node's display lines: a collapsed handoff
// node shows only its header plus an expand hint; every other node,
// and an expanded handoff, shows all lines. See TheoryOfEventTree.
func (n *eventNode) displayLines() []taiui.Line {
	if !n.expandable || n.expanded {
		return n.lines
	}
	hint := taiui.Line{
		Text:  fmt.Sprintf("⤷ press Enter or click to expand (%d lines hidden)", len(n.lines)-1),
		Color: outputColorLogLine,
	}
	return []taiui.Line{n.lines[0], hint}
}

// addEventNode files one rendered event into the tab's forest: under
// its parent when the parent has arrived, otherwise as a temporary root
// that the parent claims on arrival. Events without a sequence number
// are always roots. A handoff summary with a body is expandable and
// collapses by default. Each node records the duration from the
// session's start to the event's arrival for the elapsed-time timer.
// See TheoryOfEventTree.
func (t *TUI) addEventNode(ev pipeline.Event, lines []taiui.Line) {
	node := &eventNode{
		loop:       ev.Loop,
		seq:        ev.Seq,
		parentSeq:  ev.Parent,
		lines:      lines,
		expandable: ev.Kind == pipeline.EventHandoff && len(lines) > 1,
		elapsed:    time.Since(t.startTime),
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
// nodes that are new or repositioned. The display-width options derive
// once per pass and thread into the node wrapping, so the environment
// is scanned once per frame. The display row ranges of the expandable
// handoff nodes are recorded for mouse hit-testing. See
// TheoryOfEventTree.
func (t *TUI) eventsDisplay(contentWidth int, base taiui.Color) []taiui.Line {
	alt := taiui.AltBG(base)
	options := taiui.DisplayWidthOptions()
	var out []taiui.Line
	t.handoffRows = t.handoffRows[:0]
	index := 0
	var walk func(n *eventNode, depth int)
	walk = func(n *eventNode, depth int) {
		shade := base
		if index%2 == 1 {
			shade = alt
		}
		index++
		if n.cachedWidth != contentWidth || n.cachedDepth != depth || n.cachedShade != shade || n.wrapped == nil {
			n.wrapped = wrapEventNode(n, contentWidth, depth, shade, options)
			n.cachedWidth = contentWidth
			n.cachedDepth = depth
			n.cachedShade = shade
		}
		start := len(out)
		out = append(out, n.wrapped...)
		if n.expandable {
			t.handoffRows = append(t.handoffRows, handoffRowRange{node: n, startRow: start, endRow: len(out)})
		}
		for _, child := range n.children {
			walk(child, depth+1)
		}
	}
	for _, root := range t.eventRoots {
		walk(root, 0)
	}
	return out
}

// formatElapsed renders an elapsed duration as a stopwatch fragment:
// "+0:07" under an hour, "+1:02:03" beyond it. It is the right-aligned
// timer suffix of every Events-tab display line. See TheoryOfEventTree.
func formatElapsed(d time.Duration) string {
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

// wrapEventNode wraps one node's lines for display: each line is wrapped
// at the width left by the depth indent first, then the indent prefixes
// every wrapped display line, so wrapped continuation lines keep the
// indent. The node's elapsed time right-aligns at the pane's right edge
// of the first display line: the first source line wraps one timer-zone
// narrower and the residual columns are padded with spaces, so a
// wrapped header never collides with the timer. A collapsed handoff
// node contributes only its header and the expand hint. See
// TheoryOfEventTree.
func wrapEventNode(n *eventNode, contentWidth, depth int, shade taiui.Color, options displaywidth.Options) []taiui.Line {
	indent := strings.Repeat("\u3000", eventIndentHanWidth*depth)
	wrapWidth := max(contentWidth-2*eventIndentHanWidth*depth, 1)
	timerText := formatElapsed(n.elapsed)
	timerWidth := options.String(timerText)
	timerZone := timerWidth + 1
	display := n.displayLines()
	out := make([]taiui.Line, 0, len(display))
	for i, line := range display {
		wrapAt := wrapWidth
		withTimer := i == 0 && wrapWidth > timerZone
		if withTimer {
			wrapAt = wrapWidth - timerZone
		}
		wrapped := taiui.WrapLinesColored([]taiui.Line{{Text: line.Text, Color: line.Color}}, wrapAt)
		if withTimer && len(wrapped) > 0 {
			pad := wrapWidth - timerWidth - options.String(wrapped[0].Text)
			if pad < 1 {
				pad = 1
			}
			wrapped[0].Text += strings.Repeat(" ", pad) + timerText
		}
		for j := range wrapped {
			wrapped[j].Text = indent + wrapped[j].Text
			wrapped[j].BGColor = shade
		}
		out = append(out, wrapped...)
	}
	return out
}
