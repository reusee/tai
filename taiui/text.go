package taiui

import (
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
	"github.com/clipperhouse/uax29/v2/graphemes"
)

const TheoryOfTextRendering = `
taiui text rendering theory:
- Text rendering optimizes the common case: plain left-aligned,
  top-aligned, non-wrapped text, with an optional fill, end cursor, or
  cursor-at-offset, is the hottest path in the library, so it renders
  directly, avoiding the general pipeline's line-slice pool and
  per-line alignment overhead.
- The fast path is a strict subset of the general path: it applies only
  when the general path would produce identical output, so the two paths
  never diverge.
- Non-wrapped text renders the text's lines directly as a sub-slice,
  so no line slice is built and the pool is untouched; only wrapped
  text builds a line slice.
- Grapheme iterators are pooled: the text pipeline resets a pooled
  uax29 iterator per line instead of allocating a fresh iterator per
  call, so a per-line iteration allocates nothing. The wrap and cursor
  helpers accept a caller-provided iterator, so a render pass shares one
  pooled iterator across the line loop, wrapping, and cursor placement.
- Wrap working slices are pooled: the wrap helper's word and line
  slices are pooled across calls, so a wrapped render pass allocates
  only the joined line strings. The helper appends to a caller-provided
  line slice, so the fast path appends the line directly without an
  intermediate slice.
- A cluster is split and measured in one step: splitClusterWidth
  decodes the base rune once and measures the cluster, so the common
  single-rune cluster pays one decode instead of two. A single-rune
  cluster is measured with options.Rune; a multi-rune cluster
  (combining sequences, ZWJ emoji) is measured with options.String,
  which allocates only for the rare multi-rune clusters.
- Wrapping is bounded by the visible line count: the render path passes
  the remaining rows to wrapLineLimited, so a long line in a small box
  never wraps beyond the rows the box can show, and pathological inputs
  cannot accumulate unbounded wrapped lines.
`

// OffsetStyleFunc styles a text position by its rune offset within the
// physical line (a wrapped line restarts the offset). Offsets count runes,
// including the combining runes of clusters.
type OffsetStyleFunc func(int) StyleFunc

var _ Element = _Text{}

type _Text struct {
	elementBase
	lines           []string
	align           Align
	valign          VAlign
	padding         [4]int
	offsetStyleFunc OffsetStyleFunc
	wrap            bool
	tabWidth        int
	cursor          bool
	cursorAt        int
}

func Text(specs ...any) _Text {
	t := &_Text{tabWidth: 8, cursorAt: -1}
	buildElement(t, specs)
	return *t
}

// Input creates a single-line text input: a Text with the cursor at the
// given rune offset within the text. The text and cursor are state; the
// application updates them via scope forks and handles key events.
func Input(text string, cursor int, specs ...any) _Text {
	return Text(append([]any{text, CursorAt(cursor)}, specs...)...)
}

func (_Text) element() {}

func (Wrap) spec() {}

func (TabWidth) spec() {}

func (Cursor) spec() {}

func (CursorAt) spec() {}

func (t *_Text) applySpec(spec any) {
	if spec == nil {
		return
	}
	switch v := spec.(type) {
	case Specs:
		for _, s := range v {
			t.applySpec(s)
		}
	case string:
		t.lines = append(t.lines, splitLines(v)...)
	case []string:
		t.lines = append(t.lines, v...)
	case Align:
		t.align = v
	case VAlign:
		t.valign = v
	case _Padding:
		t.padding = applyBoxModel(v)
	case OffsetStyleFunc:
		t.offsetStyleFunc = v
	case Wrap:
		t.wrap = bool(v)
	case TabWidth:
		t.tabWidth = int(v)
	case Cursor:
		t.cursor = bool(v)
	case CursorAt:
		t.cursor = true
		t.cursorAt = int(v)
	default:
		if t.applyCommonSpec(v) {
			return
		}
		panic(fmt.Errorf("unknown spec %#v", v))
	}
}

// splitLines splits a text string into lines at newline boundaries,
// normalizing CRLF to LF so a carriage return never reaches a cell.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}

// textLinesPool pools the wrapped-line slices of Text renders. A Text
// renders its lines into a fresh slice per pass; pooling avoids the
// allocation for the common screen-sized texts.
var textLinesPool = sync.Pool{
	New: func() any { return make([]string, 0, 64) },
}

// graphemeIterPool pools the uax29 grapheme iterators of the text
// pipeline. options.StringGraphemes allocates a fresh iterator per
// call; pooling the iterator and resetting it with SetText avoids the
// allocation for the per-line iterations of text rendering, wrapping,
// and cursor placement.
var graphemeIterPool = sync.Pool{
	New: func() any { return graphemes.FromString("") },
}

// getGraphemeIter returns a grapheme iterator from the pool.
func getGraphemeIter() *graphemes.Iterator[string] {
	return graphemeIterPool.Get().(*graphemes.Iterator[string])
}

// putGraphemeIter returns a grapheme iterator to the pool.
func putGraphemeIter(iter *graphemes.Iterator[string]) {
	graphemeIterPool.Put(iter)
}

// wrapBuffers holds the reusable working slices of a wrap call.
type wrapBuffers struct {
	cur  []string
	word []string
}

// wrapBuffersPool pools the working slices of wrapLineLimitedIter. A
// wrap call allocates two string slices sized to the box width; pooling
// them avoids the allocation for the per-line wrap calls of a render
// pass.
var wrapBuffersPool = sync.Pool{
	New: func() any {
		return &wrapBuffers{
			cur:  make([]string, 0, 80),
			word: make([]string, 0, 80),
		}
	},
}

// clusterWidth returns the display width of a grapheme cluster under
// the given options. A single-rune cluster is measured with
// options.Rune; a multi-rune cluster (combining sequences, ZWJ emoji)
// is measured with options.String, which allocates only for the rare
// multi-rune clusters.
func clusterWidth(options displaywidth.Options, cluster string) int {
	r, size := utf8.DecodeRuneInString(cluster)
	if size == len(cluster) {
		return options.Rune(r)
	}
	return options.String(cluster)
}

// lineWidth returns the display width of a line under the given
// options, iterating its grapheme clusters with the pooled iterator.
func lineWidth(options displaywidth.Options, line string, iter *graphemes.Iterator[string]) int {
	iter.SetText(line)
	width := 0
	for iter.Next() {
		width += clusterWidth(options, iter.Value())
	}
	return width
}

func renderTextFastPath(t _Text, box Box, style Style, draw drawFunc, cursor cursorFunc, options displaywidth.Options, iter *graphemes.Iterator[string]) bool {
	if t.wrap || t.offsetStyleFunc != nil ||
		t.align != AlignLeft || t.valign != VAlignTop ||
		t.padding != [4]int{} {
		return false
	}
	if box.Width() <= 0 || box.Height() <= 0 {
		return false
	}
	tabWidth := t.tabWidth
	if tabWidth <= 0 {
		tabWidth = 8
	}
	y := box.Top
	var x int
	lastLine := ""
	for _, ln := range t.lines {
		if y >= box.Bottom {
			break
		}
		lastLine = ln
		x = box.Left
		iter.SetText(ln)
		for iter.Next() {
			cluster := iter.Value()
			if cluster == "\t" {
				// The tab stop is clamped to the box's right edge,
				// matching the general path: a tab never advances past
				// the content area. With fill, the skipped cells are
				// painted, matching the general path.
				tabStop := nextTabStop(x, box.Left, tabWidth)
				if tabStop > box.Right {
					tabStop = box.Right
				}
				if t.fill {
					for fx := x; fx < tabStop; fx++ {
						draw(fx, y, ' ', nil, style)
					}
				}
				x = tabStop
				continue
			}
			mainc, combc, width := splitClusterWidth(options, cluster)
			if x >= box.Right || x+width > box.Right {
				break
			}
			draw(x, y, mainc, combc, style)
			x += width
		}
		if t.fill {
			// The rest of the line is painted, matching the general
			// path's line-fill semantics. The cursor position is the
			// text end, not the line end, so x is left unchanged.
			for fx := x; fx < box.Right; fx++ {
				draw(fx, y, ' ', nil, style)
			}
		}
		y++
	}
	if t.cursor {
		if len(t.lines) > 0 {
			if t.cursorAt >= 0 {
				// CursorAt places the cursor at a rune offset within the
				// last visible line, matching the general path.
				cursor(textCursorXIter(lastLine, t.cursorAt, box.Left, box.Left, box.Right, tabWidth, options, iter), y-1)
			} else {
				// The cursor is the position after the last drawn cluster of
				// the last line, clamped to the box's right edge: a tab can
				// advance past it, and the general path clamps the same way.
				if x > box.Right {
					x = box.Right
				}
				cursor(x, y-1)
			}
		} else {
			cursor(box.Left, box.Top)
		}
	}
	return true
}

func renderText(t _Text, box Box, style Style, draw drawFunc, cursor cursorFunc, options displaywidth.Options) {
	box = t.effectiveBox(box)
	style = t.styled(style)

	iter := getGraphemeIter()
	defer putGraphemeIter(iter)

	if renderTextFastPath(t, box, style, draw, cursor, options, iter) {
		return
	}

	tabWidth := t.tabWidth
	if tabWidth <= 0 {
		tabWidth = 8
	}

	contentLeft := box.Left + t.padding[3]
	wrapWidth := box.Width() - t.padding[1] - t.padding[3]
	right := box.Right - t.padding[1]
	maxY := box.Bottom - t.padding[2]
	topY := box.Top + t.padding[0]

	// The box is full when the bound is reached; remaining lines are
	// never processed.
	maxLines := maxY - topY
	if maxLines <= 0 {
		return
	}
	// The wrapped lines are computed first, bounded by the content
	// height, so vertical alignment can place the block before
	// rendering. Non-wrapped lines are iterated directly as a sub-slice
	// of the text's lines, so no line slice is built and the pool is
	// untouched; only wrapped text builds a line slice.
	var lines []string
	if t.wrap {
		lines = textLinesPool.Get().([]string)
		lines = lines[:0]
		if cap(lines) < min(len(t.lines), maxLines) {
			lines = make([]string, 0, min(len(t.lines), maxLines))
		}
		defer func() { textLinesPool.Put(lines) }()
		for _, line := range t.lines {
			// The limit is the remaining visible line count: a long
			// line in a small box never wraps beyond the rows the box
			// can show, so pathological inputs cannot accumulate
			// unbounded wrapped lines.
			lines = wrapLineLimitedIter(line, wrapWidth, maxLines-len(lines), options, iter, lines)
			if len(lines) >= maxLines {
				break
			}
		}
	} else {
		lines = t.lines[:min(len(t.lines), maxLines)]
	}

	// Vertical alignment is relative to the padded content area.
	y := topY
	switch t.valign {
	case VAlignMiddle:
		y = (topY + maxY - len(lines)) / 2
	case VAlignBottom:
		y = maxY - len(lines)
	}

	left := contentLeft
	lastLineStart := contentLeft
	for _, ln := range lines {
		left = contentLeft
		switch t.align {
		case AlignRight:
			left = right - lineWidth(options, ln, iter)
		case AlignCenter:
			// Centering is relative to the padded content area and
			// rounds with the conventional (width-len)/2 rule, so an
			// odd-width line places the extra column on the right.
			left = (contentLeft + right - lineWidth(options, ln, iter)) / 2
		}
		lastLineStart = left
		if t.fill {
			// A line is fully painted regardless of alignment: the
			// leading gap is filled before the text draws over it.
			for x := contentLeft; x < left; x++ {
				draw(x, y, ' ', nil, style)
			}
		}
		runeIdx := 0
		edge := contentLeft
		iter.SetText(ln)
		for iter.Next() {
			cluster := iter.Value()
			if cluster == "\t" {
				// A tab advances to the next tab stop relative to the
				// content area's left edge; the skipped cells are
				// painted when fill is on.
				tabStop := nextTabStop(left, contentLeft, tabWidth)
				if tabStop > right {
					tabStop = right
				}
				if t.fill {
					for x := max(left, contentLeft); x < tabStop; x++ {
						draw(x, y, ' ', nil, style)
					}
				}
				left = tabStop
				runeIdx++
				continue
			}
			mainc, combc, width := splitClusterWidth(options, cluster)
			clusterRunes := utf8.RuneCountInString(cluster)
			// Clusters are clipped to the content area: a cluster
			// starting before it is skipped, and a cluster that would
			// extend past its right edge is not drawn, so text never
			// spills beyond the box.
			if left < contentLeft {
				left += width
				runeIdx += clusterRunes
				if t.fill && left > edge {
					// A skipped cluster spanned the content left
					// edge, leaving a residual gap; fill paints it so
					// the line background stays complete.
					for edge < left && edge < right {
						draw(edge, y, ' ', nil, style)
						edge++
					}
				}
				continue
			}
			if left >= right || left+width > right {
				break
			}
			s := style
			if t.offsetStyleFunc != nil {
				s = t.offsetStyleFunc(runeIdx)(s)
			}
			draw(left, y, mainc, combc, s)
			left += width
			runeIdx += clusterRunes
		}
		if t.fill {
			for x := left; x < right; x++ {
				draw(x, y, ' ', nil, style)
			}
		}
		y++
	}

	if t.cursor {
		// The cursor is the position after the last drawn cluster of the
		// last line. An empty text places it at the content start; a
		// clipped line places it at the clip position.
		if len(lines) > 0 {
			if t.cursorAt >= 0 {
				// CursorAt places the cursor at a rune offset within the
				// last line: the line start plus the width of the text
				// before the offset, with tabs expanded to tab stops.
				cursor(textCursorXIter(lines[len(lines)-1], t.cursorAt, lastLineStart, contentLeft, right, tabWidth, options, iter), y-1)
			} else {
				if left > right {
					left = right
				}
				cursor(left, y-1)
			}
		} else {
			cursor(contentLeft, y)
		}
	}
}

// splitCluster separates a grapheme cluster into its base rune and the
// combining runes that follow it. A single-rune cluster decodes the
// base directly and returns no combining runes.
func splitCluster(cluster string) (rune, []rune) {
	base, size := utf8.DecodeRuneInString(cluster)
	if size == len(cluster) {
		return base, nil
	}
	var combc []rune
	for _, r := range cluster[size:] {
		combc = append(combc, r)
	}
	return base, combc
}

// splitClusterWidth splits a grapheme cluster into its base rune and
// combining runes, and returns the cluster's display width, in one
// pass: the base rune is decoded once and the width is measured from
// the same bytes, so the common single-rune cluster pays one decode
// instead of two.
func splitClusterWidth(options displaywidth.Options, cluster string) (rune, []rune, int) {
	base, size := utf8.DecodeRuneInString(cluster)
	if size == len(cluster) {
		return base, nil, options.Rune(base)
	}
	var combc []rune
	for _, r := range cluster[size:] {
		combc = append(combc, r)
	}
	return base, combc, options.String(cluster)
}

// Wrap toggles word wrapping for Text: lines are wrapped to the box width,
// breaking at space runs and hard-breaking words wider than the box at
// cluster boundaries.
type Wrap bool

// TabWidth sets the tab stop interval for Text. The default is 8,
// matching the terminal convention.
type TabWidth int

// Cursor places the terminal cursor at the end of the text when true.
// The cursor position is recorded in the Frame; screens position the
// terminal cursor accordingly. An empty text places the cursor at the
// content start; a clipped line places it at the clip position.
type Cursor bool

// CursorAt places the terminal cursor at a rune offset within the last
// line of the text. The offset counts runes, including the combining
// runes of clusters, and is clamped to the line's rune count. It implies
// Cursor(true).
type CursorAt int

const TheoryOfCursor = `
taiui cursor theory:
- The cursor is part of the render output: a Text with the Cursor spec
  records the position after the last drawn cluster of the last line in
  the Frame. Screens position the terminal cursor at the recorded
  position.
- An empty text places the cursor at the content start; a clipped line
  places it at the clip position.
- CursorAt places the cursor at a rune offset within the last line,
  clamped to the line's rune count. The x position is the line start
  plus the width of the text before the offset, with tabs expanded to
  tab stops, clamped to the content area. Input is a Text with a cursor
  at an application-tracked offset.
- Inside a VerticalScroll, the cursor is transformed from content
  coordinates to window coordinates, so a cursor in a scrolled text input
  tracks the visible position.
- Frame.Equal compares the cursor state, so a screen detects a cursor-only
  change and repositions without repainting cells.
`

// wrapLine wraps a line to the given width. It is the unlimited form of
// wrapLineLimited, used by tests and callers that need the full wrapped
// result.
func wrapLine(line string, width int, options displaywidth.Options) []string {
	return wrapLineLimited(line, width, -1, options)
}

func wrapLineLimited(line string, width, limit int, options displaywidth.Options) []string {
	iter := getGraphemeIter()
	defer putGraphemeIter(iter)
	return wrapLineLimitedIter(line, width, limit, options, iter, nil)
}

// wrapLineLimitedIter is wrapLineLimited with a caller-provided
// grapheme iterator and line slice, so a render pass shares one pooled
// iterator across the line loop and the wrap calls, and appends the
// wrapped lines directly to the caller's line slice.
func wrapLineLimitedIter(line string, width, limit int, options displaywidth.Options, iter *graphemes.Iterator[string], out []string) []string {
	if width <= 0 || limit == 0 {
		return out
	}
	if line == "" {
		return append(out, "")
	}
	// Fast path: a line with no tabs, no leading or trailing spaces, and
	// no consecutive spaces that fits the box is appended as-is. The
	// slow path would join the same words with single spaces, so the
	// output is identical; skipping the word-splitting allocations is
	// the win.
	if !strings.Contains(line, "\t") &&
		!strings.HasPrefix(line, " ") &&
		!strings.HasSuffix(line, " ") &&
		!strings.Contains(line, "  ") &&
		lineWidth(options, line, iter) <= width {
		return append(out, line)
	}

	// Single pass over the grapheme clusters: words are packed onto the
	// current line as they complete, so no word list is built. A word
	// wider than the box is packed in chunks as it grows. The limit
	// bounds the appended lines, so a long line in a small box never
	// wraps beyond the visible count.
	// The working slices are pooled: each cluster occupies at least one
	// column, so the box width is a sufficient initial capacity, and the
	// common cases append without growing.
	bufs := wrapBuffersPool.Get().(*wrapBuffers)
	cur := bufs.cur[:0]
	if cap(cur) < width {
		cur = make([]string, 0, width)
	}
	word := bufs.word[:0]
	if cap(word) < width {
		word = make([]string, 0, width)
	}
	defer func() {
		bufs.cur = cur
		bufs.word = word
		wrapBuffersPool.Put(bufs)
	}()
	startLen := len(out)
	lines := out
	curWidth := 0
	wordWidth := 0

	// flushLine appends the current line and reports whether more lines
	// may be produced.
	flushLine := func() bool {
		lines = append(lines, strings.Join(cur, ""))
		cur = cur[:0]
		curWidth = 0
		return limit < 0 || len(lines)-startLen < limit
	}

	// packWord appends the current word to the current line, wrapping
	// first if the word would not fit. The word's backing array is
	// reused for the next word. It reports whether more lines may be
	// produced.
	packWord := func() bool {
		if len(word) == 0 {
			return true
		}
		if len(cur) > 0 && curWidth+1+wordWidth > width {
			if !flushLine() {
				return false
			}
		}
		if len(cur) > 0 {
			cur = append(cur, " ")
			curWidth++
		}
		cur = append(cur, word...)
		curWidth += wordWidth
		word = word[:0]
		wordWidth = 0
		return true
	}

	iter.SetText(line)
	for iter.Next() {
		cluster := iter.Value()
		if cluster == " " || cluster == "\t" {
			if !packWord() {
				return lines
			}
			continue
		}
		w := clusterWidth(options, cluster)
		if wordWidth+w > width && len(word) > 0 {
			// The word exceeds the box: pack the word so far, then
			// start a new word with the current cluster.
			if !packWord() {
				return lines
			}
		}
		word = append(word, cluster)
		wordWidth += w
	}
	if !packWord() {
		return lines
	}
	if len(cur) > 0 || len(lines) == startLen {
		lines = append(lines, strings.Join(cur, ""))
	}
	return lines
}

// nextTabStop returns the column of the next tab stop strictly after x,
// relative to the content area's left edge. Floor division handles
// negative offsets (clipped text) so a tab advances to the correct stop.
func nextTabStop(x, contentLeft, tabWidth int) int {
	offset := x - contentLeft
	q := offset / tabWidth
	if offset < 0 && offset%tabWidth != 0 {
		q-- // Go division truncates toward zero; adjust to floor.
	}
	return contentLeft + (q+1)*tabWidth
}

// textCursorX returns the x position of the cursor at the given rune
// offset within a line: the line start plus the width of the text
// before the offset, with tabs expanded to tab stops, clamped to the
// content area.
func textCursorX(line string, offset, lineLeft, contentLeft, right, tabWidth int, options displaywidth.Options) int {
	iter := getGraphemeIter()
	defer putGraphemeIter(iter)
	return textCursorXIter(line, offset, lineLeft, contentLeft, right, tabWidth, options, iter)
}

// textCursorXIter is textCursorX with a caller-provided grapheme
// iterator, so a render pass shares one pooled iterator across the
// line loop and the cursor placement.
func textCursorXIter(line string, offset, lineLeft, contentLeft, right, tabWidth int, options displaywidth.Options, iter *graphemes.Iterator[string]) int {
	if offset < 0 {
		offset = 0
	}
	if n := utf8.RuneCountInString(line); offset > n {
		offset = n
	}
	x := lineLeft
	runeIdx := 0
	iter.SetText(line)
	for iter.Next() {
		cluster := iter.Value()
		clusterRunes := utf8.RuneCountInString(cluster)
		if runeIdx+clusterRunes > offset {
			break
		}
		if cluster == "\t" {
			x = nextTabStop(x, contentLeft, tabWidth)
		} else {
			x += clusterWidth(options, cluster)
		}
		runeIdx += clusterRunes
	}
	if x < contentLeft {
		x = contentLeft
	}
	if x > right {
		x = right
	}
	return x
}
