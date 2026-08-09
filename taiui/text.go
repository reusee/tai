package taiui

import (
	"fmt"
	"strings"
)

// OffsetStyleFunc styles a text position by its rune offset within the
// line. Offsets count runes, including the combining runes of clusters.
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

	options := displayWidthOptions()
	maxY := box.Bottom - t.padding[2]
	for i, line := range t.lines {
		y := box.Top + t.padding[0] + i
		if y >= maxY {
			break
		}
		left := box.Left + t.padding[3]
		switch t.align {
		case AlignRight:
			left = box.Right - t.padding[1] - options.String(line)
		case AlignCenter:
			left = (box.Left+box.Right)/2 - options.String(line)/2
		}
		runeIdx := 0
		g := options.StringGraphemes(line)
		for g.Next() {
			cluster := g.Value()
			width := g.Width()
			clusterRunes := strings.Count(cluster, "") - 1
			if left < box.Left {
				left += width
				runeIdx += clusterRunes
				continue
			}
			if left >= box.Right {
				break
			}
			mainc, combc := splitCluster(cluster)
			s := style
			if t.offsetStyleFunc != nil {
				s = t.offsetStyleFunc(runeIdx)(s)
			}
			draw(left, y, mainc, combc, s)
			left += width
			runeIdx += clusterRunes
		}
		if t.fill {
			for left < box.Right {
				draw(left, y, ' ', nil, style)
				left++
			}
		}
	}
}

// splitCluster separates a grapheme cluster into its base rune and the
// combining runes that follow it.
func splitCluster(cluster string) (rune, []rune) {
	var base rune
	var combc []rune
	for i, r := range cluster {
		if i == 0 {
			base = r
		} else {
			combc = append(combc, r)
		}
	}
	return base, combc
}
