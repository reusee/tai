package taiui

import "fmt"

var _ Element = _Rect{}

// _Rect is a box with box-model layout and children. It is a pure value: the
// renderer interprets it, and it never touches a screen or a scope.
type _Rect struct{ uiDesc }

func Rect(specs ...any) _Rect {
	return _Rect{uiDesc: newUIDesc(specs)}
}

func (_Rect) element() {}

func renderRect(r _Rect, box Box, style Style, draw drawFunc) {
	var children []Element
	var marginTop, marginRight, marginBottom, marginLeft int
	var paddingTop, paddingRight, paddingBottom, paddingLeft int
	fill := false

	r.iterSpecs(func(v any) {
		if s, ok := applyStyleSpec(style, v); ok {
			style = s
			return
		}
		switch v := v.(type) {
		case Box:
			box = v
		case Fill:
			fill = bool(v)
		case Element:
			if v != nil {
				children = append(children, v)
			}
		case []Element:
			for _, e := range v {
				if e != nil {
					children = append(children, e)
				}
			}
		case _Margin:
			marginTop, marginRight, marginBottom, marginLeft = applyMargin(v)
		case _Padding:
			paddingTop, paddingRight, paddingBottom, paddingLeft = applyPadding(v)
		default:
			panic(fmt.Errorf("unknown spec %#v", v))
		}
	})

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
		Top:    box.Top + marginTop + paddingTop,
		Left:   box.Left + marginLeft + paddingLeft,
		Right:  box.Right - marginRight - paddingRight,
		Bottom: box.Bottom - marginBottom - paddingBottom,
	}
	for _, child := range children {
		renderElement(child, childBox, style, marked)
	}

	if fill {
		for y := box.Top + marginTop; y < box.Bottom-marginBottom; y++ {
			for x := box.Left + marginLeft; x < box.Right-marginRight; x++ {
				idx := (y-box.Top)*box.Width() + (x - box.Left)
				if !marks[idx] {
					draw(x, y, ' ', nil, style)
				}
			}
		}
	}
}
