package taiui

import "fmt"

type OffsetStyleFunc func(int) StyleFunc

var _ Element = _Text{}

// _Text is an aligned multi-line text block. It is a pure value: specs are
// interpreted at construction into typed fields, and rendering reads those
// fields.
type _Text struct {
	elementBase
	lines           []string
	align           Align
	padding         [4]int
	offsetStyleFunc OffsetStyleFunc
}

// Text creates a text element from specs. Bare strings and []string values
// are accepted as shorthands for lines; all other specs are interpreted as
// element specs. Unknown specs panic here, at construction.
func Text(specs ...any) _Text {
	t := &_Text{}
	buildElement(t, specs)
	return *t
}

func (_Text) element() {}

// applySpec interprets one spec value into _Text fields.
func (t *_Text) applySpec(spec any) {
	if spec == nil {
		return
	}
	switch v := spec.(type) {
	case Specs:
		for _, s := range v {
			t.applySpec(s)
		}
	case string:
		t.lines = append(t.lines, v)
	case []string:
		t.lines = append(t.lines, v...)
	case Align:
		t.align = v
	case _Padding:
		t.padding = applyBoxModel(v)
	case OffsetStyleFunc:
		t.offsetStyleFunc = v
	default:
		if t.applyCommonSpec(v) {
			return
		}
		panic(fmt.Errorf("unknown spec %#v", v))
	}
}

func renderText(t _Text, box Box, style Style, draw drawFunc) {
	box = t.effectiveBox(box)
	style = t.styled(style)

	maxY := box.Bottom - t.padding[2]
	for i, line := range t.lines {
		runes := []rune(line)
		var left int
		switch t.align {
		case AlignLeft:
			left = box.Left + t.padding[3]
		case AlignRight:
			left = box.Right - t.padding[1] - RunesDisplayWidth(runes)
		case AlignCenter:
			left = (box.Left+box.Right)/2 - RunesDisplayWidth(runes)/2
		}
		for left < box.Left && len(runes) > 0 {
			r := runes[0]
			runes = runes[1:]
			left += RuneDisplayWidth(r)
		}
		y := box.Top + t.padding[0] + i
		for runeIdx, r := range runes {
			if left >= box.Right {
				break
			}
			if y > maxY {
				continue
			}
			s := style
			if t.offsetStyleFunc != nil {
				s = t.offsetStyleFunc(runeIdx)(s)
			}
			draw(left, y, r, nil, s)
			left += RuneDisplayWidth(r)
		}
		if t.fill {
			for left < box.Right {
				draw(left, y, ' ', nil, style)
				left++
			}
		}
	}
}
