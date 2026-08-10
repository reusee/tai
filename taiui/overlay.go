package taiui

import (
	"fmt"

	"github.com/clipperhouse/displaywidth"
)

const TheoryOfOverlay = `
taiui overlay theory:
- Overlay stacks children in order, each into the full box; later
  children draw over earlier ones. Fill paints the background in the
  cells no child occupied, matching Rect's fill semantics: a wide
  grapheme cluster's trailing columns are marked, so fill never paints
  over them.
- Overlay enables modals and popups: the application derives the overlay
  from state, so a modal is part of the element tree, not a separate
  layer.
`

var _ Element = _Overlay{}

// _Overlay stacks children in order, each into the full box. It is a
// pure value: specs are interpreted at construction into typed fields,
// and rendering reads those fields.
type _Overlay struct {
	elementBase
	children []Element
}

// Overlay stacks children in order, each into the full box; later
// children draw over earlier ones. It accepts the common specs: a Box
// override, the style chain, and Fill, which paints the background in
// the cells no child occupied.
func Overlay(specs ...any) _Overlay {
	o := &_Overlay{}
	buildElement(o, specs)
	return *o
}

func (_Overlay) element() {}

func (o *_Overlay) applySpec(spec any) {
	if spec == nil {
		return
	}
	switch v := spec.(type) {
	case Specs:
		for _, s := range v {
			o.applySpec(s)
		}
	case Element:
		if v != nil {
			o.children = append(o.children, v)
		}
	default:
		if o.applyCommonSpec(v) {
			return
		}
		panic(fmt.Errorf("unknown spec %#v", v))
	}
}

func renderOverlay(o _Overlay, box Box, style Style, draw drawFunc, cursor cursorFunc, options displaywidth.Options) {
	box = o.effectiveBox(box)
	style = o.styled(style)

	if !o.fill {
		for _, child := range o.children {
			renderElement(child, box, style, draw, cursor, options)
		}
		return
	}

	// With no children the whole box is background; paint it directly
	// without the marks tracking.
	if len(o.children) == 0 {
		for y := box.Top; y < box.Bottom; y++ {
			for x := box.Left; x < box.Right; x++ {
				draw(x, y, ' ', nil, style)
			}
		}
		return
	}

	// With fill, track the cells children occupy so the background paints
	// only the gaps. A wide grapheme cluster occupies its trailing columns
	// too; fill must not paint over them.
	marks := getMarks(box.Width() * box.Height())
	defer putMarks(marks)
	marked := markedDraw(draw, marks, box, options)
	for _, child := range o.children {
		renderElement(child, box, style, marked, cursor, options)
	}
	for y := box.Top; y < box.Bottom; y++ {
		for x := box.Left; x < box.Right; x++ {
			idx := (y-box.Top)*box.Width() + (x - box.Left)
			if !marks[idx] {
				draw(x, y, ' ', nil, style)
			}
		}
	}
}
