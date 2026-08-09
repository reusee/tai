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
  time. Zero-argument function specs are evaluated eagerly, and each spec is
  interpreted immediately into typed element fields, so rendering reads
  plain data and never parses specs. Unknown specs fail at construction.
  Dynamics that depend on state are expressed as providers in the scope
  that build the element tree.
- The Spec language is a marker-interface protocol for element
  construction: style and layout specs, Specs groups, and elements
  themselves all implement Spec, so spec lists compose and nest. Bare
  strings are a shorthand for text lines only and are not Specs. If and Alt
  compose conditionally.
- Rendering resolves the root from the scope, interprets the element tree
  into a Frame (a styled cell grid), and presents the frame to each screen.
  Elements never call screen methods; any backend able to present cell
  grids can render (character terminals, web views, native UIs).
- Rect provides box-model layout (margin + padding) with optional fill.
- Text provides aligned multi-line rendering with per-rune StyleFunc
  support.
- VerticalScroll renders a child into a virtually unbounded column and
  crops to the visible window, with crop-count indicators.
- FrameBuffer renders offscreen content: the content is data state, and
  rendering is a pure read of it.
- The exported API is a minimal facade: spec types, constructors, and
  style helpers only.
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
	Rune  rune
	Combc []rune
	Style Style
	Set   bool
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

// Screen is a render target. Screens are independent of the scope and of the
// element model: rendering produces a Frame, and the screen presents it. Any
// backend able to present styled cell grids can be a Screen: a character
// terminal, a web view, or a native widget grid.
type Screen interface {
	Width() int
	Height() int
	Present(Frame)
}
