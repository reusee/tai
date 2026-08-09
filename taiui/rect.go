package taiui

import "fmt"

var _ Element = _Rect{}

type _Rect struct{ uiDesc }

func Rect(specs ...any) _Rect {
	return _Rect{uiDesc: newUIDesc(specs)}
}

func (r _Rect) RenderFunc() any {
	return func(
		scope Scope,
		parentBox Box,
		parentStyle Style,
		setContent SetContent,
	) {
		box := parentBox
		style := parentStyle
		var children []Element
		var marginTop, marginRight, marginBottom, marginLeft int
		var paddingTop, paddingRight, paddingBottom, paddingLeft int
		fill := false

		r.iterSpecs(scope, func(v any) {
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
		set := SetContent(func(x, y int, mainc rune, combc []rune, st Style) {
			idx := (y-box.Top)*box.Width() + (x - box.Left)
			if idx >= 0 && idx < len(marks) {
				marks[idx] = true
			}
			setContent(x, y, mainc, combc, st)
		})

		fg := style.Fg()
		bg := style.Bg()
		childBox := Box{
			Top:    box.Top + marginTop + paddingTop,
			Left:   box.Left + marginLeft + paddingLeft,
			Right:  box.Right - marginRight - paddingRight,
			Bottom: box.Bottom - marginBottom - paddingBottom,
		}
		fgColor := FGColor(fg)
		bgColor := BGColor(bg)
		childScope := scope.Fork(
			&childBox,
			&set,
			&style,
			&fgColor,
			&bgColor,
		)
		RenderAll(childScope, children...)

		if fill {
			for y := box.Top + marginTop; y < box.Bottom-marginBottom; y++ {
				for x := box.Left + marginLeft; x < box.Right-marginRight; x++ {
					idx := (y-box.Top)*box.Width() + (x - box.Left)
					if !marks[idx] {
						setContent(x, y, ' ', nil, style)
					}
				}
			}
		}
	}
}
