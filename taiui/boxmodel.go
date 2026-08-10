package taiui

// TheoryOfBoxModel documents the design rationale for the box model shared
// by the box-model elements.
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
  no background is painted.
- Text is not a box-model element: it keeps its padding-only inset and
  line-fill semantics, so a bordered text block is a Text inside a Rect.
`

// Border toggles the box border on the box-model elements (Rect, Row,
// Column). The border is a one-cell ring between the margin and the
// padding; it shrinks the content box by one cell on each side.
type Border bool

// BorderStyle styles the border. It composes with the element's style
// chain; the last BorderStyle spec wins.
type BorderStyle StyleFunc

// boxModel is the CSS-style box model shared by the box-model elements.
// The content box is the element box inset by margin, then by the border,
// then by padding; the outer box is the box inset to the margin edges.
// Fill paints the ring between the outer box and the content box, and
// drawBorder draws the border ring at the outer box's inner edge.
type boxModel struct {
	margin      [4]int
	padding     [4]int
	border      bool
	borderStyle StyleFunc
}

// applySpec interprets one box-model spec into the model. It returns true
// if the spec was a box-model spec.
func (m *boxModel) applySpec(spec any) bool {
	switch v := spec.(type) {
	case _Margin:
		m.margin = applyBoxModel(v)
	case _Padding:
		m.padding = applyBoxModel(v)
	case Border:
		m.border = bool(v)
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
	borderHorizontal        = '─'
	borderVertical          = '│'
	borderTopLeftCorner     = '┌'
	borderTopRightCorner    = '┐'
	borderBottomLeftCorner  = '└'
	borderBottomRightCorner = '┘'
)

// drawBorder draws the border ring at the inner edge of the outer box. It
// is a no-op when the border is disabled or the outer box is too small to
// hold a ring.
func (m *boxModel) drawBorder(outer Box, style Style, draw drawFunc) {
	if !m.border {
		return
	}
	if outer.Width() < 2 || outer.Height() < 2 {
		return
	}
	if m.borderStyle != nil {
		style = m.borderStyle(style)
	}
	top, left := outer.Top, outer.Left
	bottom, right := outer.Bottom-1, outer.Right-1
	draw(left, top, borderTopLeftCorner, nil, style)
	draw(right, top, borderTopRightCorner, nil, style)
	draw(left, bottom, borderBottomLeftCorner, nil, style)
	draw(right, bottom, borderBottomRightCorner, nil, style)
	for x := left + 1; x < right; x++ {
		draw(x, top, borderHorizontal, nil, style)
		draw(x, bottom, borderHorizontal, nil, style)
	}
	for y := top + 1; y < bottom; y++ {
		draw(left, y, borderVertical, nil, style)
		draw(right, y, borderVertical, nil, style)
	}
}
