package taiui

import (
	"fmt"
	"sync"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3/vt"
)

var _ Element = _VerticalScroll{}

// maxScrollContentHeight bounds the virtual column a VerticalScroll renders
// its child into. Box-driven children (Rect, Row, Column) render their whole
// box, so an unbounded column would make them loop over unbounded space;
// the bound keeps such rendering finite. The TUI streams the session output
// (see TheoryOfTUI in cmd/tai/tui.go), whose lines each wrap into several
// display rows, so the bound must comfortably hold the wrapped display
// lines of a full session.
const maxScrollContentHeight = 1 << 17

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
// crops to the visible window whose first content row is given by offset.
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

	// The collection range covers the expected window: for tall content
	// the window is within the range, so one pass suffices; for short
	// content whose offset clamps below the requested offset, the range
	// misses the window and a second pass re-collects the window cells.
	// The range spans one window height, so a tall virtual column never
	// accumulates cells for rows outside it.
	collectFrom := max(box.Top, box.Top+v.offset)
	collectTo := collectFrom + box.Height()
	if collectTo > box.Top+maxScrollContentHeight {
		collectTo = box.Top + maxScrollContentHeight
	}

	// Cells are collected into one flat slice in draw order, so a later
	// draw of the same cell wins when the replay below walks the slice in
	// order. The slice is pooled: a scroll renders its child into the
	// virtual column, and pooling avoids an allocation per render.
	cells := scrollCellsPool.Get().([]scrollCell)
	if cap(cells) < box.Width()*box.Height()/4 {
		cells = make([]scrollCell, 0, box.Width()*box.Height()/4)
	}
	cells = cells[:0]
	defer func() {
		scrollCellsPool.Put(cells)
	}()

	// collect renders the child and stores the cells in the given row
	// range. maxY tracks the true content extent across passes, so the
	// view window is computed from the full content even when the stored
	// cells are a sub-range.
	collect := func(from, to int) {
		cells = cells[:0]
		sub := drawFunc(func(x, y int, mainc rune, combc []rune, st Style) {
			if y > maxY {
				maxY = y
			}
			if y < from || y >= to {
				return
			}
			cells = append(cells, scrollCell{X: x, Y: y, Rune: mainc, Combc: combc, Style: st})
		})
		renderElement(v.child, elemBox, style, sub, subCursor, options)
	}

	// Reserve the scrollbar column from the start: the child renders at
	// the visible width (the window width minus the scrollbar column), so
	// wrapped text wraps within the visible area instead of hiding behind
	// the scrollbar. When the content fits and no scrollbar is needed,
	// the child is re-rendered at the full width below.
	if v.scrollbar {
		elemBox.Right = box.Right - 1
	}

	collect(collectFrom, collectTo)

	// computeViewWindow clamps the view window to the content extent: it
	// never starts before the box top, nor past the last visible content
	// row. It is called after each collection pass, because the content
	// extent may change when the child is re-rendered at a different width.
	var contentHeight, fromY, maxFromY int
	computeViewWindow := func() {
		contentHeight = maxY - box.Top + 1
		fromY = max(box.Top, box.Top+v.offset)
		maxFromY = maxY - box.Height() + 1
		if maxFromY < box.Top {
			maxFromY = box.Top
		}
		if fromY > maxFromY {
			fromY = maxFromY
		}
	}
	computeViewWindow()

	clipRight := box.Right
	showScrollbar := v.scrollbar && contentHeight > box.Height()
	if showScrollbar {
		clipRight = box.Right - 1
	} else if v.scrollbar {
		// The content fits without a scrollbar: re-render the child at
		// the full width so the rightmost column is not wasted.
		elemBox.Right = box.Right
		collect(collectFrom, collectTo)
		computeViewWindow()
	}

	// When the window falls outside the collected range (short content
	// with a large offset), re-collect the window cells. The window is
	// known now, so the new range covers it exactly.
	if fromY < collectFrom || fromY+box.Height() > collectTo {
		collect(fromY, fromY+box.Height())
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
