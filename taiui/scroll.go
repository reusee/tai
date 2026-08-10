package taiui

import (
	"fmt"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3/vt"
)

var _ Element = _VerticalScroll{}

// maxScrollContentHeight bounds the virtual column a VerticalScroll renders
// its child into. Box-driven children (Rect, Row, Column) render their whole
// box, so an unbounded column would make them loop over unbounded space;
// the bound keeps such rendering finite. Real scrollable content rarely
// approaches the bound.
const maxScrollContentHeight = 1 << 14

// scrollCell is one content cell collected while rendering the child of a
// VerticalScroll. Cells are appended in draw order, so replaying them in
// order makes the last draw of a cell win.
type scrollCell struct {
	X     int
	Rune  rune
	Combc []rune
	Style Style
}

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
// The column is bounded to maxScrollContentHeight rows so box-driven
// children (Rect, Row, Column) cannot drive unbounded rendering. The view
// is clamped to the content extent, so an offset beyond the end shows the
// last rows. It accepts the common specs: a Box override, the style chain,
// and Fill, which paints the visible window's background.
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

func renderVerticalScroll(v _VerticalScroll, box Box, style Style, draw drawFunc, cursor cursorFunc, options displaywidth.Options) {
	box = v.effectiveBox(box)
	style = v.styled(style)

	elemBox := Box{
		Left:   box.Left,
		Right:  box.Right,
		Top:    box.Top,
		Bottom: box.Top + maxScrollContentHeight,
	}
	maxY := box.Top
	// The slice is pre-sized to the window height, the common case where
	// the content is not taller than the window; taller content grows the
	// slice. Rows are indexed by y-box.Top, so the replay loop below
	// walks the window rows directly.
	cells := make([][]scrollCell, max(box.Height(), 1))
	sub := drawFunc(func(x, y int, mainc rune, combc []rune, st Style) {
		if y > maxY {
			maxY = y
		}
		idx := y - box.Top
		if idx < 0 {
			return
		}
		for len(cells) <= idx {
			cells = append(cells, nil)
		}
		// Cells are appended in draw order, so a later draw of the same
		// cell wins when the blit below replays the draws in order.
		cells[idx] = append(cells[idx], scrollCell{X: x, Rune: mainc, Combc: combc, Style: st})
	})
	// Cursor requests from the child are in content coordinates; they are
	// transformed to window coordinates after the view window is computed.
	var cursorRequests []struct{ x, y int }
	subCursor := func(x, y int) {
		cursorRequests = append(cursorRequests, struct{ x, y int }{x, y})
	}
	renderElement(v.child, elemBox, style, sub, subCursor, options)

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
	if v.fill {
		marks = make([]bool, box.Width()*box.Height())
	}
	numTopCrop := fromY - box.Top
	for i := 0; i < box.Height(); i++ {
		y := fromY + i
		rowIdx := y - box.Top
		if rowIdx < 0 || rowIdx >= len(cells) {
			continue
		}
		for _, cell := range cells[rowIdx] {
			// Cells outside the window are clipped on both edges: a child
			// with a Box override or a negative margin may draw beyond the
			// window, and none of it may bleed onto the screen.
			if cell.X < box.Left || cell.X >= clipRight {
				continue
			}
			w := clusterWidth(options, cell.Rune, cell.Combc)
			// A cluster that would extend past the right edge is not
			// drawn, so content never spills beyond the window.
			if cell.X+w > clipRight {
				continue
			}
			if marks != nil {
				idx := i*box.Width() + (cell.X - box.Left)
				if idx >= 0 && idx < len(marks) {
					marks[idx] = true
					for j := 1; j < w; j++ {
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
			if box.Left+i >= clipRight {
				break
			}
			draw(box.Left+i, box.Top, r, nil, s)
		}
	}
	if numBottomCrop > 0 {
		s := withAttrOn(DarkerOrLighterStyle(style, 15), true, vt.Bold)
		for i, r := range fmt.Sprintf(" %d.. ", numBottomCrop) {
			if box.Left+i >= clipRight {
				break
			}
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
	if len(cursorRequests) > 0 {
		// The last cursor request wins, mirroring the last-draw-wins rule
		// for cells. The content coordinate is mapped to the window.
		last := cursorRequests[len(cursorRequests)-1]
		wy := last.y - fromY + box.Top
		if last.x >= box.Left && last.x < clipRight && wy >= box.Top && wy < box.Bottom {
			cursor(last.x, wy)
		}
	}
}
