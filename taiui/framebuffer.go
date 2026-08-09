package taiui

import "sync"

type frameBufferCell struct {
	rune  rune
	combc []rune
	style Style
}

// FrameBufferContent is offscreen framebuffer content. It is state: an
// ordinary data value that the application builds and mutates. The
// FrameBuffer element reads it purely during rendering, so updating a
// framebuffer is updating state, never an imperative element-update call.
type FrameBufferContent struct {
	sync.RWMutex
	width  int
	height int
	cells  []*frameBufferCell
}

// NewFrameBufferContent creates empty framebuffer content of the given size.
func NewFrameBufferContent(width, height int) *FrameBufferContent {
	return &FrameBufferContent{
		width:  width,
		height: height,
		cells:  make([]*frameBufferCell, width*height),
	}
}

// SetContent writes a cell into the content, using content-local
// coordinates. Writes outside the content bounds are ignored.
func (c *FrameBufferContent) SetContent(x, y int, mainc rune, combc []rune, style Style) {
	c.Lock()
	defer c.Unlock()
	if x < 0 || x >= c.width || y < 0 || y >= c.height {
		return
	}
	c.cells[y*c.width+x] = &frameBufferCell{rune: mainc, combc: combc, style: style}
}

var _ Element = _FrameBuffer{}

// _FrameBuffer renders framebuffer content into the box supplied by the
// layout. It is a pure value: the content is data state, and rendering is a
// pure read of it.
type _FrameBuffer struct {
	content *FrameBufferContent
}

func FrameBuffer(content *FrameBufferContent) _FrameBuffer {
	return _FrameBuffer{content: content}
}

func (_FrameBuffer) element() {}

func renderFrameBuffer(f _FrameBuffer, box Box, style Style, draw drawFunc) {
	content := f.content
	if content == nil {
		return
	}
	content.RLock()
	defer content.RUnlock()
	width := min(content.width, box.Width())
	height := min(content.height, box.Height())
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cell := content.cells[y*content.width+x]
			if cell == nil {
				continue
			}
			draw(box.Left+x, box.Top+y, cell.rune, cell.combc, cell.style)
		}
	}
}
