package taiui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/clipperhouse/displaywidth"
)

// eventIndentHanWidth is the event tree's indent per depth, in Han
// characters: two U+3000 characters, four terminal columns.
const eventIndentHanWidth = 2

const TheoryOfEventTree = `
taiui event tree theory:
- An EventTree renders a stream of tree-structured events as display
  lines: nodes carry a (run, sequence, parent) identity, a parentless
  node roots a branch, and children nest under their parent.
- Nodes are stored as a forest keyed by the run's (run, sequence)
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
- Every node may carry an elapsed-time duration: the node's first
  display line right-aligns the stopwatch fragment ("+0:07",
  "+1:02:03") at the pane's right edge. The first source line wraps
  one timer-zone narrower, so a wrapped header never collides with the
  timer, and a pane too narrow for the timer omits it. The value is
  static per node — it marks when the node happened, not a live
  countdown — so the wrapped-line cache stays valid.
- Consecutive nodes alternate the two log background shades by display
  order; every display line of one node shares one shade. Each node's
  wrapped lines are cached by width, depth, and shade, so a frame
  re-wraps only nodes that are new or repositioned.
- An expandable node collapses by default to its header plus an expand
  hint, so a long body does not flood the tail view. ToggleLastExpanded
  toggles the last expandable node in display order, for keyboard
  access without a cursor. Display records the display row range of
  every expandable node so ToggleAtRow can map a pointer press onto a
  node: any row of a collapsed node expands it; the header row of an
  expanded node collapses it, so clicking inside a long expanded body
  never collapses it by accident.
`

// eventRowRange maps one expandable node onto its display row range in
// the last-rendered display, so a pointer press can find the node under
// the cursor. endRow is exclusive. See TheoryOfEventTree.
type eventRowRange struct {
	node     *EventNode
	startRow int
	endRow   int
}

// eventSeqKey identifies one node across several independently numbered
// runs: each run numbers its own events from 1, so the run number and
// the sequence number together address one node. Nodes without a
// sequence number are never indexed. See TheoryOfEventTree.
type eventSeqKey struct {
	run int
	seq int
}

// EventNode is one node of an event tree: the node's rendered lines,
// its tree position, and its cached wrapped display lines. An
// expandable node collapses to its header until the user expands it.
// See TheoryOfEventTree.
type EventNode struct {
	// Run and Seq identify the node within its stream; ParentSeq is the
	// sequence number of the node's parent, zero for a root.
	Run       int
	Seq       int
	ParentSeq int
	Lines     []Line

	// Children holds the nodes filed under this one, ordered by
	// sequence number. See TheoryOfEventTree.
	Children []*EventNode

	// Expandable marks a node whose body hides until the user expands
	// it; Expanded records the toggle state, collapsed by default.
	// See TheoryOfEventTree.
	Expandable bool
	Expanded   bool

	// Elapsed is rendered as the right-aligned stopwatch fragment on
	// the node's first display line. See TheoryOfEventTree.
	Elapsed time.Duration

	cachedWidth int
	cachedDepth int
	cachedShade Color
	wrapped     []Line
}

// displayLines returns the node's display lines: a collapsed expandable
// node shows only its header plus an expand hint; every other node, and
// an expanded one, shows all lines. See TheoryOfEventTree.
func (n *EventNode) displayLines(hintColor Color) []Line {
	if !n.Expandable || n.Expanded {
		return n.Lines
	}
	hint := Line{
		Text:  fmt.Sprintf("⤷ click to expand (%d lines hidden)", len(n.Lines)-1),
		Color: hintColor,
	}
	return []Line{n.Lines[0], hint}
}

// EventTree is the event forest of an event-stream view: nodes filed by
// (run, sequence) identity, rendered depth-first with cached wrapping.
// Its zero value is empty and ready for use. The caller serializes
// access (typically a single-threaded render loop); the tree performs
// no locking of its own. See TheoryOfEventTree.
type EventTree struct {
	// Roots holds the top nodes of the forest in arrival order.
	Roots []*EventNode

	// HintColor styles the expand hint of a collapsed expandable node.
	HintColor Color

	bySeq      map[eventSeqKey]*EventNode
	expandRows []eventRowRange
}

// Add files one node into the forest: under its parent when the parent
// has arrived, otherwise as a temporary root that the parent claims on
// arrival. A node without a sequence number is always a root and never
// the target of orphan claiming. Add returns the filed node. See
// TheoryOfEventTree.
func (t *EventTree) Add(node EventNode) *EventNode {
	n := &node
	if parent := t.bySeq[eventSeqKey{n.Run, n.ParentSeq}]; n.ParentSeq != 0 && parent != nil {
		parent.Children = append(parent.Children, n)
		sortChildren(parent)
	} else {
		t.Roots = append(t.Roots, n)
	}
	if n.Seq != 0 {
		if t.bySeq == nil {
			t.bySeq = make(map[eventSeqKey]*EventNode)
		}
		t.bySeq[eventSeqKey{n.Run, n.Seq}] = n
		// Claim the orphan roots whose intended parent is this node,
		// healing children that arrived before their parent.
		remaining := t.Roots[:0]
		for _, root := range t.Roots {
			if root != n && root.Run == n.Run && root.ParentSeq == n.Seq {
				n.Children = append(n.Children, root)
				sortChildren(n)
			} else {
				remaining = append(remaining, root)
			}
		}
		t.Roots = remaining
	}
	return n
}

// sortChildren orders a node's children by sequence number, so the
// display order is canonical however the nodes arrived.
func sortChildren(n *EventNode) {
	sort.Slice(n.Children, func(i, j int) bool {
		return n.Children[i].Seq < n.Children[j].Seq
	})
}

// Display walks the forest depth-first and returns the view's display
// lines: each node's lines wrapped within the width left by its
// indent, shaded alternately by display order. Each node's wrapped
// lines are cached by width, depth, and shade, so a frame re-wraps only
// nodes that are new or repositioned. The display-width options derive
// once per pass and thread into the node wrapping, so the environment
// is scanned once per frame. The display row ranges of the expandable
// nodes are recorded for pointer hit-testing (ToggleAtRow). See
// TheoryOfEventTree.
func (t *EventTree) Display(contentWidth int, base Color) []Line {
	alt := AltBG(base)
	options := DisplayWidthOptions()
	var out []Line
	t.expandRows = t.expandRows[:0]
	index := 0
	var walk func(n *EventNode, depth int)
	walk = func(n *EventNode, depth int) {
		shade := base
		if index%2 == 1 {
			shade = alt
		}
		index++
		if n.cachedWidth != contentWidth || n.cachedDepth != depth || n.cachedShade != shade || n.wrapped == nil {
			n.wrapped = wrapEventNode(n, t.HintColor, contentWidth, depth, shade, options)
			n.cachedWidth = contentWidth
			n.cachedDepth = depth
			n.cachedShade = shade
		}
		start := len(out)
		out = append(out, n.wrapped...)
		if n.Expandable {
			t.expandRows = append(t.expandRows, eventRowRange{node: n, startRow: start, endRow: len(out)})
		}
		for _, child := range n.Children {
			walk(child, depth+1)
		}
	}
	for _, root := range t.Roots {
		walk(root, 0)
	}
	return out
}

// ToggleLastExpanded toggles the expandable node displayed last in
// depth-first order, so a keyboard command works on the most recent
// expandable node without a cursor. See TheoryOfEventTree.
func (t *EventTree) ToggleLastExpanded() {
	var target *EventNode
	var walk func(n *EventNode)
	walk = func(n *EventNode) {
		if n.Expandable {
			target = n
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	for _, root := range t.Roots {
		walk(root)
	}
	if target == nil {
		return
	}
	target.Expanded = !target.Expanded
	target.wrapped = nil
}

// ToggleAtRow toggles the expandable node whose recorded display row
// range contains row: any row of a collapsed node expands it, and the
// header row of an expanded node collapses it, so clicking inside a
// long expanded body never collapses it by accident. Row ranges are
// recorded by the last Display; presses outside every range are no-ops.
// See TheoryOfEventTree.
func (t *EventTree) ToggleAtRow(row int) {
	for _, r := range t.expandRows {
		if row < r.startRow || row >= r.endRow {
			continue
		}
		n := r.node
		if !n.Expanded || row == r.startRow {
			n.Expanded = !n.Expanded
			n.wrapped = nil
		}
		return
	}
}

// formatElapsed renders an elapsed duration as a stopwatch fragment:
// "+0:07" under an hour, "+1:02:03" beyond it. It is the right-aligned
// timer suffix of a node's first display line. See TheoryOfEventTree.
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

// wrapEventNode wraps one node's lines for display: each line is
// wrapped at the width left by the depth indent first, then the indent
// prefixes every wrapped display line, so wrapped continuation lines
// keep the indent. The node's elapsed time right-aligns at the pane's
// right edge of the first display line: the first source line wraps
// one timer-zone narrower and the residual columns are padded with
// spaces, so a wrapped header never collides with the timer. A
// collapsed expandable node contributes only its header and the expand
// hint. See TheoryOfEventTree.
func wrapEventNode(n *EventNode, hintColor Color, contentWidth, depth int, shade Color, options displaywidth.Options) []Line {
	indent := strings.Repeat("　", eventIndentHanWidth*depth)
	wrapWidth := max(contentWidth-2*eventIndentHanWidth*depth, 1)
	timerText := formatElapsed(n.Elapsed)
	timerWidth := options.String(timerText)
	timerZone := timerWidth + 1
	display := n.displayLines(hintColor)
	out := make([]Line, 0, len(display))
	for i, line := range display {
		wrapAt := wrapWidth
		withTimer := i == 0 && wrapWidth > timerZone
		if withTimer {
			wrapAt = wrapWidth - timerZone
		}
		wrapped := WrapLinesColored([]Line{{Text: line.Text, Color: line.Color}}, wrapAt)
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
