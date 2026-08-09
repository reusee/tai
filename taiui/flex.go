package taiui

import "fmt"

var _ Element = _Flex{}

// _Flex lays out children along one axis. Each child receives a share of
// the box proportional to its weight (Weighted, default 1), tiling the
// content area without overlaps or gaps; the last child absorbs rounding.
// It is a pure value: specs are interpreted at construction into typed
// fields, and rendering reads those fields.
type _Flex struct {
	elementBase
	axis     axis
	children []Element
	weights  []int
	margin   [4]int
	padding  [4]int
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
	case _Margin:
		f.margin = applyBoxModel(v)
	case _Padding:
		f.padding = applyBoxModel(v)
	default:
		if f.applyCommonSpec(v) {
			return
		}
		panic(fmt.Errorf("unknown spec %#v", v))
	}
}

func renderFlex(f _Flex, box Box, style Style, draw drawFunc) {
	box = f.effectiveBox(box)
	style = f.styled(style)

	content := Box{
		Top:    box.Top + f.margin[0] + f.padding[0],
		Left:   box.Left + f.margin[3] + f.padding[3],
		Right:  box.Right - f.margin[1] - f.padding[1],
		Bottom: box.Bottom - f.margin[2] - f.padding[2],
	}

	total := 0
	for _, w := range f.weights {
		total += w
	}
	if total == 0 {
		return
	}

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
		renderElement(child, childBox, style, draw)
		pos += size
	}

	if !f.fill {
		return
	}
	// The content area is fully tiled by the children; fill only the border
	// formed by the margins and padding.
	for y := box.Top + f.margin[0]; y < box.Bottom-f.margin[2]; y++ {
		for x := box.Left + f.margin[3]; x < box.Right-f.margin[1]; x++ {
			if x >= content.Left && x < content.Right && y >= content.Top && y < content.Bottom {
				continue
			}
			draw(x, y, ' ', nil, style)
		}
	}
}
