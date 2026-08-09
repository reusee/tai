package taiui

import "fmt"

type OffsetStyleFunc func(int) StyleFunc

var _ Element = _Text{}

type _Text struct{ uiDesc }

func Text(specs ...any) _Text {
	return _Text{uiDesc: newUIDesc(specs)}
}

func (t _Text) RenderFunc() any {
	return func(
		parentBox Box,
		parentStyle Style,
		setContent SetContent,
		scope Scope,
	) {
		box := parentBox
		style := parentStyle
		var lines []string
		align := AlignLeft
		var paddingLeft, paddingRight, paddingTop, paddingBottom int
		var offsetStyleFunc OffsetStyleFunc
		fill := false

		t.iterSpecs(scope, func(v any) {
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
				setContent(left, y, r, nil, s)
				left += RuneDisplayWidth(r)
			}
			if fill {
				for left < box.Right {
					setContent(left, y, ' ', nil, style)
					left++
				}
			}
		}
	}
}
