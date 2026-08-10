package taiui

import (
	"github.com/reusee/dscope"
)

const TheoryOfTaiUI = `
taiui theory: UI = pure Element value derived from state.
- The scope stores state only: data state plus the root UI state (a Root
  value wrapping the root element). Render context (boxes, styles, draw
  callbacks) is never stored in the scope; screens are never bound in the
  scope.
- A state change is a scope fork: providers re-evaluate, the root element
  changes, and the next render reflects the change. There is no imperative
  element-update protocol.
- Elements are pure values: constructors resolve spec lists at construction
  time. Zero-argument function specs are evaluated eagerly, and each result
  is itself resolved as specs, so nested zero-argument functions expand
  recursively. Each spec is interpreted immediately into typed element
  fields, so rendering reads plain data and never parses specs. Unknown
  specs fail at construction. Dynamics that depend on state are expressed
  as providers in the scope that build the element tree.
- The Spec language is a marker-interface protocol for element
  construction: style and layout specs, Specs groups, and elements
  themselves all implement Spec, so spec lists compose and nest. Bare
  strings are a shorthand for text lines only and are not Specs. If and Alt
  compose conditionally.
- Rendering resolves the root from the scope, interprets the element tree
  into a Frame (a styled cell grid), and presents the frame to each screen.
  A nil root element renders an empty frame, clearing every screen. Each
  render pass allocates a fresh frame per screen; frames are never reused,
  because a screen may retain the frame it presented. Elements never call
  screen methods; any backend able to present cell grids can render
  (character terminals, web views, native UIs). Frame.Equal lets a screen
  detect an unchanged frame and skip repainting; Frame.Dirty reports the
  runs of changed cells so a screen can repaint only the damaged regions,
  mirroring change-based rendering in terminal libraries.
- Rect provides box-model layout (margin, border, and padding) with
  optional fill. The border is a one-cell ring between margin and padding
  that shrinks the content box by one cell per side; Fill paints a
  background in the box cells that no child occupies, so children render
  over it and wide grapheme clusters keep their trailing columns. The
  border draws independently of fill and stays visible without a painted
  background. A content box whose border and padding exceed the box
  dimensions has negative size; rendering treats it as empty and never
  leaves the element box.
- Row and Column provide flex layout along their axis: each child receives
  a share of the box proportional to its Weighted weight (default 1),
  tiling the content area without overlaps or gaps; the last child absorbs
  rounding. The box model and fill behave as in Rect, with fill covering
  the cells no child occupied: the ring around the tiled content, or the
  whole outer box when there are no children.
- Text provides aligned multi-line rendering with per-position StyleFunc
  support. Lines are segmented into grapheme clusters (uax29): a cluster
  renders as one cell carrying its base and combining runes, and advances
  by its display width, so combining sequences and ZWJ emoji occupy their
  real columns. Width honors RUNEWIDTH_EASTASIAN for ambiguous runes. With
  Wrap, lines word-wrap to the box width: breaks fall at space runs,
  words wider than the box hard-break at cluster boundaries, and a cluster
  never splits across lines. Left, right, and center alignment are
  relative to the padded content area; a centered line rounds with the
  conventional (width-len)/2 rule, placing the extra column on the right.
  Alignments apply per physical line, so wrapped lines align
  independently. Fill paints the content cells the text does not occupy,
  including the residual gaps left by clusters clipped at either edge.
- VerticalScroll renders a child into a virtually unbounded column and
  crops to the visible window, clamping the view to the content extent.
  Content is clipped to the window on both edges: cells drawn outside the
  window are dropped, and a cluster that would extend past the right edge
  is not drawn, so content never spills onto the screen. It accepts the
  common specs: a Box override, the style chain, and Fill, which paints
  the visible window's unoccupied cells. Crop-count indicators at the
  window edges, and an optional Scrollbar thumb at the right edge, draw
  over the fill.
- FrameBuffer renders offscreen content: the content is data state, and
  rendering is a pure read of it. Rendering snapshots the visible cells
  under the read lock, then draws outside the lock, so a concurrent
  writer is blocked only for the snapshot, never for the draw.
- The exported API is a minimal facade: spec types, constructors, and
  style helpers only. Color specs cover the foreground, the background,
  and the underline color; attr and underline-style specs cover the VT
  attribute set and its underline variants.
`

type Scope = dscope.Scope

// Element is a pure, screen-independent description of UI state.
// Implementations are data values: they describe what to render and never
// interact with a screen or a scope.
type Element interface {
	Spec
	element()
}

// Root is the root UI state in the scope: a wrapper around the root UI
// element. The scope must provide exactly one Root value; Render resolves it
// and renders its element to each screen. When the scope is forked with new
// state, the Root provider re-evaluates, so the next Render reflects the
// change.
type Root struct {
	Element Element
}

type Box struct {
	Top    int
	Left   int
	Bottom int
	Right  int
}

func (b Box) Width() int  { return max(b.Right-b.Left, 0) }
func (b Box) Height() int { return max(b.Bottom-b.Top, 0) }

// UnderlineColor sets the color of the underline. It is visible only when
// the underline is on.
type UnderlineColor Color

type Align uint8

const (
	AlignLeft Align = iota
	AlignRight
	AlignCenter
)

type (
	BGColor   Color
	FGColor   Color
	Bold      bool
	Underline bool
	Fill      bool
)

// Frame is the render output: a styled cell grid. Rendering interprets the
// element tree into a frame, and screens present frames.
type Frame struct {
	Width  int
	Height int
	Cells  []FrameCell
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
	return Frame{
		Width:  width,
		Height: height,
		Cells:  make([]FrameCell, width*height),
	}
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

// frameCellEqual reports whether two cells hold identical content.
func frameCellEqual(a, b FrameCell) bool {
	return a.Rune == b.Rune && a.Set == b.Set &&
		sameStyle(a.Style, b.Style) && sameCombc(a.Combc, b.Combc)
}

// Equal reports whether f and o hold identical cells. Screens use it to
// skip presenting an unchanged frame, mirroring change-based rendering in
// terminal libraries.
func (f Frame) Equal(o Frame) bool {
	if len(f.Cells) != len(o.Cells) {
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

func sameStyle(a, b Style) bool {
	if a == nil || b == nil {
		return a == b
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
type Screen interface {
	Width() int
	Height() int
	Present(Frame)
}
