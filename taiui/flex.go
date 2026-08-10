package taiui

import (
	"fmt"

	"github.com/clipperhouse/displaywidth"
)

var _ Element = _Flex{}

// _Flex lays out children along one axis. Each child receives a share of
// the box proportional to its weight (Weighted, default 1), tiling the
// content area without overlaps or gaps; the last child absorbs rounding.
// The box model (margin, border, padding) behaves as in Rect. It is a
// pure value: specs are interpreted at construction into typed fields,
// and rendering reads those fields.
type _Flex struct {
	elementBase
	axis     axis
	children []Element
	weights  []int
	boxModel boxModel
}

// axis is the layout direction of a flex container.
type axis uint8

const (
	axisHorizontal axis = iota
	axisVertical
)

// Row lays children out left to right. Each child receives a horizontal
// share of the box proportional to its weight, and the box's full height.
func Row(specs ...any) _Flex {
	f := &_Flex{axis: axisHorizontal}
	buildElement(f, specs)
	return *f
}

// Column lays children out top to bottom. Each child receives a vertical
// share of the box proportional to its weight.
func Column(specs ...any) _Flex {
	f := &_Flex{axis: axisVertical}
	buildElement(f, specs)
	return *f
}

func (_Flex) element() {}

func (_Flex) spec()     {}
func (_Weighted) spec() {}

// Weighted gives an element a flex weight. The weight must be positive and
// the element must not be nil.
func Weighted(weight int, e Element) _Weighted {
	if e == nil {
		panic(fmt.Errorf("Weighted: nil element"))
	}
	if weight <= 0 {
		panic(fmt.Errorf("Weighted: bad weight %d", weight))
	}
	return _Weighted{weight: weight, element: e}
}

// _Weighted wraps an element with its flex weight. It is a spec, not an
// element: it only appears inside a Row or Column spec list.
type _Weighted struct {
	weight  int
	element Element
}

func (f *_Flex) applySpec(spec any) {
	if spec == nil {
		return
	}
	switch v := spec.(type) {
	case Specs:
		for _, s := range v {
			f.applySpec(s)
		}
	case _Weighted:
		f.children = append(f.children, v.element)
		f.weights = append(f.weights, v.weight)
	case Element:
		if v != nil {
			f.children = append(f.children, v)
			f.weights = append(f.weights, 1)
		}
	default:
		if f.applyCommonSpec(v) {
			return
		}
		if f.boxModel.applySpec(v) {
			return
		}
		panic(fmt.Errorf("unknown spec %#v", v))
	}
}

func renderFlex(f _Flex, box Box, style Style, draw drawFunc, options displaywidth.Options) {
	box = f.effectiveBox(box)
	style = f.styled(style)

	outer := f.boxModel.outerBox(box)
	content := f.boxModel.contentBox(box)

	total := 0
	for _, w := range f.weights {
		total += w
	}

	// With no children, fill covers the whole outer box, matching Rect's
	// no-children behavior: there is no tiled content area to leave blank.
	if f.fill && total == 0 {
		for y := max(outer.Top, box.Top); y < min(outer.Bottom, box.Bottom); y++ {
			for x := max(outer.Left, box.Left); x < min(outer.Right, box.Right); x++ {
				draw(x, y, ' ', nil, style)
			}
		}
		f.boxModel.drawBorder(outer, style, draw)
		return
	}

	// With fill, track the cells children occupy so the background paints
	// only the ring cells no child drew. A wide grapheme cluster occupies
	// its trailing columns too; fill must not paint over them.
	var marks []bool
	marked := draw
	if f.fill {
		marks = make([]bool, box.Width()*box.Height())
		marked = drawFunc(func(x, y int, mainc rune, combc []rune, st Style) {
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
	}

	if total > 0 {
		start, length := content.Left, content.Width()
		if f.axis == axisVertical {
			start, length = content.Top, content.Height()
		}

		pos := start
		for i, child := range f.children {
			size := length * f.weights[i] / total
			if i == len(f.children)-1 {
				size = start + length - pos // the last child absorbs rounding
			}
			var childBox Box
			if f.axis == axisHorizontal {
				childBox = Box{Top: content.Top, Left: pos, Bottom: content.Bottom, Right: pos + size}
			} else {
				childBox = Box{Top: pos, Left: content.Left, Bottom: pos + size, Right: content.Right}
			}
			renderElement(child, childBox, style, marked, options)
			pos += size
		}
	}

	if f.fill {
		// The content area is fully tiled by the children; fill only the
		// ring formed by the margins, border, and padding that no child
		// occupied, clipped to the element box so a negative margin cannot
		// paint outside it.
		for y := max(outer.Top, box.Top); y < min(outer.Bottom, box.Bottom); y++ {
			for x := max(outer.Left, box.Left); x < min(outer.Right, box.Right); x++ {
				if x >= content.Left && x < content.Right && y >= content.Top && y < content.Bottom {
					continue
				}
				idx := (y-box.Top)*box.Width() + (x - box.Left)
				if !marks[idx] {
					draw(x, y, ' ', nil, style)
				}
			}
		}
	}
	f.boxModel.drawBorder(outer, style, draw)
}
