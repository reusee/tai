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

- The user's chat input opens the first section: displayChatInput
  opens a section before writing the chats, so the initial input is
  collapsible like every other section. See TheoryOfOutputControls.

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
  source line begins: the projection records each section's
  display-row count, so the offset is the prefix rows plus the earlier
  sections' counts (outputSectionOffset) — the same offsets the
  projection rendered. See TheoryOfOutputControls.
`

const TheoryOfOutputControls = `
Output tab control column theory (cmd/tai):

- The Output tab reserves a control column beside its content rows,
  one Han character wide: the panel's content is indented past the
  column (taiui.ContentIndent), so no content hides under the controls,
  and the column's background follows the tab's focus state like the
  panel's own. The title row spans the full box width and is not part
  of the column.
- Every section carries a fold control: the unicode triangle ▾ while
  the section is expanded, ▸ while collapsed, drawn in the default
  foreground so it never competes with the content. A press on the
  control's cells toggles the section's collapsed state; expanding a
  collapsed section scrolls the view so the section's first display
  row lands at the pane top and stops following the tail, so the
  expanded content opens at its beginning.
- The c key toggles the whole structure: when not every section is
  collapsed, it snapshots the per-section collapsed state and folds
  every section to one row; when every section is collapsed, it
  restores the snapshotted state. A manual expand breaks the
  all-collapsed state, so the next press folds and re-snapshots rather
  than restoring. A press on any collapsed section's display row
  expands it, so the collapsed structure doubles as the table of
  contents. Expanded sections are inert to body presses — only the
  control column collapses them — so reading inside an expanded
  section never folds it.
- The control follows the content: it renders at the section's first
  display row, clamped into the viewport when that row has scrolled
  above it, so every section with a visible row stays addressable while
  scrolled. Sections whose rows left the viewport carry no control.
- A collapsed section renders as exactly one display row — its show
  line, truncated to the content width. The show line is the section's
  first source line at collapse time; while output streams into the
  collapsed section, it follows the newest line, completed or partial,
  so the row always shows the latest output. Collapse is a display
  projection over the append-only buffer: the projection re-derives
  from the source, so expanding re-reveals every line. See
  TheoryOfTUINoTruncation.
- A section may carry several controls. The column shows the first
  control per row; when the pointer hovers the control column on a
  control's row, all of the section's controls display horizontally,
  one Han-width slot each, and a press maps its column onto the slot.
  The pointer's no-button motion events (mode 1003) drive the hover.
- The projection is incremental: each completed source line wraps at
  most once per content width behind a left-to-right pointer, the
  trailing partial line re-wraps fresh every frame, and a section
  toggle, a collapse-all key press, or a content-width change resets
  the projection. Only the last collapsed section's row changes while
  output streams, so the projection rewrites that single row in place
  each frame instead of re-deriving.
`

// controlColumnWidth is the control column's width in terminal cells:
// one Han character. See TheoryOfOutputControls.
const controlColumnWidth = 2

const (
	// sectionGlyphCollapsed marks a collapsed section: a press expands
	// it. See TheoryOfOutputControls.
	sectionGlyphCollapsed = "▸"
	// sectionGlyphExpanded marks an expanded section: a press collapses
	// it. See TheoryOfOutputControls.
	sectionGlyphExpanded = "▾"
)

// outputControl is one control of a section's control column: a unicode
// glyph label resolved per section at render time. The first control is
// the fold toggle; further controls join the hover strip. See
// TheoryOfOutputControls.
type outputControl struct {
	Glyph string
}

// controlStripText renders the horizontal strip of a section's
// controls: one Han-width slot per control, shown when the pointer
// hovers the control column on the control's row. See
// TheoryOfOutputControls.
func controlStripText(controls []outputControl) string {
	var b strings.Builder
	for _, c := range controls {
		b.WriteString(c.Glyph)
		b.WriteString(" ")
	}
	return b.String()
}

// sectionControls returns the controls of section idx. The fold toggle
// comes first, its glyph following the section's collapsed state;
// further controls append here. See TheoryOfOutputControls.
func (t *TUI) sectionControls(idx int) []outputControl {
	glyph := sectionGlyphExpanded
	if t.outputSections[idx].collapsed {
		glyph = sectionGlyphCollapsed
	}
	return []outputControl{{Glyph: glyph}}
}

// outputSection is one section of the Output tab's content: the index
// in the output line buffer at which the section's first source line
// begins, whether the section is collapsed to one row, and the source
// line the collapsed row displays — the header at collapse time, the
// latest line once new output arrives. The section spans to the next
// section's start.
type outputSection struct {
	startLine int
	collapsed bool
	showLine  int
}

// outputSectionOwner identifies the pipeline event that owns a
// section: the event's (run, sequence) identity, the same identity the
// Events tab files nodes by.
type outputSectionOwner struct {
	run int
	seq int
}

// outputControlRow is one rendered control: the section the control
// acts on and the absolute screen row of its glyph. See
// TheoryOfOutputControls.
type outputControlRow struct {
	section int
	row     int
}

// sectionCollapsedRow renders one source line as the single row of a
// collapsed section: the line truncated to the content width, keeping
// the line's color. See TheoryOfOutputControls.
func sectionCollapsedRow(line taiui.Line, contentWidth int) taiui.Line {
	return taiui.Line{
		Text:  displaywidth.TruncateString(line.Text, contentWidth, "…"),
		Color: line.Color,
	}
}

// sectionCollapsed reports the section's projected form: a collapsed
// section renders as its single show-line row while every other section
// renders in full. The caller holds t.mu. See TheoryOfOutputControls.
func (t *TUI) sectionCollapsed(idx int) bool {
	return t.outputSections[idx].collapsed
}

// outputDisplay renders the Output tab's display: the projection of the
// append-only output buffer where every collapsed section contributes
// one row — its show line, truncated to the content width — and every
// other line wraps in full. Each completed source line wraps at most
// once per content width behind a left-to-right pointer; the trailing
// partial line re-wraps fresh every frame; a section toggle, a
// collapse-all key press, or a content-width change resets the
// projection. The caller holds t.mu. See TheoryOfOutputControls.
func (t *TUI) outputDisplay(contentWidth int) []taiui.Line {
	lines := t.output.Lines()
	completed := len(t.output.CompletedLines())
	sections := t.outputSections
	last := len(sections) - 1

	reset := t.projWidth != contentWidth
	if reset {
		t.projWidth = contentWidth
		t.projDisplay = t.projDisplay[:0]
		t.projCounts = t.projCounts[:0]
		t.projWrapped = 0
		t.projPrefixRows = 0
		t.projTailRows = 0
		t.projPartialRows = 0
	} else if t.projPartialRows > 0 {
		// Drop the previous frame's transient partial rows before the
		// region loop appends behind them.
		t.projDisplay = t.projDisplay[:len(t.projDisplay)-t.projPartialRows]
		t.projPartialRows = 0
	}
	for len(t.projCounts) < len(sections) {
		n := len(t.projCounts)
		if n > 0 && !reset {
			t.finalizeProjectedSection(n-1, sections[n].startLine, lines, contentWidth)
		}
		t.projCounts = append(t.projCounts, 0)
	}

	for t.projWrapped < completed {
		from := t.projWrapped
		var to int
		switch {
		case len(sections) == 0 || from < sections[0].startLine:
			// Prefix: lines before the first section render in full.
			to = completed
			if len(sections) > 0 {
				to = sections[0].startLine
			}
			before := len(t.projDisplay)
			t.projDisplay = taiui.WrapLinesColoredInto(lines[from:to], contentWidth, t.projDisplay)
			t.projPrefixRows += len(t.projDisplay) - before
		case from < sections[last].startLine:
			// A closed section.
			i := 0
			for i < last && sections[i+1].startLine <= from {
				i++
			}
			to = sections[i+1].startLine
			if t.sectionCollapsed(i) {
				if t.projCounts[i] == 0 && to > sections[i].startLine {
					t.projDisplay = append(t.projDisplay, sectionCollapsedRow(lines[sections[i].showLine], contentWidth))
					t.projCounts[i] = 1
				}
			} else {
				before := len(t.projDisplay)
				t.projDisplay = taiui.WrapLinesColoredInto(lines[from:to], contentWidth, t.projDisplay)
				t.projCounts[i] += len(t.projDisplay) - before
			}
		case from < t.sectionedLines:
			// The last section's territory; the partial line that
			// sectionedLines may cover wraps after the loop.
			to = min(t.sectionedLines, completed)
			if t.sectionCollapsed(last) {
				if t.projCounts[last] == 0 && to > sections[last].startLine {
					t.projDisplay = append(t.projDisplay, sectionCollapsedRow(lines[sections[last].showLine], contentWidth))
					t.projCounts[last] = 1
				}
			} else {
				before := len(t.projDisplay)
				t.projDisplay = taiui.WrapLinesColoredInto(lines[from:to], contentWidth, t.projDisplay)
				t.projCounts[last] += len(t.projDisplay) - before
			}
		default:
			// Tail: lines appended outside any section render in full.
			to = completed
			before := len(t.projDisplay)
			t.projDisplay = taiui.WrapLinesColoredInto(lines[from:to], contentWidth, t.projDisplay)
			t.projTailRows += len(t.projDisplay) - before
		}
		t.projWrapped = to
	}

	// The last collapsed section's single row follows the streaming
	// output: rewrite it in place from the section's show line, which
	// the output path keeps on the newest line. The rewrite also
	// renders the row when the section's span holds only a partial
	// line — a case the wrap loop never reaches, because completed
	// equals the section's start until the partial completes. Closed
	// collapsed sections never change, so only this row is rewritten.
	if last >= 0 && t.sectionCollapsed(last) {
		sec := &t.outputSections[last]
		covered := completed
		if t.output.HasPartial() {
			covered++
		}
		if covered > sec.startLine {
			row := sectionCollapsedRow(lines[sec.showLine], contentWidth)
			pos := t.outputSectionOffset(last)
			if t.projCounts[last] == 0 {
				// Insert at the section's projected position, before
				// any tail rows rendered after it.
				t.projDisplay = append(t.projDisplay, taiui.Line{})
				copy(t.projDisplay[pos+1:], t.projDisplay[pos:])
				t.projCounts[last] = 1
			}
			if pos < len(t.projDisplay) {
				t.projDisplay[pos] = row
			}
		}
	}

	// The trailing partial line re-wraps fresh every frame; a collapsed
	// section hides it under the section's single row, which shows the
	// newest line — the partial itself when one is open.
	if t.output.HasPartial() {
		show := true
		switch {
		case len(sections) == 0 || completed < sections[0].startLine:
		case completed < t.sectionedLines:
			show = !t.sectionCollapsed(last)
		}
		if show {
			before := len(t.projDisplay)
			t.projDisplay = taiui.WrapLinesColoredInto(lines[completed:completed+1], contentWidth, t.projDisplay)
			t.projPartialRows = len(t.projDisplay) - before
		}
	}

	return t.projDisplay
}

// finalizeProjectedSection folds the previously last section into its
// closed form when a newer section opens: an expanded section absorbs
// the tail rows and wraps its remaining unprocessed lines; a collapsed
// section drops the tail rows its single row hides and, when its span
// stayed empty until now, renders its show-line row. See
// TheoryOfOutputControls.
func (t *TUI) finalizeProjectedSection(idx int, spanEnd int, lines []taiui.Line, contentWidth int) {
	sec := t.outputSections[idx]
	if t.sectionCollapsed(idx) {
		t.projDisplay = t.projDisplay[:len(t.projDisplay)-t.projTailRows]
		t.projTailRows = 0
		if t.projCounts[idx] == 0 && spanEnd > sec.startLine {
			t.projDisplay = append(t.projDisplay, sectionCollapsedRow(lines[sec.showLine], contentWidth))
			t.projCounts[idx] = 1
		}
	} else {
		t.projCounts[idx] += t.projTailRows
		t.projTailRows = 0
		if t.projWrapped < spanEnd {
			before := len(t.projDisplay)
			t.projDisplay = taiui.WrapLinesColoredInto(lines[t.projWrapped:spanEnd], contentWidth, t.projDisplay)
			t.projCounts[idx] += len(t.projDisplay) - before
		}
	}
	t.projWrapped = spanEnd
}

// resetProjectionLocked drops the projection state, so the next
// outputDisplay call re-derives every row from the source. The caller
// holds t.mu. See TheoryOfOutputControls.
func (t *TUI) resetProjectionLocked() {
	t.projWidth = -1
}

// collapseAllSections toggles the collapse-all key's two states: when
// not every section is collapsed, it snapshots the per-section
// collapsed state and folds the whole output structure to one row per
// section; when every section is collapsed, it restores the state
// snapshotted by the last fold instead. A manual expand breaks the
// all-collapsed state, so the next press folds and re-snapshots rather
// than restoring. Every folded section shows its header; new output
// switches a collapsed section's row to its latest line. See
// TheoryOfOutputControls.
func (t *TUI) collapseAllSections() {
	t.mu.Lock()
	defer t.mu.Unlock()
	allCollapsed := true
	for i := range t.outputSections {
		if !t.outputSections[i].collapsed {
			allCollapsed = false
			break
		}
	}
	if allCollapsed && t.collapseAllSaved != nil {
		// Restore the state from before the last fold. Sections created
		// after the snapshot arrive expanded, so restoring expands them.
		for i := range t.outputSections {
			sec := &t.outputSections[i]
			if i < len(t.collapseAllSaved) {
				sec.collapsed = t.collapseAllSaved[i]
			} else {
				sec.collapsed = false
			}
		}
		t.resetProjectionLocked()
		return
	}
	// Fold branch: snapshot the current collapsed state, then fold
	// everything, re-anchoring drifted show lines to the headers.
	saved := make([]bool, len(t.outputSections))
	changed := false
	for i := range t.outputSections {
		sec := &t.outputSections[i]
		saved[i] = sec.collapsed
		if !sec.collapsed || sec.showLine != sec.startLine {
			sec.collapsed = true
			sec.showLine = sec.startLine
			changed = true
		}
	}
	t.collapseAllSaved = saved
	if changed {
		t.resetProjectionLocked()
	}
}

// toggleOutputSectionLocked flips section idx's collapsed state. A
// newly collapsed section shows its header; the latest-line display
// returns only when new output arrives. The caller holds t.mu. See
// TheoryOfOutputControls.
func (t *TUI) toggleOutputSectionLocked(idx int) {
	if idx < 0 || idx >= len(t.outputSections) {
		return
	}
	sec := &t.outputSections[idx]
	sec.collapsed = !sec.collapsed
	if sec.collapsed {
		sec.showLine = sec.startLine
	}
	t.resetProjectionLocked()
}

// outputSectionOffset returns the display offset at which section idx's
// rows begin in the projected display: the prefix rows plus every
// earlier section's row count. The projection must have been computed
// for the current content width. The caller holds t.mu. See
// TheoryOfTUIOutputSections.
func (t *TUI) outputSectionOffset(idx int) int {
	top := t.projPrefixRows
	for i := 0; i < idx && i < len(t.projCounts); i++ {
		top += t.projCounts[i]
	}
	return top
}

// outputSectionAtOffset locates the section whose projected rows cover
// the given display offset in the current projection: the prefix rows
// clamp to the first section and the tail rows fall back to the last,
// so every display offset maps onto a section when one exists. The
// projection must have been computed for the current content width.
// The caller holds t.mu. See TheoryOfOutputControls.
func (t *TUI) outputSectionAtOffset(offset int) int {
	if len(t.outputSections) == 0 {
		return -1
	}
	if offset < t.projPrefixRows {
		return 0
	}
	for i, count := range t.projCounts {
		if offset < t.outputSectionOffset(i)+count {
			return i
		}
	}
	return len(t.outputSections) - 1
}

// outputControlRows computes the control column's rows for the current
// view: one row per section with at least one visible display row,
// pinned to the section's first display row or the viewport top when
// that row scrolled above it. Sections tile the content disjointly, so
// two controls never pin to the same row. The caller holds t.mu. See
// TheoryOfOutputControls.
func (t *TUI) outputControlRows(box taiui.Box, display []taiui.Line, offset int) []outputControlRow {
	paneHeight := t.tuiPaneHeight(0, box)
	var rows []outputControlRow
	for i := range t.outputSections {
		count := 0
		if i < len(t.projCounts) {
			count = t.projCounts[i]
		}
		if count <= 0 {
			continue
		}
		top := t.outputSectionOffset(i)
		if top+count <= offset || top >= offset+paneHeight {
			continue
		}
		rows = append(rows, outputControlRow{
			section: i,
			row:     box.Top + 1 + (max(top, offset) - offset),
		})
	}
	return rows
}

// toggleControlAtClick toggles the section whose control the press hit:
// the press must land on a rendered control row; with one control the
// press must land in the control column, and while the hover strip
// shows, the press column maps onto the strip's slots — one Han-width
// slot per control, the fold toggle first. It reports whether a control
// consumed the press. A minimal TUI without the output buffer carries
// no sections and no controls, so nothing consumes the press. The
// caller holds t.mu. See TheoryOfOutputControls.
func (t *TUI) toggleControlAtClick(x, y int) bool {
	if !t.tabs.Expanded[0] || t.output == nil {
		return false
	}
	box := t.tabs.Boxes(t.width, t.height)[0]
	if box.Width() <= controlColumnWidth || box.Height() <= 0 {
		return false
	}
	panelBottom := box.Bottom
	if t.interactive {
		panelBottom--
	}
	if y < box.Top+1 || y >= panelBottom {
		return false
	}
	// A press outside the control column can only be a hover-strip
	// slot, which requires the strip conditions; reject before paying
	// for the projection walk.
	if x < box.Left || x >= box.Left+controlColumnWidth {
		if !(t.ctlHover && t.mouseReporting && t.ctlHoverY == y) {
			return false
		}
	}
	display := wrappedDisplay(t, 0, box)
	if len(display) == 0 {
		return false
	}
	offset := taiui.ClampOffset(t.scrolls[0].Offset, len(display), t.tuiPaneHeight(0, box))
	for _, row := range t.outputControlRows(box, display, offset) {
		if row.row != y {
			continue
		}
		controls := t.sectionControls(row.section)
		if len(controls) == 0 {
			return false
		}
		// The press must land in the control column, or — while the
		// hover strip shows — anywhere within the strip's slots. See
		// TheoryOfOutputControls.
		extent := controlColumnWidth
		if t.ctlHover && t.mouseReporting && t.ctlHoverY == y && len(controls) > 1 {
			extent = controlColumnWidth * len(controls)
		}
		if x < box.Left || x >= box.Left+min(extent, box.Width()) {
			return false
		}
		slot := min((x-box.Left)/controlColumnWidth, len(controls)-1)
		if slot != 0 {
			return true
		}
		wasCollapsed := t.outputSections[row.section].collapsed
		t.toggleOutputSectionLocked(row.section)
		if wasCollapsed {
			// Expanding a collapsed section scrolls the view to the
			// section's first display row. See TheoryOfOutputControls.
			t.scrollToOutputSection(row.section)
		}
		return true
	}
	return false
}

// setControlHoverLocked records the pointer position from a no-button
// motion event, driving the control column's hover strip. The caller
// holds t.mu. See TheoryOfOutputControls.
func (t *TUI) setControlHoverLocked(x, y int) {
	t.ctlHover = true
	t.ctlHoverX = x
	t.ctlHoverY = y
}

// expandCollapsedSectionAtClick expands the collapsed section whose
// display row the press hit: every row of a collapsed section is its
// expansion target, the click behavior that replaces the removed
// global preview. The view scrolls to the section's first display row,
// so the expanded content opens at its beginning, and the Output tab
// takes the focus so the result is visible. Expanded sections are
// inert here — only the control column collapses them — so reading
// inside an expanded section never folds it. It reports whether the
// press expanded a section. The caller holds t.mu. See
// TheoryOfOutputControls.
func (t *TUI) expandCollapsedSectionAtClick(x, y int) bool {
	if !t.tabs.Expanded[0] || t.output == nil {
		return false
	}
	box := t.tabs.Boxes(t.width, t.height)[0]
	if x < box.Left || x >= box.Right {
		return false
	}
	panelBottom := box.Bottom
	if t.interactive {
		panelBottom--
	}
	if y < box.Top+1 || y >= panelBottom {
		return false
	}
	display := wrappedDisplay(t, 0, box)
	if len(display) == 0 {
		return false
	}
	offset := taiui.ClampOffset(t.scrolls[0].Offset, len(display), t.tuiPaneHeight(0, box))
	idx := t.outputSectionAtOffset(offset + (y - box.Top - 1))
	if idx < 0 || !t.outputSections[idx].collapsed {
		return false
	}
	t.outputSections[idx].collapsed = false
	t.resetProjectionLocked()
	if t.tabs.Focus != 0 {
		t.tabs.FocusTab(0)
	}
	t.scrollToOutputSection(idx)
	return true
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
	// Separator blank lines and any lines appended outside the
	// sectioned path since the last content belong to the closing
	// section's span: the high-water mark covers them so the
	// projection treats them as sectioned. See
	// TheoryOfTUIOutputSections.
	covered := len(t.output.CompletedLines())
	if t.output.HasPartial() {
		covered++
	}
	if covered > t.sectionedLines {
		t.sectionedLines = covered
	}
	start := len(t.output.CompletedLines())
	idx := len(t.outputSections)
	t.outputSections = append(t.outputSections, outputSection{
		startLine: start,
		showLine:  start,
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
// first display line lands at the top of the pane. The jump result must
// be visible: the Output tab is expanded and focused when needed, and
// following the tail stops — the live tail resumes only when the view
// reaches the latest row.
func (t *TUI) showOutputSection(idx int) {
	if idx < 0 || idx >= len(t.outputSections) {
		return
	}
	if !t.tabs.Expanded[0] || t.tabs.Focus != 0 {
		t.tabs.Toggle(0)
	}
	t.scrollToOutputSection(idx)
}

// scrollToOutputSection scrolls the Output tab's view so section idx's
// first display row lands at the top of the pane, stopping the live
// tail — the view keeps the section anchored until the user scrolls
// back to the latest row. The projection is derived for the current
// content width, so the call is safe right after a projection reset.
// The caller holds t.mu. See TheoryOfOutputControls.
func (t *TUI) scrollToOutputSection(idx int) {
	if idx < 0 || idx >= len(t.outputSections) {
		return
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
	// The projected display records each section's row count, so the
	// offset derives from the projection. See TheoryOfTUIOutputSections.
	t.scrolls[0].Offset = taiui.ClampOffset(t.outputSectionOffset(idx), len(display), t.tuiPaneHeight(0, box))
	t.scrolls[0].Follow = false
}
