package taiui

import (
	"github.com/reusee/dscope"
)

const TheoryOfTaiUI = `
taiui theory: UI = RenderFunc(State).
- The UI is a pure function of the state; State is the set of values in the
  dscope Scope. All element dependencies (RenderFunc parameters and spec
  providers) are resolved from the scope, so the rendered UI is fully
  determined by the state.
- Elements are values of distinct types in the same dependency graph as the
  state. A state change recomputes every dependent element definition through
  the scope's machinery, so updates propagate automatically; there is no
  per-element imperative update protocol.
- Rendering is idempotent recomputation: rendering the element tree against
  the scope holding the new state yields the updated UI.
- RenderAll performs BFS: each element's RenderFunc is invoked through
  scope.Call, collecting returned child Elements until exhausted.
- Spec lists (values or functions) are resolved via scope; functions always act
  as injectable initializers, whether or not they are anonymous.
- Rect provides box-model layout (margin + padding) with optional fill.
- Text provides aligned multi-line rendering with per-rune StyleFunc support.
- Style composition uses StyleFunc chaining over a base vt.Style.
- FrameBuffer renders offscreen content into the layout-supplied box. The
  content is a data value, i.e. state: the application builds and mutates it,
  and rendering is a pure read of it. Updating a framebuffer is therefore
  updating state, never an imperative element-update call.
- The exported API is a minimal facade: spec types, constructors, and style
  helpers only; every concept has exactly one way to express it.
`

type Scope = dscope.Scope

type Element interface {
	RenderFunc() any
}

type SetContent func(x int, y int, mainc rune, combc []rune, style Style)

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
