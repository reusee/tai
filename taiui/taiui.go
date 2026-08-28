package taiui

import (
	"sync"
)

const TheoryOfTaiUI = `
taiui theory: UI = pure Element value derived from state.
- The application holds its state outside the library and derives an
  Element value from it; rendering interprets the element tree into a
  Frame and presents the frame to each screen. A state change is a
  rebuilt Element; the next render reflects the change. There is no
  imperative element-update protocol and no dependency-injection
  framework: per-component caching was not worth its complexity —
  building a Frame is cheap and screens diff whole frames anyway.
  Render context (boxes, styles, draw callbacks) is never stored;
  screens are passed per call.
- Elements are pure values: spec lists are resolved at construction
  time into typed element fields, so rendering reads plain data and
  never parses specs. Zero-argument function specs are evaluated
  eagerly and expand recursively. Unknown specs fail at construction.
  Dynamics that depend on state are expressed as functions that build
  the element tree.
- The spec language is a marker-interface protocol: style and layout
  specs, spec groups, and elements themselves all implement it, so spec
  lists compose and nest. Bare strings are a shorthand for text lines
  only, split at newline boundaries with CRLF normalized to LF.
  Conditionals compose specs conditionally.
- Each render pass allocates a fresh frame per screen; frames are never
  reused unless the screen opts in via FrameReleaser, because a screen
  may retain the frame it presented. A screen implementing
  FrameReleaser returns the frame's cells to an internal pool after
  presenting and must not retain the frame. Frame comparison lets a
  screen detect an unchanged frame and skip repainting, and reports the
  changed cells or rows so a screen can repaint only damaged regions;
  comparisons apply only to frames of equal dimensions, and a screen
  repainting whole rows can reuse a buffer across presents. A nil
  element renders an empty frame, clearing every screen. An element
  with an empty box is skipped entirely: no child is rendered and no
  cursor is recorded.
- The box model (margin, border, padding, fill): the border is a
  one-cell ring between margin and padding that shrinks the content box
  by one cell per side; fill paints a background in the cells no child
  occupies, so children render over it and wide grapheme clusters keep
  their trailing columns. The border draws independently of fill and
  stays visible without a painted background, clipped to the element
  box so a negative margin never paints border glyphs outside it. A
  title draws in the top border, clipped to the visible edge. A content
  box whose border and padding exceed the box dimensions is treated as
  empty and never leaves the element box.
- Flex layout along an axis: each child receives a share of the box
  proportional to its weight, tiling the content area without overlaps
  or gaps; the last child absorbs rounding. The box model and fill
  behave as in the box-model element; when the ring is empty, fill is a
  no-op.
- Overlay stacks children in order, each into the full box; later
  children draw over earlier ones. Overlay enables modals and popups:
  the application derives the overlay from state, so a modal is part of
  the element tree, not a separate layer.
- Text provides multi-line rendering with horizontal and vertical
  alignment and per-position style support. Lines are segmented into
  grapheme clusters: a cluster renders as one cell carrying its base
  and combining runes and advances by its display width, so combining
  sequences and ZWJ emoji occupy their real columns. Width honors
  RUNEWIDTH_EASTASIAN for ambiguous runes. A tab advances to the next
  tab stop relative to the content area's left edge, painting the
  skipped cells when fill is on; in wrapped text, tabs break like
  spaces. With wrapping, lines word-wrap to the box width: breaks fall
  at space runs, words wider than the box hard-break at cluster
  boundaries, and a cluster never splits across lines. Alignment is
  relative to the padded content area, applied per physical line, so
  wrapped lines align independently; conventional rounding places the
  extra column on the right and the extra row below. Fill paints the
  content cells the text does not occupy, including residual gaps left
  by clusters clipped at either edge. Rendering stops at the box's last
  row; lines beyond it are never processed.
- The scroll viewport renders its child into a virtually unbounded
  column and crops to the visible window at the offset, clamped to the
  content extent. Content is clipped to the window on both edges, so
  content never spills onto the screen. With a scrollbar, the child
  renders at the visible width, so wrapped text wraps within the
  visible area. The scroll collects only the cells of the expected
  window, re-collecting when the view falls outside the range, so a
  tall virtual column never accumulates cells for rows outside it.
- The list renders a vertical list of single-line items with a selected
  index, highlighted; the view scrolls to keep the selection visible,
  clamped to the content extent, rendering only the visible items in
  O(window) time. Tab panes apply the same principle.
- The canvas renders offscreen content: the content is data state, and
  rendering is a pure read of it. Cells are stored by value, so a write
  allocates nothing. Rendering snapshots the visible cells under a read
  lock, then draws outside the lock, so a concurrent writer is blocked
  only for the snapshot, never for the draw. A wide cluster covers its
  trailing columns, so a stale cell left by a moved cluster never
  corrupts the display; a wide cluster that would extend past the box's
  right edge is not drawn — content never spills past its box.
- The cursor is part of the render output: text records the cursor
  position in the Frame, and screens position the terminal cursor
  there. Inside a scroll viewport, the cursor is transformed from
  content coordinates to window coordinates. Frame comparison includes
  the cursor state, so a screen detects a cursor-only change and
  repositions without repainting cells.
- The exported API is a minimal facade: spec types, constructors, and
  style helpers only.
`

const TheoryOfTUISupport = `
taiui TUI support theory:
- The TUI content mechanisms (colored line buffers, wrapped colored
  lines, alternating log backgrounds, and grouped colored text
  elements) and the tab state machine (auto-expansion, focus order,
  weighted panel layout, and collapsed strips) live in this library so
  any project can build a terminal UI on top of taiui's rendering. They
  are pure state and rendering: they do not depend on generators,
  blocks, or any application-specific model.
- ScrollState encapsulates the follow-tail view state of a scrollable
  pane: the offset is clamped against the wrapped display-line count,
  following sticks to the latest row, and reaching the latest row
  resumes following.
`

const TheoryOfCellComparison = `
taiui cell comparison theory:
- Cell comparison is optimized for the common case: the Set flag and the
  rune are compared before the style and combining runes, so the majority
  of cells (unset) short-circuit on the cheap fields.
- Styles are compared by pointer identity first: a render pass shares
  style values, so most style comparisons are pointer equality, and the
  Equal method is called only for distinct style values.
`

// Element is a pure, screen-independent description of UI state.
// Implementations are data values: they describe what to render and never
// interact with a screen or a scope.
type Element interface {
	Spec
	element()
}

type Box struct {
	Top    int
	Left   int
	Bottom int
	Right  int
}

// Width returns the box's horizontal extent, clamped to non-negative.
func (b Box) Width() int { return max(b.Right-b.Left, 0) }

// Height returns the box's vertical extent, clamped to non-negative.
func (b Box) Height() int { return max(b.Bottom-b.Top, 0) }

// Intersect returns the overlap of b and o. The result may be empty
// (Width or Height zero) when the boxes do not overlap.
func (b Box) Intersect(o Box) Box {
	return Box{
		Top:    max(b.Top, o.Top),
		Left:   max(b.Left, o.Left),
		Bottom: min(b.Bottom, o.Bottom),
		Right:  min(b.Right, o.Right),
	}
}

// UnderlineColor sets the color of the underline. It is visible only when
// the underline is on.
type UnderlineColor Color

// Align selects the horizontal alignment of a Text element.
type Align uint8

// AlignLeft, AlignRight, and AlignCenter are the alignment values for
// Text elements.

const (
	AlignLeft Align = iota
	AlignRight
	AlignCenter
)

// VAlign selects the vertical alignment of a Text element.
type VAlign uint8

// VAlignTop, VAlignMiddle, and VAlignBottom are the vertical alignment
// values for Text elements.

const (
	VAlignTop VAlign = iota
	VAlignMiddle
	VAlignBottom
)

// BGColor, FGColor, Bold, Underline, and Fill are style and layout specs
// accepted by every element: they set the named property on the element.

type (
	BGColor   Color
	FGColor   Color
	Bold      bool
	Underline bool
	Fill      bool
)

type Frame struct {
	Width  int
	Height int
	Cells  []FrameCell

	// CursorSet reports whether an element requested a cursor, and
	// CursorX and CursorY hold its position. Screens position the
	// terminal cursor at the recorded position.
	CursorSet bool
	CursorX   int
	CursorY   int
}

// FrameCell is one cell of a Frame. Cells that no element drew have Set
// false; screens render them as blank with the default style.
type FrameCell struct {
	Rune  rune   // Rune is the base rune of the grapheme cluster in this cell.
	Combc []rune // Combc are the combining runes that follow Rune within the cluster.
	Style Style  // Style styles the cell.
	Set   bool   // Set reports whether an element drew this cell.
}

func newFrame(width, height int) Frame {
	cells := frameCellPool.Get().([]FrameCell)
	if cap(cells) < width*height {
		cells = make([]FrameCell, width*height)
	}
	cells = cells[:width*height]
	clear(cells)
	return Frame{
		Width:  width,
		Height: height,
		Cells:  cells,
	}
}

// frameCellPool pools the cell slices of frames presented to screens
// that implement FrameReleaser. The pool reduces GC pressure for
// high-frequency rendering; screens that do not opt in keep the
// fresh-frame behavior.
var frameCellPool = sync.Pool{
	New: func() any {
		return make([]FrameCell, 0, 80*24)
	},
}

// ReleaseFrame returns a frame's cells to the internal pool. It is
// intended for use by a Screen's ReleaseFrame method.
func ReleaseFrame(f Frame) {
	frameCellPool.Put(f.Cells)
}

func (f *Frame) setCell(x, y int, mainc rune, combc []rune, style Style) {
	if x < 0 || x >= f.Width || y < 0 || y >= f.Height {
		return
	}
	f.Cells[y*f.Width+x] = FrameCell{
		Rune:  mainc,
		Combc: combc,
		Style: style,
		Set:   true,
	}
}

// setCursor records a cursor request. Requests outside the frame are
// ignored, so a clipped element never positions the cursor off-screen.
func (f *Frame) setCursor(x, y int) {
	if x < 0 || x >= f.Width || y < 0 || y >= f.Height {
		return
	}
	f.CursorX, f.CursorY = x, y
	f.CursorSet = true
}

// frameCellEqual reports whether two cells hold identical content.
func frameCellEqual(a, b FrameCell) bool {
	return a.Rune == b.Rune && a.Set == b.Set &&
		sameStyle(a.Style, b.Style) && sameCombc(a.Combc, b.Combc)
}

func (f Frame) Equal(o Frame) bool {
	if f.Width != o.Width || f.Height != o.Height || len(f.Cells) != len(o.Cells) {
		return false
	}
	if f.CursorSet != o.CursorSet || f.CursorX != o.CursorX || f.CursorY != o.CursorY {
		return false
	}
	for i := range f.Cells {
		if !frameCellEqual(f.Cells[i], o.Cells[i]) {
			return false
		}
	}
	return true
}

// Dirty reports the maximal horizontal runs of cells that differ between
// f and o, in row-major order; each reported Box is one row high. A
// screen that keeps the last presented frame calls Dirty on the next
// frame and repaints only the reported runs, mirroring damage-based
// rendering in terminal libraries.
func (f Frame) Dirty(o Frame) []Box {
	if f.Width != o.Width || f.Height != o.Height || len(f.Cells) != len(o.Cells) {
		return []Box{{Top: 0, Left: 0, Bottom: f.Height, Right: f.Width}}
	}
	var dirty []Box
	for y := 0; y < f.Height; y++ {
		start := -1
		for x := 0; x < f.Width; x++ {
			idx := y*f.Width + x
			if frameCellEqual(f.Cells[idx], o.Cells[idx]) {
				if start >= 0 {
					dirty = append(dirty, Box{Top: y, Left: start, Bottom: y + 1, Right: x})
					start = -1
				}
				continue
			}
			if start < 0 {
				start = x
			}
		}
		if start >= 0 {
			dirty = append(dirty, Box{Top: y, Left: start, Bottom: y + 1, Right: f.Width})
		}
	}
	return dirty
}

// DirtyRows reports the row indices that differ between f and o, in
// ascending order. A screen that repaints whole rows uses it to avoid
// the run-level detail of Dirty.
func (f Frame) DirtyRows(o Frame) []int {
	return f.DirtyRowsInto(o, nil)
}

// DirtyRowsInto appends the row indices that differ between f and o to
// the provided buffer and returns the extended buffer. A screen that
// repaints whole rows uses it to avoid the per-call allocation of
// DirtyRows by reusing a buffer across presents.
func (f Frame) DirtyRowsInto(o Frame, buf []int) []int {
	if f.Width != o.Width || f.Height != o.Height || len(f.Cells) != len(o.Cells) {
		buf = buf[:0]
		for i := 0; i < f.Height; i++ {
			buf = append(buf, i)
		}
		return buf
	}
	buf = buf[:0]
	for y := 0; y < f.Height; y++ {
		for x := 0; x < f.Width; x++ {
			if !frameCellEqual(f.Cells[y*f.Width+x], o.Cells[y*f.Width+x]) {
				buf = append(buf, y)
				break
			}
		}
	}
	return buf
}

func sameStyle(a, b Style) bool {
	if a == nil || b == nil {
		return a == b
	}
	// The vt style is a pointer, so interface equality is pointer
	// identity; a render pass shares style values, so most comparisons
	// short-circuit here. Equal is called only for distinct style
	// values.
	if a == b {
		return true
	}
	return a.Equal(b)
}

func sameCombc(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Screen is a render target. Screens are independent of the scope and of the
// element model: rendering produces a Frame, and the screen presents it. Any
// backend able to present styled cell grids can be a Screen: a character
// terminal, a web view, or a native widget grid.
//
// A Screen that does not retain the presented frame may implement
// FrameReleaser to return the frame's cells to an internal pool.
type Screen interface {
	Width() int
	Height() int
	Present(Frame)
}

// FrameReleaser is an optional interface a Screen may implement to return
// a presented frame's cells to an internal pool. A screen that implements
// it must not retain the frame after Present returns; the cells may be
// reused by the next render pass.
type FrameReleaser interface {
	ReleaseFrame(Frame)
}
