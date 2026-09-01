package main

import (
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

- A left press on an Events row scrolls the Output tab's view to the
  section the pressed event's attempt wrote. The press maps onto an
  event node through taiui.EventTree.NodeAtRow; the node or its nearest
  section-owning ancestor selects the section, so every event of an
  attempt (request, finish, usage) reaches the attempt's output, and an
  event with no output (an attempt that produced nothing) is a no-op.
  Mirroring jumpToTransition, the jump expands and focuses the Output
  tab when needed and stops following the tail; the live tail resumes
  only when the view reaches the latest row.

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
// the event whose display rows the press landed on. Rows outside the
// Events pane's content area, rows without a node, and events whose
// chain owns no section are no-ops. Called with t.mu held, like
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
	node := t.events.NodeAtRow(t.scrolls[1].Offset + (y - box.Top - 1))
	if node == nil {
		return
	}
	t.showOutputSection(t.sectionOfEventNode(node))
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
