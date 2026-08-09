package taiui

import (
	"fmt"
	"math"

	"github.com/gdamore/tcell/v3/vt"
)

var _ Element = _VerticalScroll{}

type _VerticalScroll struct {
	child     Element
	offset    int
	scrollbar bool
}

// VerticalScroll renders the child into a virtually unbounded column and
// crops to the visible window centered on the content row given by offset.
// The view is clamped to the content extent, so an offset beyond the end
// shows the last rows. Scrollbar(true) reserves the rightmost column for a
// thumb indicating the view position within the content.
func VerticalScroll(e Element, offset int, specs ...any) _VerticalScroll {
	v := &_VerticalScroll{child: e, offset: offset}
	buildElement(v, specs)
	return *v
}

func (_VerticalScroll) element() {}

// Scrollbar toggles the VerticalScroll thumb indicator.
type Scrollbar bool

func (Scrollbar) spec() {}

func (v *_VerticalScroll) applySpec(spec any) {
	if spec == nil {
		return
	}
	switch spec := spec.(type) {
	case Specs:
		for _, s := range spec {
			v.applySpec(s)
		}
	case Scrollbar:
		v.scrollbar = bool(spec)
	default:
		panic(fmt.Errorf("unknown spec %#v", spec))
	}
}

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

	// Clamp the view window to the content extent: it never starts before
	// the box top, nor past the last visible content row.
	contentHeight := maxY - box.Top + 1
	fromY := max(box.Top+v.offset-box.Height()/2, box.Top)
	maxFromY := maxY - box.Height() + 1
	if maxFromY < box.Top {
		maxFromY = box.Top
	}
	if fromY > maxFromY {
		fromY = maxFromY
	}

	clipRight := box.Right
	showScrollbar := v.scrollbar && contentHeight > box.Height()
	if showScrollbar {
		clipRight = box.Right - 1
	}

	numTopCrop := fromY - box.Top
	for i := 0; i < box.Height(); i++ {
		y := fromY + i
		for x, cell := range cells[y] {
			if x >= clipRight {
				continue
			}
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
	if showScrollbar {
		// The thumb position maps the visible window onto the track.
		thumbSize := max(1, box.Height()*box.Height()/contentHeight)
		thumbY := (fromY - box.Top) * (box.Height() - thumbSize) / (contentHeight - box.Height())
		s := withAttrOn(DarkerOrLighterStyle(style, 15), true, vt.Bold)
		for i := 0; i < thumbSize; i++ {
			draw(box.Right-1, box.Top+thumbY+i, '█', nil, s)
		}
	}
}
