package main

import (
	"strings"

	"github.com/clipperhouse/displaywidth"
	"github.com/reusee/tai/taiui"
)

const TheoryOfTUIOutputSections = `
Output tab sections and event-to-output navigation theory (cmd/tai):

- The Output tab's content stream is organized into sections. A section
  break happens where the output switches role or between thinking and
  non-thinking content — the existing blank-line separator points — and
  where an attempt starts: a pendingOwner set from the run's
  EventAttemptStart is consumed by the next visible content part, which
  then opens a section owned by that event, so consecutive attempts
  sharing one role still split into addressable sections. A section
  records the source-line index where it starts in the output buffer,
  so a section is an append-only slice of the stream.

- The attempt-start line carries the 👉 jump marker (eventJumpMarker):
  a left press on the marker's cells jumps the Output tab's view to
  the section the attempt wrote — the only Events-tab press that
  jumps. The press maps onto an event node through
  taiui.EventTree.NodeAtRow, and only an attempt-start node — its
  header ends with the marker — is eligible; the marker's cell range
  is then located in the pane's wrapped display line, measured cluster
  by cluster with the same width options the renderer uses, so the
  press must land on the marker's own columns. Presses on other rows,
  other columns, rows without a node, and attempt starts whose attempt
  produced no visible output are no-ops. The node or its nearest
  section-owning ancestor selects the section. Mirroring
  jumpToTransition, the jump expands and focuses the Output tab when
  needed and stops following the tail; the live tail resumes only
  when the view reaches the latest row. The display geometry is
  recomputed on the click path only, not per frame.

- The scroll target is the display line where the section's first
  source line begins: the source lines before the section start are
  wrapped with the same wrapping the Output pane's display uses, so the
  offset indexes into the wrapped display identically. The wrap runs on
  the click path only, not per frame.
`

// outputSection is one section of the Output tab's content: the index
// in the output line buffer at which the section's first source line
// begins. The section spans to the next section's start.
type outputSection struct {
	startLine int
}

// outputSectionOwner identifies the pipeline event that owns a
// section: the event's (run, sequence) identity, the same identity the
// Events tab files nodes by.
type outputSectionOwner struct {
	run int
	seq int
}

// beginOutputSection records a new section starting at the next source
// line the output buffer will create — the line the caller is about to
// write — and, when the section is owned by an event, binds the event
// to it for the Events tab's click-to-jump mapping. Guarded by mu.
func (t *TUI) beginOutputSection(owner *outputSectionOwner) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.eventSections == nil {
		t.eventSections = make(map[outputSectionOwner]int)
	}
	idx := len(t.outputSections)
	t.outputSections = append(t.outputSections, outputSection{
		startLine: len(t.output.CompletedLines()),
	})
	if owner != nil {
		t.eventSections[*owner] = idx
	}
}

// clearPendingOutputOwner drops a pending section owner whose attempt
// ended without visible output, so no later, unrelated content opens
// the abandoned section.
func (t *TUI) clearPendingOutputOwner() {
	t.mu.Lock()
	t.pendingOwner = nil
	t.mu.Unlock()
}

// jumpToEventAtClick scrolls the Output tab to the output section of
// the pressed event — but only when the press lands on the jump
// marker that ends the attempt-start line's text: presses elsewhere in
// the Events pane, on rows without a node, and events whose chain owns
// no section are no-ops. Called with t.mu held, like
// toggleHandoffAtClick.
func (t *TUI) jumpToEventAtClick(x, y int) {
	if !t.tabs.Expanded[1] {
		return
	}
	box := t.tabs.Boxes(t.width, t.height)[1]
	if x < box.Left || x >= box.Right || y <= box.Top || y >= box.Bottom {
		return
	}
	// The press's screen row maps onto the tab's content row by
	// dropping the label strip and re-adding the scroll offset, the
	// same mapping toggleHandoffAtClick uses.
	row := t.scrolls[1].Offset + (y - box.Top - 1)
	node := t.events.NodeAtRow(row)
	// Only the attempt-start line's jump marker is clickable: a press
	// on any other row never jumps. See TheoryOfTUIOutputSections.
	if node == nil || !nodeHasJumpMarker(node) {
		return
	}
	line, ok := t.eventDisplayLine(row, box)
	if !ok {
		return
	}
	start, end, hasMarker := markerColumnRange(line.Text, taiui.DisplayWidthOptions())
	pressCol := x - box.Left
	if !hasMarker || pressCol < start || pressCol >= end {
		return
	}
	t.showOutputSection(t.sectionOfEventNode(node))
}

// eventDisplayLine returns the Events pane's wrapped display line at
// the given content row. The tree recomputes its display with the same
// width and shade the pane renders with, so the line's text and column
// layout match what is on screen. Click path only — never per frame.
func (t *TUI) eventDisplayLine(row int, box taiui.Box) (taiui.Line, bool) {
	contentWidth := max(box.Width()-1, 1)
	base := panelStyle.BaseBG
	if t.tabs.Focus == 1 {
		base = panelStyle.FocusBG
	}
	display := t.events.Display(contentWidth, base)
	if row < 0 || row >= len(display) {
		return taiui.Line{}, false
	}
	return display[row], true
}

// nodeHasJumpMarker reports whether the node's header line ends with
// the jump marker: only attempt-start lines carry it, so only their
// presses are eligible for the section jump. The check runs on the
// node's source line, which carries no elapsed-timer suffix.
func nodeHasJumpMarker(node *taiui.EventNode) bool {
	return len(node.Lines) > 0 && strings.HasSuffix(node.Lines[0].Text, " "+eventJumpMarker)
}

// markerColumnRange returns the cell-column range of the jump marker
// within text, measured cluster by cluster with the given width
// options — the same measurement the renderer uses, so multi-column
// emoji line up with the pressed cell. ok is false when the marker is
// absent from the text.
func markerColumnRange(text string, options displaywidth.Options) (start, end int, ok bool) {
	iter := options.StringGraphemes(text)
	col := 0
	for iter.Next() {
		width := iter.Width()
		if iter.Value() == eventJumpMarker {
			return col, col + width, true
		}
		col += width
	}
	return 0, 0, false
}

// sectionOfEventNode resolves the output section an event row maps to:
// the event's own section, or the nearest ancestor's — every event of
// an attempt (request, finish, usage, retry) reaches the output its
// attempt wrote. -1 when no event on the chain owns a section.
func (t *TUI) sectionOfEventNode(node *taiui.EventNode) int {
	for n := node; n != nil; {
		if idx, ok := t.eventSections[outputSectionOwner{run: n.Run, seq: n.Seq}]; ok {
			return idx
		}
		if n.ParentSeq == 0 {
			break
		}
		n = findEventNode(t.events.Roots, n.Run, n.ParentSeq)
	}
	return -1
}

// findEventNode locates the tree node identified by (run, seq) in a
// depth-first search over the forest; nil when absent.
func findEventNode(roots []*taiui.EventNode, run, seq int) *taiui.EventNode {
	var walk func(n *taiui.EventNode) *taiui.EventNode
	walk = func(n *taiui.EventNode) *taiui.EventNode {
		if n.Run == run && n.Seq == seq {
			return n
		}
		for _, child := range n.Children {
			if found := walk(child); found != nil {
				return found
			}
		}
		return nil
	}
	for _, root := range roots {
		if found := walk(root); found != nil {
			return found
		}
	}
	return nil
}

// showOutputSection scrolls the Output tab's view so the section's
// first display line lands at the top of the pane. The jump result
// must be visible: the Output tab is expanded and focused when needed,
// and following the tail stops — the live tail resumes only when the
// view reaches the latest row.
func (t *TUI) showOutputSection(idx int) {
	if idx < 0 || idx >= len(t.outputSections) {
		return
	}
	if !t.tabs.Expanded[0] || t.tabs.Focus != 0 {
		t.tabs.Toggle(0)
	}
	boxes := t.tabs.Boxes(t.width, t.height)
	box := boxes[0]
	if box.Width() <= 0 || box.Height() <= 0 {
		return
	}
	display := wrappedDisplay(t, 0, box)
	if len(display) == 0 {
		return
	}
	offset := t.outputSectionDisplayTop(t.outputSections[idx].startLine, box)
	t.scrolls[0].Offset = taiui.ClampOffset(offset, len(display), t.tuiPaneHeight(0, box))
	t.scrolls[0].Follow = false
}

// outputSectionDisplayTop computes the display offset at which the
// section starting at source line startLine begins: the source lines
// before it are wrapped with the same wrapping the Output pane's
// display uses, so the offset indexes into wrappedDisplay's lines
// identically. startLine is always a completed source line, so the
// wrap covers completed lines only.
func (t *TUI) outputSectionDisplayTop(startLine int, box taiui.Box) int {
	contentWidth := max(box.Width()-1, 1)
	lines := t.output.Lines()
	if startLine > len(lines) {
		startLine = len(lines)
	}
	if startLine <= 0 {
		return 0
	}
	return len(taiui.WrapLinesColored(lines[:startLine], contentWidth))
}
