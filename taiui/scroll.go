package taiui

import (
	"fmt"
	"math"

	"github.com/gdamore/tcell/v3/vt"
)

var _ Element = _VerticalScroll{}

type _VerticalScroll struct {
	child  Element
	offset int
}

func VerticalScroll(e Element, offset int) _VerticalScroll {
	return _VerticalScroll{child: e, offset: offset}
}

func (_VerticalScroll) element() {}

func renderVerticalScroll(v _VerticalScroll, box Box, style Style, draw drawFunc) {
	elemBox := Box{
		Left:   box.Left,
		Right:  box.Right,
		Top:    box.Top,
		Bottom: math.MaxInt32,
	}
	maxY := box.Top
	type Cell struct {
		Rune  rune
		Combc []rune
		Style Style
	}
	cells := make(map[int]map[int]Cell)
	sub := drawFunc(func(x, y int, mainc rune, combc []rune, st Style) {
		if y > maxY {
			maxY = y
		}
		line, ok := cells[y]
		if !ok {
			line = make(map[int]Cell)
			cells[y] = line
		}
		line[x] = Cell{Rune: mainc, Combc: combc, Style: st}
	})
	renderElement(v.child, elemBox, style, sub)
	fromY := max(box.Top+v.offset-box.Height()/2, box.Top)
	numTopCrop := fromY - box.Top
	for i := 0; i < box.Height(); i++ {
		y := fromY + i
		for x, cell := range cells[y] {
			draw(x, y-numTopCrop, cell.Rune, cell.Combc, cell.Style)
		}
	}
	numBottomCrop := maxY - (fromY + box.Height()) + 1
	if numTopCrop > 0 {
		s := withAttrOn(DarkerOrLighterStyle(style, 15), true, vt.Bold)
		for i, r := range fmt.Sprintf(" %d.. ", numTopCrop) {
			draw(box.Left+i, box.Top, r, nil, s)
		}
	}
	if numBottomCrop > 0 {
		s := withAttrOn(DarkerOrLighterStyle(style, 15), true, vt.Bold)
		for i, r := range fmt.Sprintf(" %d.. ", numBottomCrop) {
			draw(box.Left+i, box.Bottom-1, r, nil, s)
		}
	}
}
