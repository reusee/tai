package taiui

import "fmt"

const TheoryOfBoxModel = `
taiui box model theory:
- The box-model elements (Rect, Row, Column) share one margin/border/
  padding model: the content box is the element box inset by margin,
  then by the border, then by padding. A border consumes one cell on
  each side; margin and padding consume exactly their listed counts.
- Fill paints the ring between the outer box (the margin edges) and the
  content box, so the margin ring stays unpainted and the padding and
  border rings show the element background.
- The border draws independently of fill: a border is visible even when
  no background is painted. BorderType selects the glyph set (single,
  rounded, double, thick). The border ring is clipped to the element
  box, so a negative margin can never paint border glyphs outside it.
- Text is not a box-model element: it keeps its padding-only inset and
  line-fill semantics, so a bordered text block is a Text inside a Rect.
`

// Border toggles the box border on the box-model elements (Rect, Row,
// Column). The border is a one-cell ring between the margin and the
// padding; it shrinks the content box by one cell on each side.
type Border bool

// BorderType selects the glyph set for a box border. It composes with
// Border; the default is BorderSingle.
type BorderType int

// The border glyph sets: single-line (default), rounded corners,
// double-line, and thick.
const (
	BorderSingle BorderType = iota
	BorderRounded
	BorderDouble
	BorderThick
)

// BorderStyle styles the border. It composes with the element's style
// chain; the last BorderStyle spec wins.
type BorderStyle StyleFunc

type boxModel struct {
	margin      [4]int
	padding     [4]int
	border      bool
	borderType  BorderType
	borderStyle StyleFunc
}

// borderGlyphs is the glyph set of one border style: the horizontal and
// vertical edges and the four corners.
type borderGlyphs struct {
	horizontal, vertical, topLeft, topRight, bottomLeft, bottomRight rune
}

// borderGlyphSets maps each border type to its glyph set.
var borderGlyphSets = [...]borderGlyphs{
	BorderSingle:  {horizontal: '─', vertical: '│', topLeft: '┌', topRight: '┐', bottomLeft: '└', bottomRight: '┘'},
	BorderRounded: {horizontal: '─', vertical: '│', topLeft: '╭', topRight: '╮', bottomLeft: '╰', bottomRight: '╯'},
	BorderDouble:  {horizontal: '═', vertical: '║', topLeft: '╔', topRight: '╗', bottomLeft: '╚', bottomRight: '╝'},
	BorderThick:   {horizontal: '━', vertical: '┃', topLeft: '┏', topRight: '┓', bottomLeft: '┗', bottomRight: '┛'},
}

func (m *boxModel) applySpec(spec any) bool {
	switch v := spec.(type) {
	case _Margin:
		m.margin = applyBoxModel(v)
	case _Padding:
		m.padding = applyBoxModel(v)
	case Border:
		m.border = bool(v)
	case BorderType:
		if v < 0 || int(v) >= len(borderGlyphSets) {
			panic(fmt.Errorf("taiui: bad border type %d", v))
		}
		m.borderType = v
	case BorderStyle:
		m.borderStyle = StyleFunc(v)
	default:
		return false
	}
	return true
}

// outerBox returns the box at the margin edges. The margin ring outside
// it is never painted.
func (m *boxModel) outerBox(box Box) Box {
	return Box{
		Top:    box.Top + m.margin[0],
		Left:   box.Left + m.margin[3],
		Right:  box.Right - m.margin[1],
		Bottom: box.Bottom - m.margin[2],
	}
}

// contentBox returns the box the children render into: the outer box
// inset by the border and the padding.
func (m *boxModel) contentBox(box Box) Box {
	outer := m.outerBox(box)
	border := 0
	if m.border {
		border = 1
	}
	return Box{
		Top:    outer.Top + border + m.padding[0],
		Left:   outer.Left + border + m.padding[3],
		Right:  outer.Right - border - m.padding[1],
		Bottom: outer.Bottom - border - m.padding[2],
	}
}

// The box-drawing glyphs of a full border.
const (
	borderVertical          = '│'
	borderTopLeftCorner     = '┌'
	borderTopRightCorner    = '┐'
	borderBottomLeftCorner  = '└'
	borderBottomRightCorner = '┘'
)

func (m *boxModel) drawBorder(outer, box Box, style Style, draw drawFunc) {
	if !m.border {
		return
	}
	if outer.Width() < 2 || outer.Height() < 2 {
		return
	}
	if m.borderStyle != nil {
		style = m.borderStyle(style)
	}
	glyphs := borderGlyphSets[m.borderType]
	top, left := outer.Top, outer.Left
	bottom, right := outer.Bottom-1, outer.Right-1
	// The ring is clipped to the element box: a negative margin pushes
	// the outer box past the element box, and no border glyph may paint
	// outside it. A ring cell is a corner only where both its edges are
	// visible; a clipped ring end gets the edge glyph.
	topVisible := top >= box.Top && top < box.Bottom
	bottomVisible := bottom >= box.Top && bottom < box.Bottom
	leftVisible := left >= box.Left && left < box.Right
	rightVisible := right >= box.Left && right < box.Right

	if topVisible && leftVisible {
		draw(left, top, glyphs.topLeft, nil, style)
	}
	if topVisible && rightVisible {
		draw(right, top, glyphs.topRight, nil, style)
	}
	if bottomVisible && leftVisible {
		draw(left, bottom, glyphs.bottomLeft, nil, style)
	}
	if bottomVisible && rightVisible {
		draw(right, bottom, glyphs.bottomRight, nil, style)
	}
	if topVisible {
		for x := max(left+1, box.Left); x <= min(right-1, box.Right-1); x++ {
			draw(x, top, glyphs.horizontal, nil, style)
		}
	}
	if bottomVisible {
		for x := max(left+1, box.Left); x <= min(right-1, box.Right-1); x++ {
			draw(x, bottom, glyphs.horizontal, nil, style)
		}
	}
	if leftVisible {
		for y := max(top+1, box.Top); y <= min(bottom-1, box.Bottom-1); y++ {
			draw(left, y, glyphs.vertical, nil, style)
		}
	}
	if rightVisible {
		for y := max(top+1, box.Top); y <= min(bottom-1, box.Bottom-1); y++ {
			draw(right, y, glyphs.vertical, nil, style)
		}
	}
}
