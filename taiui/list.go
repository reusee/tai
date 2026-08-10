package taiui

import (
	"fmt"

	"github.com/clipperhouse/displaywidth"
)

const TheoryOfList = `
taiui list theory:
- List renders a vertical list of single-line items. The selected
  item is highlighted with the ListStyle spec. The view scrolls to
  keep the selected item visible, clamped to the content extent.
- List renders only the visible items: the view window is computed
  from the selected index and the box height, and items outside it
  are never processed. This is O(window) per render, unlike a
  VerticalScroll of a Column of Text, which renders the whole
  content into a virtual column.
- The selected item is styled with the ListStyle applied to the
  element's style chain. The ListStyle spec is a StyleFunc, so it
  composes with the chain.
- Fill paints the unoccupied cells: the rows below the last item
  when the content is shorter than the box. Each item row is fully
  painted by the item's own fill, so the selected row shows the
  selected style's background across the whole row.
`

var _ Element = _List{}

// _List is a vertical list of single-line items. It is a pure value:
// specs are interpreted at construction into typed fields, and
// rendering reads those fields.
type _List struct {
	elementBase
	items         []string
	selected      int
	selectedStyle StyleFunc
}

// List renders a vertical list of single-line items. The selected
// item is highlighted with the ListStyle spec. The view scrolls to
// keep the selected item visible, clamped to the content extent.
// It accepts the common specs: a Box override, the style chain, and
// Fill, which paints the unoccupied cells.
func List(items []string, selected int, specs ...any) _List {
	l := &_List{items: items, selected: selected}
	buildElement(l, specs)
	return *l
}

func (_List) element() {}

// ListStyle styles the selected item of a List. It composes with the
// element's style chain; the last ListStyle spec wins.
type ListStyle StyleFunc

func (ListStyle) spec() {}

func (l *_List) applySpec(spec any) {
	if spec == nil {
		return
	}
	switch v := spec.(type) {
	case Specs:
		for _, s := range v {
			l.applySpec(s)
		}
	case ListStyle:
		l.selectedStyle = StyleFunc(v)
	default:
		if l.applyCommonSpec(v) {
			return
		}
		panic(fmt.Errorf("unknown spec %#v", v))
	}
}

func renderList(l _List, box Box, style Style, draw drawFunc, cursor cursorFunc, options displaywidth.Options) {
	box = l.effectiveBox(box)
	style = l.styled(style)

	contentHeight := len(l.items)
	selected := l.selected
	if contentHeight == 0 {
		selected = -1
	} else {
		if selected < 0 {
			selected = 0
		}
		if selected >= contentHeight {
			selected = contentHeight - 1
		}
	}

	// The view is centered on the selected item, clamped to the
	// content extent.
	fromY := selected - box.Height()/2
	if fromY < 0 {
		fromY = 0
	}
	if maxFromY := contentHeight - box.Height(); maxFromY > 0 && fromY > maxFromY {
		fromY = maxFromY
	}

	// Render only the visible items. Each item is a single-line Text
	// with fill, so the row is fully painted and the selected row
	// shows the selected style's background across the whole row.
	for i := fromY; i < min(fromY+box.Height(), contentHeight); i++ {
		itemStyle := style
		if i == selected && l.selectedStyle != nil {
			itemStyle = l.selectedStyle(style)
		}
		t := _Text{
			elementBase: elementBase{fill: l.fill || i == selected},
			lines:       l.items[i : i+1],
		}
		renderText(t, Box{
			Top:    box.Top + i - fromY,
			Left:   box.Left,
			Bottom: box.Top + i - fromY + 1,
			Right:  box.Right,
		}, itemStyle, draw, cursor, options)
	}

	// Fill paints the rows below the last item when the content is
	// shorter than the box.
	if l.fill {
		for y := box.Top + min(contentHeight, box.Height()); y < box.Bottom; y++ {
			for x := box.Left; x < box.Right; x++ {
				draw(x, y, ' ', nil, style)
			}
		}
	}
}
