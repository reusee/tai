package taiui

import (
	"fmt"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3/vt"
	"github.com/reusee/dscope"
)

// Render resolves the root element from the scope and renders it to each
// screen. The scope must provide a Root value; each screen receives a Frame
// sized to that screen. Rendering is idempotent recomputation: forking the
// scope with new state re-evaluates the root, and the next Render produces
// the updated UI. A nil root element renders an empty frame, so a state
// with no UI clears every screen.
func Render(scope Scope, screens ...Screen) {
	root := dscope.Get[Root](scope)
	options := displayWidthOptions()
	for _, screen := range screens {
		width := screen.Width()
		height := screen.Height()
		if width <= 0 || height <= 0 {
			continue
		}
		frame := newFrame(width, height)
		box := Box{Top: 0, Left: 0, Bottom: height, Right: width}
		renderElement(root.Element, box, vt.BaseStyle, frame.setCell, options)
		screen.Present(frame)
	}
}

// drawFunc is the renderer's internal drawing target: it writes one styled
// cell at a coordinate, either into a Frame or into a sparse collector used
// by scroll. Elements never receive a draw target; the renderer interprets
// the element tree and drives the drawing.
type drawFunc func(x, y int, mainc rune, combc []rune, style Style)

// renderElement dispatches one element to its render function. The options
// are the display-width options for this render pass, derived once by
// Render and shared by every element that measures width.
func renderElement(e Element, box Box, style Style, draw drawFunc, options displaywidth.Options) {
	if e == nil {
		return
	}
	switch e := e.(type) {
	case _Rect:
		renderRect(e, box, style, draw, options)
	case _Text:
		renderText(e, box, style, draw, options)
	case _VerticalScroll:
		renderVerticalScroll(e, box, style, draw, options)
	case _Flex:
		renderFlex(e, box, style, draw, options)
	case _FrameBuffer:
		renderFrameBuffer(e, box, style, draw)
	default:
		panic(fmt.Errorf("unknown element %#v", e))
	}
}

type _Margin []int

func Margin(spec ...int) _Margin { return _Margin(spec) }

type _Padding []int

func Padding(spec ...int) _Padding { return _Padding(spec) }

// applyBoxModel maps a 0..4 element margin or padding list to the
// (top, right, bottom, left) order.
func applyBoxModel(v []int) (m [4]int) {
	switch len(v) {
	case 0:
	case 1:
		m[0], m[1], m[2], m[3] = v[0], v[0], v[0], v[0]
	case 2:
		m[0], m[1], m[2], m[3] = v[0], v[1], v[0], v[1]
	case 3:
		m[0], m[1], m[2], m[3] = v[0], v[1], v[1], v[2]
	case 4:
		m[0], m[1], m[2], m[3] = v[0], v[1], v[2], v[3]
	default:
		panic(fmt.Errorf("bad box model spec: %v", v))
	}
	return
}
