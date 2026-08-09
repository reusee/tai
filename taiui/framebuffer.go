package taiui

import "sync"

type frameBufferCell struct {
	rune  rune
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

// SetContent writes a cell into the content, using content-local coordinates.
func (c *FrameBufferContent) SetContent(x, y int, mainc rune, combc []rune, style Style) {
	c.Lock()
	defer c.Unlock()
	i := y*c.width + x
	if i >= 0 && i < len(c.cells) {
		c.cells[i] = &frameBufferCell{rune: mainc, style: style}
	}
}

var _ Element = _FrameBuffer{}

type _FrameBuffer struct {
	content *FrameBufferContent
}

// FrameBuffer renders framebuffer content into the box supplied by the
// layout. The content is state: the rendered output is a pure function of the
// content value, and placement is the layout's decision.
func FrameBuffer(content *FrameBufferContent) _FrameBuffer {
	return _FrameBuffer{content: content}
}

func (f _FrameBuffer) RenderFunc() any {
	return func(box Box, setContent SetContent) {
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
				setContent(box.Left+x, box.Top+y, cell.rune, nil, cell.style)
			}
		}
	}
}
