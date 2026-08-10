package taiui

import (
	"fmt"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3/vt"
	"github.com/reusee/dscope"
)

func Render(scope Scope, screens ...Screen) {
	root := dscope.Get[Root](scope)
	options := DisplayWidthOptions()
	for _, screen := range screens {
		width := screen.Width()
		height := screen.Height()
		if width <= 0 || height <= 0 {
			continue
		}
		frame := newFrame(width, height)
		box := Box{Top: 0, Left: 0, Bottom: height, Right: width}
		renderElement(root.Element, box, vt.BaseStyle, frame.setCell, frame.setCursor, options)
		screen.Present(frame)
		if releaser, ok := screen.(FrameReleaser); ok {
			releaser.ReleaseFrame(frame)
		}
	}
}

// drawFunc is the renderer's internal drawing target: it writes one styled
// cell at a coordinate, either into a Frame or into a sparse collector used
// by scroll. Elements never receive a draw target; the renderer interprets
// the element tree and drives the drawing.
type drawFunc func(x, y int, mainc rune, combc []rune, style Style)

// cursorFunc records a cursor request during rendering. Elements call it
// to place the terminal cursor; the renderer stores the last request in
// the Frame.
type cursorFunc func(x, y int)

func renderElement(e Element, box Box, style Style, draw drawFunc, cursor cursorFunc, options displaywidth.Options) {
	if e == nil {
		return
	}
	// An empty box has no visible area: no child is rendered and no
	// cursor is recorded, so off-screen elements cost nothing.
	if box.Width() <= 0 || box.Height() <= 0 {
		return
	}
	switch e := e.(type) {
	case _Rect:
		renderRect(e, box, style, draw, cursor, options)
	case _Text:
		renderText(e, box, style, draw, cursor, options)
	case _VerticalScroll:
		renderVerticalScroll(e, box, style, draw, cursor, options)
	case _Flex:
		renderFlex(e, box, style, draw, cursor, options)
	case _FrameBuffer:
		renderFrameBuffer(e, box, style, draw, cursor, options)
	case _Overlay:
		renderOverlay(e, box, style, draw, cursor, options)
	case _List:
		renderList(e, box, style, draw, cursor, options)
	default:
		panic(fmt.Errorf("unknown element %#v", e))
	}
}

func markedDraw(draw drawFunc, marks []bool, box Box, options displaywidth.Options) drawFunc {
	return drawFunc(func(x, y int, mainc rune, combc []rune, st Style) {
		idx := (y-box.Top)*box.Width() + (x - box.Left)
		if idx >= 0 && idx < len(marks) {
			marks[idx] = true
			for i := 1; i < ClusterWidth(options, mainc, combc); i++ {
				// The trailing columns stay within the cluster's row.
				if (x-box.Left)+i < box.Width() {
					marks[idx+i] = true
				}
			}
		}
		draw(x, y, mainc, combc, st)
	})
}

type _Margin []int

// Margin sets the box margin of a box-model element. It takes one to four
// values, following the CSS convention: one applies to all sides, two set
// vertical and horizontal, three set top, horizontal, and bottom, and four
// set top, right, bottom, and left.
func Margin(spec ...int) _Margin { return _Margin(spec) }

type _Padding []int

// Padding sets the box padding of a box-model element. It takes one to
// four values, following the CSS convention: one applies to all sides, two
// set vertical and horizontal, three set top, horizontal, and bottom, and
// four set top, right, bottom, and left.
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
