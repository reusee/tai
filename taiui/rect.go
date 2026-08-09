package taiui

import "fmt"

var _ Element = _Rect{}

// _Rect is a box with box-model layout and children. It is a pure value:
// specs are interpreted at construction into typed fields, and rendering
// reads those fields.
type _Rect struct {
	elementBase
	children []Element
	margin   [4]int
	padding  [4]int
}

// Rect creates a box element from specs. Specs are interpreted immediately;
// unknown specs panic here, at construction.
func Rect(specs ...any) _Rect {
	r := &_Rect{}
	buildElement(r, specs)
	return *r
}

func (_Rect) element() {}

// applySpec interprets one spec value into _Rect fields.
func (r *_Rect) applySpec(spec any) {
	if spec == nil {
		return
	}
	switch v := spec.(type) {
	case Specs:
		for _, s := range v {
			r.applySpec(s)
		}
	case Element:
		if v != nil {
			r.children = append(r.children, v)
		}
	case _Margin:
		r.margin = applyBoxModel(v)
	case _Padding:
		r.padding = applyBoxModel(v)
	default:
		if r.applyCommonSpec(v) {
			return
		}
		panic(fmt.Errorf("unknown spec %#v", v))
	}
}

func renderRect(r _Rect, box Box, style Style, draw drawFunc) {
	box = r.effectiveBox(box)
	style = r.styled(style)

	l := box.Width() * box.Height()
	if l == 0 {
		return
	}
	marks := make([]bool, l)
	marked := drawFunc(func(x, y int, mainc rune, combc []rune, st Style) {
		idx := (y-box.Top)*box.Width() + (x - box.Left)
		if idx >= 0 && idx < len(marks) {
			marks[idx] = true
		}
		draw(x, y, mainc, combc, st)
	})

	childBox := Box{
		Top:    box.Top + r.margin[0] + r.padding[0],
		Left:   box.Left + r.margin[3] + r.padding[3],
		Right:  box.Right - r.margin[1] - r.padding[1],
		Bottom: box.Bottom - r.margin[2] - r.padding[2],
	}
	for _, child := range r.children {
		renderElement(child, childBox, style, marked)
	}

	if !r.fill {
		return
	}
	for y := box.Top + r.margin[0]; y < box.Bottom-r.margin[2]; y++ {
		for x := box.Left + r.margin[3]; x < box.Right-r.margin[1]; x++ {
			idx := (y-box.Top)*box.Width() + (x - box.Left)
			if !marks[idx] {
				draw(x, y, ' ', nil, style)
			}
		}
	}
}
