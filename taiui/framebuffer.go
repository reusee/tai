package taiui

import "sync"

type frameBufferCell struct {
	rune  rune
	style Style
}

type FrameBuffer struct {
	sync.RWMutex
	cells  []*frameBufferCell
	left   int
	top    int
	width  int
	height int
}

var _ Element = new(FrameBuffer)

func NewFrameBuffer(box Box) *FrameBuffer {
	width := box.Width()
	height := box.Height()
	return &FrameBuffer{
		cells:  make([]*frameBufferCell, width*height),
		left:   box.Left,
		top:    box.Top,
		width:  width,
		height: height,
	}
}

func (f *FrameBuffer) RenderFunc() any {
	return func(box Box, set SetContent) {
		tw := box.Width()
		th := box.Height()
		f.RLock()
		defer f.RUnlock()
		for y := 0; y < f.height; y++ {
			if y >= th {
				break
			}
			for x := 0; x < f.width; x++ {
				if x >= tw {
					break
				}
				cell := f.cells[y*f.width+x]
				if cell == nil {
					continue
				}
				set(box.Left+x, box.Top+y, cell.rune, nil, cell.style)
			}
		}
	}
}

func (f *FrameBuffer) SetContent(x, y int, mainc rune, combc []rune, style Style) {
	x -= f.left
	y -= f.top
	i := y*f.width + x
	f.Lock()
	defer f.Unlock()
	if i >= 0 && i < len(f.cells) {
		f.cells[i] = &frameBufferCell{rune: mainc, style: style}
	}
}
