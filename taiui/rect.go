package taiui

import (
	"fmt"

	"github.com/clipperhouse/displaywidth"
)

var _ Element = _Rect{}

// _Rect is a box with box-model layout (margin, border, padding) and
// children. It is a pure value: specs are interpreted at construction
// into typed fields, and rendering reads those fields.
type _Rect struct {
	elementBase
	children []Element
	boxModel boxModel
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
	default:
		if r.applyCommonSpec(v) {
			return
		}
		if r.boxModel.applySpec(v) {
			return
		}
		panic(fmt.Errorf("unknown spec %#v", v))
	}
}

func renderRect(r _Rect, box Box, style Style, draw drawFunc, options displaywidth.Options) {
	box = r.effectiveBox(box)
	style = r.styled(style)

	l := box.Width() * box.Height()
	if l == 0 {
		return
	}

	outer := r.boxModel.outerBox(box)
	content := r.boxModel.contentBox(box)
	if !r.fill {
		for _, child := range r.children {
			renderElement(child, content, style, draw, options)
		}
		r.boxModel.drawBorder(outer, style, draw)
		return
	}

	// With no children the whole box is background; paint it directly
	// without the marks tracking.
	if len(r.children) == 0 {
		for y := max(outer.Top, box.Top); y < min(outer.Bottom, box.Bottom); y++ {
			for x := max(outer.Left, box.Left); x < min(outer.Right, box.Right); x++ {
				draw(x, y, ' ', nil, style)
			}
		}
		r.boxModel.drawBorder(outer, style, draw)
		return
	}

	// With fill, track the cells children occupy so the background paints
	// only the gaps. A wide grapheme cluster occupies its trailing columns
	// too; fill must not paint over them.
	marks := make([]bool, l)
	marked := drawFunc(func(x, y int, mainc rune, combc []rune, st Style) {
		idx := (y-box.Top)*box.Width() + (x - box.Left)
		if idx >= 0 && idx < len(marks) {
			marks[idx] = true
			for i := 1; i < clusterWidth(options, mainc, combc); i++ {
				// The trailing columns stay within the cluster's row.
				if (x-box.Left)+i < box.Width() {
					marks[idx+i] = true
				}
			}
		}
		draw(x, y, mainc, combc, st)
	})
	for _, child := range r.children {
		renderElement(child, content, style, marked, options)
	}

	for y := max(outer.Top, box.Top); y < min(outer.Bottom, box.Bottom); y++ {
		for x := max(outer.Left, box.Left); x < min(outer.Right, box.Right); x++ {
			idx := (y-box.Top)*box.Width() + (x - box.Left)
			if !marks[idx] {
				draw(x, y, ' ', nil, style)
			}
		}
	}
	r.boxModel.drawBorder(outer, style, draw)
}
