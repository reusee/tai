package taiui

import (
	"fmt"
	"math"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3/vt"
)

var _ Element = _VerticalScroll{}

// _VerticalScroll is a scrollable viewport over a child element. It is a
// pure value: specs are interpreted at construction into typed fields,
// and rendering reads those fields.
type _VerticalScroll struct {
	elementBase
	child     Element
	offset    int
	scrollbar bool
}

// VerticalScroll renders the child into a virtually unbounded column and
// crops to the visible window centered on the content row given by offset.
// The view is clamped to the content extent, so an offset beyond the end
// shows the last rows. It accepts the common specs: a Box override, the
// style chain, and Fill, which paints the visible window's background.
// Scrollbar(true) reserves the rightmost column for a thumb indicating the
// view position within the content.
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
		if v.applyCommonSpec(spec) {
			return
		}
		panic(fmt.Errorf("unknown spec %#v", spec))
	}
}

func renderVerticalScroll(v _VerticalScroll, box Box, style Style, draw drawFunc) {
	box = v.effectiveBox(box)
	style = v.styled(style)

	elemBox := Box{
		Left:   box.Left,
		Right:  box.Right,
		Top:    box.Top,
		Bottom: math.MaxInt32,
	}
	maxY := box.Top
	type Cell struct {
		X     int
		Rune  rune
		Combc []rune
		Style Style
	}
	cells := make(map[int][]Cell)
	sub := drawFunc(func(x, y int, mainc rune, combc []rune, st Style) {
		if y > maxY {
			maxY = y
		}
		// Cells are appended in draw order, so a later draw of the same
		// cell wins when the blit below replays the draws in order.
		cells[y] = append(cells[y], Cell{X: x, Rune: mainc, Combc: combc, Style: st})
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

	// With fill, track the window cells the content occupies so the
	// background paints only the gaps. A wide grapheme cluster occupies
	// its trailing columns too; fill must not paint over them.
	var marks []bool
	var options displaywidth.Options
	if v.fill {
		marks = make([]bool, box.Width()*box.Height())
		options = displayWidthOptions()
	}
	numTopCrop := fromY - box.Top
	for i := 0; i < box.Height(); i++ {
		y := fromY + i
		for _, cell := range cells[y] {
			if cell.X >= clipRight {
				continue
			}
			if marks != nil {
				idx := i*box.Width() + (cell.X - box.Left)
				if idx >= 0 && idx < len(marks) {
					marks[idx] = true
					for j := 1; j < clusterWidth(options, cell.Rune, cell.Combc); j++ {
						// The trailing columns stay within the cluster's row.
						if (cell.X-box.Left)+j < box.Width() {
							marks[idx+j] = true
						}
					}
				}
			}
			draw(cell.X, y-numTopCrop, cell.Rune, cell.Combc, cell.Style)
		}
	}
	if marks != nil {
		for i := 0; i < box.Height(); i++ {
			for x := box.Left; x < box.Right; x++ {
				if !marks[i*box.Width()+(x-box.Left)] {
					draw(x, box.Top+i, ' ', nil, style)
				}
			}
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
