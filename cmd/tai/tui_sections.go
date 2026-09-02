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
  wrapped with the same wrapping the Output pane's display uses, so
  the offset indexes into the wrapped display identically. The wrap
  runs on the click path only, not per frame. In the collapsed thought
  projection the offset derives from the cached per-section row counts
  instead; see TheoryOfTUIThoughtsCollapse.
`

// collapsedThoughtRow renders one source line as the collapsed row of a
// thought section: the line truncated to the content width, keeping the
// line's color, so the row never exceeds one display column.
func collapsedThoughtRow(line taiui.Line, contentWidth int) taiui.Line {
	return taiui.Line{
		Text:  displaywidth.TruncateString(line.Text, contentWidth, "…"),
		Color: line.Color,
	}
}

const TheoryOfTUIThoughtsCollapse = `
Output tab thought collapse theory (cmd/tai):

- The t key toggles thoughtsCollapsed. When on, every thought section
  of the Output tab renders as exactly one display row: a closed
  section shows its first source line, and a still-streaming section —
  a generation or handoff request in flight — shows its newest
  sectioned line, so a long thinking phase keeps a live single-row
  view of the newest reasoning while the earlier lines stay hidden.
  Non-thought sections render in full, and lines appended outside any
  section (command output, stderr) always render in full.
- Collapse is a display projection, never a buffer change: the output
  buffer and the expanded wrap cache are untouched, so toggling the
  mode off re-reveals every line. See TheoryOfTUINoTruncation.
- The projection wraps each source line at most once per content
  width: the prefix, the closed sections, the last section, and the
  tail are wrapped incrementally behind a single left-to-right
  pointer, and only the unprocessed suffix is wrapped per frame; a
  thought section's single row is the one transient row, overwritten
  in place as the newest line changes. A content-width change resets
  the caches.
- Section jumps (showOutputSection) derive a section's display row
  from the cached per-section row counts, so a click on an
  attempt-start marker lands on the collapsed section's row.
`

// collapsedOutputDisplay renders the Output tab's collapsed projection:
// prefix and tail lines in full, non-thought sections in full, and each
// thought section as one row — its first source line when closed, its
// newest sectioned line while output is still streaming into it. Every
// source line is wrapped at most once per content width: the regions
// behind a single left-to-right pointer are cached and only the
// unprocessed suffix is wrapped per frame. See
// TheoryOfTUIThoughtsCollapse.
func (t *TUI) collapsedOutputDisplay(contentWidth int) []taiui.Line {
	lines := t.output.Lines()
	sections := t.outputSections

	if t.collapsedWidth != contentWidth {
		// A content-width change invalidates every wrapped row; the
		// wrap loop below rebuilds the projection from the start, so
		// every section registers with a zeroed count slot and no
		// section is finalized here.
		t.collapsedWidth = contentWidth
		t.collapsedDisplay = t.collapsedDisplay[:0]
		t.collapsedCounts = t.collapsedCounts[:0]
		t.collapsedWrapped = 0
		t.collapsedTailRows = 0
		for len(t.collapsedCounts) < len(sections) {
			t.collapsedCounts = append(t.collapsedCounts, 0)
		}
	} else {
		// Fold the previously last section into its closed form and
		// register every section the cache does not know yet.
		for len(t.collapsedCounts) < len(sections) {
			n := len(t.collapsedCounts)
			if n > 0 {
				t.finalizeCollapsedSection(n-1, sections[n].startLine, lines, contentWidth)
			}
			t.collapsedCounts = append(t.collapsedCounts, 0)
		}
	}

	// Wrap the unprocessed suffix region by region: the prefix before
	// the first section, every closed section, the last section's
	// territory, and the tail. The buffer is append-only and the
	// pointer advances left to right, so appended rows keep source
	// order.
	for t.collapsedWrapped < len(lines) {
		from := t.collapsedWrapped
		var to int
		switch {
		case len(sections) == 0 || from < sections[0].startLine:
			// Prefix: lines before the first section render in full.
			to = len(lines)
			if len(sections) > 0 {
				to = sections[0].startLine
			}
			t.collapsedDisplay = taiui.WrapLinesColoredInto(
				lines[from:to], contentWidth, t.collapsedDisplay)
		case from < sections[len(sections)-1].startLine:
			// A closed section: a thought section renders one row, a
			// non-thought section in full.
			i := 0
			for i < len(sections)-1 && sections[i+1].startLine <= from {
				i++
			}
			to = sections[i+1].startLine
			if sections[i].isThought {
				if to > sections[i].startLine {
					t.collapsedDisplay = append(t.collapsedDisplay,
						collapsedThoughtRow(lines[sections[i].startLine], contentWidth))
					t.collapsedCounts[i] = 1
				}
			} else {
				before := len(t.collapsedDisplay)
				t.collapsedDisplay = taiui.WrapLinesColoredInto(
					lines[from:to], contentWidth, t.collapsedDisplay)
				t.collapsedCounts[i] += len(t.collapsedDisplay) - before
			}
		case from < t.sectionedLines:
			// The last section's territory: a thought section's lines
			// stay hidden under its single row, a non-thought section
			// renders in full.
			to = t.sectionedLines
			if !sections[len(sections)-1].isThought {
				before := len(t.collapsedDisplay)
				t.collapsedDisplay = taiui.WrapLinesColoredInto(
					lines[from:to], contentWidth, t.collapsedDisplay)
				t.collapsedCounts[len(sections)-1] += len(t.collapsedDisplay) - before
			}
		default:
			// Tail: lines appended outside any section render in full.
			to = len(lines)
			before := len(t.collapsedDisplay)
			t.collapsedDisplay = taiui.WrapLinesColoredInto(
				lines[from:to], contentWidth, t.collapsedDisplay)
			t.collapsedTailRows += len(t.collapsedDisplay) - before
		}
		t.collapsedWrapped = to
	}

	// The collapsed row of a thought last section: its first line when
	// closed, its newest sectioned line while streaming.
	if last := len(sections) - 1; last >= 0 && sections[last].isThought {
		secEnd := t.sectionedLines
		if secEnd > sections[last].startLine {
			row := lines[sections[last].startLine]
			if t.collapsedThoughtsOpen() {
				row = lines[secEnd-1]
			}
			t.placeCollapsedRow(collapsedThoughtRow(row, contentWidth))
		}
	}

	return t.collapsedDisplay
}

// outputSection is one section of the Output tab's content: the index
// in the output line buffer at which the section's first source line
// begins, and whether the section carries reasoning thoughts, which
// the collapsed display reduces to one row. The section spans to the
// next section's start.
type outputSection struct {
	startLine int
	isThought bool
}

// outputSectionOwner identifies the pipeline event that owns a
// section: the event's (run, sequence) identity, the same identity the
// Events tab files nodes by.
type outputSectionOwner struct {
	run int
	seq int
}

// finalizeCollapsedSection folds the previously last section into its
// closed form when a newer section opens: a non-thought section keeps
// its rows, absorbs the tail rows, and wraps any unprocessed trailing
// lines; a thought section drops the tail rows hidden under its
// collapsed row and freezes that row to the section's first line. See
// TheoryOfTUIThoughtsCollapse.
func (t *TUI) finalizeCollapsedSection(idx int, spanEnd int, lines []taiui.Line, contentWidth int) {
	if t.outputSections[idx].isThought {
		// Tail rows are hidden under the collapsed row.
		t.collapsedDisplay = t.collapsedDisplay[:len(t.collapsedDisplay)-t.collapsedTailRows]
		t.collapsedTailRows = 0
		// The collapsed row freezes to the section's first line.
		if t.collapsedCounts[idx] == 1 {
			t.collapsedDisplay[len(t.collapsedDisplay)-1] =
				collapsedThoughtRow(lines[t.outputSections[idx].startLine], contentWidth)
		}
	} else {
		t.collapsedCounts[idx] += t.collapsedTailRows
		t.collapsedTailRows = 0
		if t.collapsedWrapped < spanEnd {
			before := len(t.collapsedDisplay)
			t.collapsedDisplay = taiui.WrapLinesColoredInto(
				lines[t.collapsedWrapped:spanEnd], contentWidth, t.collapsedDisplay)
			t.collapsedCounts[idx] += len(t.collapsedDisplay) - before
		}
	}
	t.collapsedWrapped = spanEnd
}

// beginOutputSection records a new section starting at the next source
// line the output buffer will create — the line the caller is about to
// write — marks whether the section carries reasoning thoughts, and,
// when the section is owned by an event, binds the event to it for the
// Events tab's click-to-jump mapping. Guarded by mu.
func (t *TUI) beginOutputSection(owner *outputSectionOwner, isThought bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.eventSections == nil {
		t.eventSections = make(map[outputSectionOwner]int)
	}
	// Separator blank lines and any lines appended outside the
	// sectioned path since the last content belong to the closing
	// section's span: the high-water mark covers them so the collapsed
	// projection treats them as sectioned. See
	// TheoryOfTUIThoughtsCollapse.
	covered := len(t.output.CompletedLines())
	if t.output.HasPartial() {
		covered++
	}
	if covered > t.sectionedLines {
		t.sectionedLines = covered
	}
	idx := len(t.outputSections)
	t.outputSections = append(t.outputSections, outputSection{
		startLine: len(t.output.CompletedLines()),
		isThought: isThought,
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

// collapsedThoughtsOpen reports whether the last thought section is
// still receiving output: a generation or handoff request is in
// flight. A partial trailing line alone does not count — output stops
// arriving once the request ends, so the row freezes to the section's
// first line. The caller holds t.mu. See
// TheoryOfTUIThoughtsCollapse.
func (t *TUI) collapsedThoughtsOpen() bool {
	return t.generating || t.handoff
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

// collapsedSectionTop returns the display offset at which section idx's
// content begins in the collapsed projection: the prefix rows plus the
// display rows of every earlier section. The caller holds t.mu. See
// TheoryOfTUIThoughtsCollapse.
func (t *TUI) collapsedSectionTop(idx int) int {
	rest := 0
	for _, count := range t.collapsedCounts[min(idx, len(t.collapsedCounts)):] {
		rest += count
	}
	return len(t.collapsedDisplay) - t.collapsedTailRows - rest
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

// placeCollapsedRow writes the collapsed row of the last thought
// section: the row slot is created before any tail rows when the
// section receives its first content, then overwritten in place as the
// newest line changes. See TheoryOfTUIThoughtsCollapse.
func (t *TUI) placeCollapsedRow(row taiui.Line) {
	K := len(t.collapsedCounts) - 1
	if t.collapsedCounts[K] == 0 {
		at := len(t.collapsedDisplay) - t.collapsedTailRows
		if t.collapsedTailRows > 0 {
			t.collapsedDisplay = append(t.collapsedDisplay, taiui.Line{})
			copy(t.collapsedDisplay[at+1:], t.collapsedDisplay[at:])
			t.collapsedDisplay[at] = row
		} else {
			t.collapsedDisplay = append(t.collapsedDisplay, row)
		}
		t.collapsedCounts[K] = 1
		return
	}
	t.collapsedDisplay[len(t.collapsedDisplay)-t.collapsedTailRows-1] = row
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
	// The collapsed projection reindexes the sections: the offset
	// derives from the cached per-section row counts instead of
	// wrapping the source lines. See TheoryOfTUIThoughtsCollapse.
	offset := t.outputSectionDisplayTop(t.outputSections[idx].startLine, box)
	if t.thoughtsCollapsed {
		offset = t.collapsedSectionTop(idx)
	}
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
