package taiui

import (
	"fmt"
	"strconv"
	"sync"

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

// scrollCellsPool pools the content-cell slices of VerticalScroll
// renders. A scroll renders its whole child into a virtual column, so
// the collected cells can be far larger than the visible window;
// pooling avoids an allocation per render.
var scrollCellsPool = sync.Pool{
	New: func() any {
		return make([]scrollCell, 0, 64)
	},
}

// scrollCell is one content cell collected while rendering the child of a
// VerticalScroll. Cells are appended in draw order, so replaying them in
// order makes the last draw of a cell win. Y is the content row, used to
// map the cell into the visible window during replay.
type scrollCell struct {
	X, Y  int
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
	// Cells are collected into one flat slice in draw order, so a later
	// draw of the same cell wins when the replay below walks the slice in
	// order. The slice is pooled: a scroll renders its whole child into
	// the virtual column, so the collected cells can be far larger than
	// the visible window, and pooling avoids an allocation per render.
	cells := scrollCellsPool.Get().([]scrollCell)
	if cap(cells) < box.Width()*box.Height()/4 {
		cells = make([]scrollCell, 0, box.Width()*box.Height()/4)
	}
	cells = cells[:0]
	defer func() {
		scrollCellsPool.Put(cells)
	}()
	sub := drawFunc(func(x, y int, mainc rune, combc []rune, st Style) {
		if y > maxY {
			maxY = y
		}
		if y < box.Top {
			return
		}
		cells = append(cells, scrollCell{X: x, Y: y, Rune: mainc, Combc: combc, Style: st})
	})
	// Cursor requests from the child are in content coordinates; they are
	// transformed to window coordinates after the view window is computed.
	// Only the last request matters, mirroring the last-draw-wins rule for
	// cells, so a single value replaces a slice.
	var cursorX, cursorY int
	cursorSet := false
	subCursor := func(x, y int) {
		cursorX, cursorY = x, y
		cursorSet = true
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
		marks = getMarks(box.Width() * box.Height())
		defer putMarks(marks)
	}
	for _, cell := range cells {
		// Cells outside the window are clipped on both edges: a child
		// with a Box override or a negative margin may draw beyond the
		// window, and none of it may bleed onto the screen.
		wy := cell.Y - fromY
		if wy < 0 || wy >= box.Height() {
			continue
		}
		if cell.X < box.Left || cell.X >= clipRight {
			continue
		}
		w := ClusterWidth(options, cell.Rune, cell.Combc)
		// A cluster that would extend past the right edge is not
		// drawn, so content never spills beyond the window.
		if cell.X+w > clipRight {
			continue
		}
		if marks != nil {
			idx := wy*box.Width() + (cell.X - box.Left)
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
		draw(cell.X, box.Top+wy, cell.Rune, cell.Combc, cell.Style)
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
	numTopCrop := fromY - box.Top
	numBottomCrop := maxY - (fromY + box.Height()) + 1
	if numTopCrop > 0 {
		drawCropIndicator(draw, box.Left, box.Top, clipRight, numTopCrop, withAttrOn(DarkerOrLighterStyle(style, 15), true, vt.Bold))
	}
	if numBottomCrop > 0 {
		drawCropIndicator(draw, box.Left, box.Bottom-1, clipRight, numBottomCrop, withAttrOn(DarkerOrLighterStyle(style, 15), true, vt.Bold))
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
	if cursorSet {
		// The content coordinate is mapped to the window.
		wy := cursorY - fromY + box.Top
		if cursorX >= box.Left && cursorX < clipRight && wy >= box.Top && wy < box.Bottom {
			cursor(cursorX, wy)
		}
	}
}

// drawCropIndicator draws a " N.. " crop indicator at the given row,
// clipped to the window's content area so it never paints the scrollbar
// column. The indicator is ASCII, so the stack buffer is written byte by
// byte without allocating.
func drawCropIndicator(draw drawFunc, x, y, clipRight, n int, style Style) {
	var buf [16]byte
	b := append(buf[:0], ' ')
	b = strconv.AppendInt(b, int64(n), 10)
	b = append(b, '.', '.', ' ')
	for i, c := range b {
		if x+i >= clipRight {
			break
		}
		draw(x+i, y, rune(c), nil, style)
	}
}
