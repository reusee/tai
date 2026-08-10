package taiui

import (
	"sync"

	"github.com/clipperhouse/displaywidth"
)

type frameBufferCell struct {
	rune  rune
	combc []rune
	style Style
	set   bool
}

// placedCell is one set cell of a framebuffer snapshot, with its
// position in the render box.
type placedCell struct {
	x, y int
	cell frameBufferCell
}

// placedCellPool pools the snapshot slices of framebuffer renders.
// A framebuffer snapshots its visible set cells under the read lock;
// pooling avoids an allocation per render.
var placedCellPool = sync.Pool{
	New: func() any { return make([]placedCell, 0, 64) },
}

// FrameBufferContent is offscreen framebuffer content. It is state: an
// ordinary data value that the application builds and mutates. The
// FrameBuffer element reads it purely during rendering, so updating a
// framebuffer is updating state, never an imperative element-update call.
type FrameBufferContent struct {
	sync.RWMutex
	width  int
	height int
	cells  []frameBufferCell
}

// NewFrameBufferContent creates empty framebuffer content of the given size.
func NewFrameBufferContent(width, height int) *FrameBufferContent {
	return &FrameBufferContent{
		width:  width,
		height: height,
		cells:  make([]frameBufferCell, width*height),
	}
}

// SetContent writes a cell into the content, using content-local
// coordinates. Writes outside the content bounds are ignored. The cell
// is stored by value, so a write allocates nothing.
func (c *FrameBufferContent) SetContent(x, y int, mainc rune, combc []rune, style Style) {
	c.Lock()
	defer c.Unlock()
	if x < 0 || x >= c.width || y < 0 || y >= c.height {
		return
	}
	c.cells[y*c.width+x] = frameBufferCell{rune: mainc, combc: combc, style: style, set: true}
}

// Clear resets every cell to a blank cell with the given style, in a
// single locked operation. Clearing a large canvas with Clear is faster
// than clearing cell by cell with SetContent, which locks per cell.
// Cells are written by value, so a clear allocates nothing.
func (c *FrameBufferContent) Clear(style Style) {
	c.Lock()
	defer c.Unlock()
	for i := range c.cells {
		c.cells[i] = frameBufferCell{rune: ' ', style: style, set: true}
	}
}

var _ Element = _FrameBuffer{}

// _FrameBuffer renders framebuffer content into the box supplied by the
// layout. It is a pure value: the content is data state, and rendering is a
// pure read of it.
type _FrameBuffer struct {
	content *FrameBufferContent
}

// FrameBuffer wraps framebuffer content into an element that renders the
// content into the box supplied by the layout. The content is data state:
// mutating it and re-rendering shows the update without any element call.
func FrameBuffer(content *FrameBufferContent) _FrameBuffer {
	return _FrameBuffer{content: content}
}

func (_FrameBuffer) element() {}

func renderFrameBuffer(f _FrameBuffer, box Box, style Style, draw drawFunc, cursor cursorFunc, options displaywidth.Options) {
	content := f.content
	if content == nil {
		return
	}
	// Snapshot the visible cells under the read lock, then draw outside
	// the lock: a concurrent writer is blocked only for the snapshot,
	// never for the draw. Cells are stored by value, so the snapshot
	// copies each cell and stays consistent.
	content.RLock()
	width := min(content.width, box.Width())
	height := min(content.height, box.Height())
	placed := placedCellPool.Get().([]placedCell)
	if cap(placed) < width*height/4 {
		placed = make([]placedCell, 0, width*height/4)
	}
	placed = placed[:0]
	defer func() {
		placedCellPool.Put(placed)
	}()
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cell := content.cells[y*content.width+x]
			if !cell.set {
				continue
			}
			placed = append(placed, placedCell{x: box.Left + x, y: box.Top + y, cell: cell})
		}
	}
	content.RUnlock()
	// The placed cells are in row-major order. A wide cluster covers its
	// trailing columns; a cell in those columns is part of the cluster's
	// visual space and must not be drawn over it, so a stale cell left
	// by a moved cluster never corrupts the display.
	coveredUntil := -1
	lastY := -1
	for _, c := range placed {
		if c.y != lastY {
			coveredUntil = -1
			lastY = c.y
		}
		if c.x < coveredUntil {
			continue
		}
		w := ClusterWidth(options, c.cell.rune, c.cell.combc)
		// A wide cluster that would extend past the box's right edge is
		// not drawn, matching Text and VerticalScroll: content never
		// spills past its box.
		if c.x+w > box.Right {
			continue
		}
		draw(c.x, c.y, c.cell.rune, c.cell.combc, c.cell.style)
		if c.x+w > coveredUntil {
			coveredUntil = c.x + w
		}
	}
}
