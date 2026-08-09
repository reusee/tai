package taiui

import (
	"fmt"
	"math"

	"github.com/gdamore/tcell/v3/vt"
)

type _VerticalScroll struct {
	element Element
	offset  int
}

func VerticalScroll(e Element, offset int) _VerticalScroll {
	return _VerticalScroll{element: e, offset: offset}
}

var _ Element = _VerticalScroll{}

func (v _VerticalScroll) RenderFunc() any {
	return func(
		box Box,
		setContent SetContent,
		scope Scope,
		style Style,
	) {
		elemBox := Box{
			Left:   box.Left,
			Right:  box.Right,
			Top:    box.Top,
			Bottom: math.MaxInt32,
		}
		maxY := box.Top
		type Cell struct {
			Rune  rune
			Style Style
		}
		cells := make(map[int]map[int]Cell)
		set := SetContent(func(x, y int, mainc rune, combc []rune, st Style) {
			if y > maxY {
				maxY = y
			}
			line, ok := cells[y]
			if !ok {
				line = make(map[int]Cell)
				cells[y] = line
			}
			line[x] = Cell{Rune: mainc, Style: st}
		})
		RenderAll(scope.Fork(&elemBox, &set), v.element)
		fromY := max(box.Top+v.offset-box.Height()/2, box.Top)
		numTopCrop := fromY - box.Top
		for i := 0; i < box.Height(); i++ {
			y := fromY + i
			for x, cell := range cells[y] {
				setContent(x, y-numTopCrop, cell.Rune, nil, cell.Style)
			}
		}
		numBottomCrop := maxY - (fromY + box.Height()) + 1
		if numTopCrop > 0 {
			s := withAttrOn(DarkerOrLighterStyle(style, 15), true, vt.Bold)
			for i, r := range fmt.Sprintf(" %d.. ", numTopCrop) {
				setContent(box.Left+i, box.Top, r, nil, s)
			}
		}
		if numBottomCrop > 0 {
			s := withAttrOn(DarkerOrLighterStyle(style, 15), true, vt.Bold)
			for i, r := range fmt.Sprintf(" %d.. ", numBottomCrop) {
				setContent(box.Left+i, box.Bottom-1, r, nil, s)
			}
		}
	}
}
