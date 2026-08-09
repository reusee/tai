package taiui

import (
	"fmt"
	"reflect"

	"github.com/gdamore/tcell/v3/vt"
	"github.com/reusee/dscope"
)

// Render resolves the root element from the scope and renders it to each
// screen. The scope must provide a Root value; each screen receives a Frame
// sized to that screen. Rendering is idempotent recomputation: forking the
// scope with new state re-evaluates the root, and the next Render produces
// the updated UI.
func Render(scope Scope, screens ...Screen) {
	root := dscope.Get[Root](scope)
	if root.Element == nil {
		return
	}
	for _, screen := range screens {
		width := screen.Width()
		height := screen.Height()
		if width <= 0 || height <= 0 {
			continue
		}
		frame := newFrame(width, height)
		box := Box{Top: 0, Left: 0, Bottom: height, Right: width}
		renderElement(root.Element, box, vt.BaseStyle, frame.setCell)
		screen.Present(frame)
	}
}

// drawFunc is the renderer's internal drawing target: it writes one styled
// cell at a coordinate, either into a Frame or into a sparse collector used
// by scroll. Elements never receive a draw target; the renderer interprets
// the element tree and drives the drawing.
type drawFunc func(x, y int, mainc rune, combc []rune, style Style)

func renderElement(e Element, box Box, style Style, draw drawFunc) {
	if e == nil {
		return
	}
	switch e := e.(type) {
	case _Rect:
		renderRect(e, box, style, draw)
	case _Text:
		renderText(e, box, style, draw)
	case _VerticalScroll:
		renderVerticalScroll(e, box, style, draw)
	case _FrameBuffer:
		renderFrameBuffer(e, box, style, draw)
	default:
		panic(fmt.Errorf("unknown element %#v", e))
	}
}

// uiDesc wraps the spec list of an element. Specs are resolved at
// construction time so that every element is a complete value; rendering
// never consults a scope.
type uiDesc struct {
	specs []any
}

// newUIDesc resolves each spec: zero-argument functions are evaluated
// eagerly and their results are processed as specs; any other value is kept
// as-is. Functions with arguments are kept as-is (e.g. StyleFunc, which
// renders as a style transformer).
func newUIDesc(specs []any) uiDesc {
	return uiDesc{specs: resolveSpecs(specs)}
}

func resolveSpecs(specs []any) []any {
	var out []any
	for _, spec := range specs {
		if spec == nil {
			continue
		}
		t := reflect.TypeOf(spec)
		if t.Kind() == reflect.Func && t.NumIn() == 0 {
			res := reflect.ValueOf(spec).Call(nil)
			for _, r := range res {
				out = append(out, resolveSpecs([]any{r.Interface()})...)
			}
			continue
		}
		out = append(out, spec)
	}
	return out
}

func (u uiDesc) iterSpecs(cb func(any)) {
	for _, spec := range u.specs {
		cb(spec)
	}
}

type _Margin []int

func Margin(spec ...int) _Margin { return _Margin(spec) }

type _Padding []int

func Padding(spec ...int) _Padding { return _Padding(spec) }

func applyBoxModel(v []int) (top, right, bottom, left int) {
	switch len(v) {
	case 0:
	case 1:
		top, bottom, left, right = v[0], v[0], v[0], v[0]
	case 2:
		top, bottom, left, right = v[0], v[0], v[1], v[1]
	case 3:
		top, left, right, bottom = v[0], v[1], v[1], v[2]
	case 4:
		top, right, bottom, left = v[0], v[1], v[2], v[3]
	default:
		panic(fmt.Errorf("bad box model spec: %v", v))
	}
	return
}

func applyMargin(v _Margin) (top, right, bottom, left int) {
	return applyBoxModel(v)
}

func applyPadding(v _Padding) (top, right, bottom, left int) {
	return applyBoxModel(v)
}
