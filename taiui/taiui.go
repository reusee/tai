package taiui

import (
	"github.com/reusee/dscope"
)

const TheoryOfTaiUI = `
taiui: a declarative TUI framework built on dscope dependency injection.
- Elements declare rendering logic via RenderFunc returning injectable functions.
- RenderAll performs BFS: each element's RenderFunc is invoked through scope.Call,
  collecting returned child Elements until exhausted.
- Spec lists (values or functions) are resolved via scope; functions always act as
  injectable initializers, whether or not they are anonymous.
- Rect provides box-model layout (margin + padding) with optional fill.
- Text provides aligned multi-line rendering with per-rune StyleFunc support.
- Style composition uses StyleFunc chaining over a base vt.Style.
- FrameBuffer caches rendered output to avoid redundant computation.
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
