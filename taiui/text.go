package taiui

import "fmt"

type OffsetStyleFunc func(int) StyleFunc

var _ Element = _Text{}

// _Text is an aligned multi-line text block. It is a pure value: the renderer
// interprets it, and it never touches a screen or a scope.
type _Text struct{ uiDesc }

func Text(specs ...any) _Text {
	return _Text{uiDesc: newUIDesc(specs)}
}

func (_Text) element() {}

func renderText(t _Text, box Box, style Style, draw drawFunc) {
	var lines []string
	align := AlignLeft
	var paddingLeft, paddingRight, paddingTop, paddingBottom int
	var offsetStyleFunc OffsetStyleFunc
	fill := false

	t.iterSpecs(func(v any) {
		if s, ok := applyStyleSpec(style, v); ok {
			style = s
			return
		}
		switch v := v.(type) {
		case Box:
			box = v
		case Fill:
			fill = bool(v)
		case string:
			lines = append(lines, v)
		case []string:
			lines = append(lines, v...)
		case Align:
			align = v
		case _Padding:
			paddingTop, paddingRight, paddingBottom, paddingLeft = applyPadding(v)
		case OffsetStyleFunc:
			offsetStyleFunc = v
		default:
			panic(fmt.Errorf("unknown spec %#v", v))
		}
	})

	maxY := box.Bottom - paddingBottom
	for i, line := range lines {
		runes := []rune(line)
		var left int
		switch align {
		case AlignLeft:
			left = box.Left + paddingLeft
		case AlignRight:
			left = box.Right - paddingRight - RunesDisplayWidth(runes)
		case AlignCenter:
			left = (box.Left+box.Right)/2 - RunesDisplayWidth(runes)/2
		}
		for left < box.Left && len(runes) > 0 {
			r := runes[0]
			runes = runes[1:]
			left += RuneDisplayWidth(r)
		}
		y := box.Top + paddingTop + i
		for runeIdx, r := range runes {
			if left >= box.Right {
				break
			}
			if y > maxY {
				continue
			}
			s := style
			if offsetStyleFunc != nil {
				s = offsetStyleFunc(runeIdx)(s)
			}
			draw(left, y, r, nil, s)
			left += RuneDisplayWidth(r)
		}
		if fill {
			for left < box.Right {
				draw(left, y, ' ', nil, style)
				left++
			}
		}
	}
}
