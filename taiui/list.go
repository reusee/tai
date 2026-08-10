package taiui

import (
	"fmt"

	"github.com/clipperhouse/displaywidth"
	"github.com/clipperhouse/uax29/v2/graphemes"
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
- Each visible item is rendered directly as a single line: list
  items are always single-line, left-aligned texts, so the general
  text pipeline's alignment, padding, wrap, and line-slice machinery
  is unnecessary overhead. The visible items share one pooled
  grapheme iterator, so a list render allocates nothing per item.
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

	// The visible items share one pooled grapheme iterator, so a list
	// render allocates nothing per item.
	iter := getGraphemeIter()
	defer putGraphemeIter(iter)

	// Render only the visible items. Each item is a single-line Text
	// rendered directly, so the selected row is fully painted with
	// the selected style's background.
	for i := fromY; i < min(fromY+box.Height(), contentHeight); i++ {
		itemStyle := style
		if i == selected && l.selectedStyle != nil {
			itemStyle = l.selectedStyle(style)
		}
		renderListLine(l.items[i], Box{
			Top:    box.Top + i - fromY,
			Left:   box.Left,
			Bottom: box.Top + i - fromY + 1,
			Right:  box.Right,
		}, itemStyle, l.fill || i == selected, draw, options, iter)
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

func renderListLine(line string, box Box, style Style, fill bool, draw drawFunc, options displaywidth.Options, iter *graphemes.Iterator[string]) {
	x := box.Left
	iter.SetText(line)
	for iter.Next() {
		cluster := iter.Value()
		if cluster == "\t" {
			// A tab advances to the next tab stop relative to the
			// content area's left edge; the skipped cells are painted
			// when fill is on.
			tabStop := nextTabStop(x, box.Left, 8)
			if tabStop > box.Right {
				tabStop = box.Right
			}
			if fill {
				for ; x < tabStop; x++ {
					draw(x, box.Top, ' ', nil, style)
				}
			}
			x = tabStop
			continue
		}
		mainc, combc, width := splitClusterWidth(options, cluster)
		// A cluster that would extend past the right edge is not
		// drawn, so text never spills beyond the box.
		if x >= box.Right || x+width > box.Right {
			break
		}
		draw(x, box.Top, mainc, combc, style)
		x += width
	}
	if fill {
		for ; x < box.Right; x++ {
			draw(x, box.Top, ' ', nil, style)
		}
	}
}
