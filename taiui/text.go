package taiui

import (
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
)

const TheoryOfTextRendering = `
taiui text rendering theory:
- Text rendering optimizes the common case: plain left-aligned,
  top-aligned, non-wrapped, non-filled text, with an optional end cursor,
  is the hottest path in the library, so it renders directly, avoiding
  the general pipeline's line-slice pool and per-line alignment and fill
  overhead.
- The fast path is a strict subset of the general path: it applies only
  when the general path would produce identical output, so the two paths
  never diverge.
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

// renderTextFastPath renders the common case directly: plain
// left-aligned, top-aligned, non-wrapped, non-filled text with an
// optional end cursor, and no offset style or padding. It avoids the
// line-slice pool and the per-line alignment and fill overhead of the
// general path. It returns false when the text does not match the
// fast-path conditions.
func renderTextFastPath(t _Text, box Box, style Style, draw drawFunc, cursor cursorFunc, options displaywidth.Options) bool {
	if t.wrap || t.fill || t.offsetStyleFunc != nil ||
		t.align != AlignLeft || t.valign != VAlignTop ||
		t.padding != [4]int{} || t.cursorAt >= 0 {
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
	for _, ln := range t.lines {
		if y >= box.Bottom {
			break
		}
		x = box.Left
		g := options.StringGraphemes(ln)
		for g.Next() {
			cluster := g.Value()
			if cluster == "\t" {
				// The tab stop is clamped to the box's right edge,
				// matching the general path: a tab never advances past
				// the content area.
				tabStop := nextTabStop(x, box.Left, tabWidth)
				if tabStop > box.Right {
					tabStop = box.Right
				}
				x = tabStop
				continue
			}
			width := g.Width()
			if x >= box.Right || x+width > box.Right {
				break
			}
			mainc, combc := splitCluster(cluster)
			draw(x, y, mainc, combc, style)
			x += width
		}
		y++
	}
	if t.cursor {
		if len(t.lines) > 0 {
			// The cursor is the position after the last drawn cluster of
			// the last line, clamped to the box's right edge: a tab can
			// advance past it, and the general path clamps the same way.
			if x > box.Right {
				x = box.Right
			}
			cursor(x, y-1)
		} else {
			cursor(box.Left, box.Top)
		}
	}
	return true
}

func renderText(t _Text, box Box, style Style, draw drawFunc, cursor cursorFunc, options displaywidth.Options) {
	box = t.effectiveBox(box)
	style = t.styled(style)

	if renderTextFastPath(t, box, style, draw, cursor, options) {
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

	// Compute the wrapped lines first, bounded by the content height, so
	// vertical alignment can place the block before rendering. The box is
	// full when the bound is reached; remaining lines are never processed.
	maxLines := maxY - topY
	if maxLines <= 0 {
		return
	}
	// The lines are pre-sized to the content height so the common case
	// (the text fits the box) appends without growing the slice. The
	// slice is pooled: a Text renders its lines into a fresh slice per
	// pass, and pooling avoids the allocation for the common
	// screen-sized texts.
	lines := textLinesPool.Get().([]string)
	lines = lines[:0]
	if cap(lines) < min(len(t.lines), maxLines) {
		lines = make([]string, 0, min(len(t.lines), maxLines))
	}
	defer func() { textLinesPool.Put(lines) }()
	for _, line := range t.lines {
		if t.wrap {
			// The limit is the remaining visible line count: a long
			// line in a small box never wraps beyond the rows the box
			// can show, so pathological inputs cannot accumulate
			// unbounded wrapped lines.
			wrapped := wrapLineLimited(line, wrapWidth, maxLines-len(lines), options)
			lines = append(lines, wrapped...)
		} else {
			if len(lines) >= maxLines {
				break
			}
			// Unwrapped lines append directly, so a line never allocates
			// a one-element wrapper slice.
			lines = append(lines, line)
		}
		if len(lines) >= maxLines {
			break
		}
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
			left = right - options.String(ln)
		case AlignCenter:
			// Centering is relative to the padded content area and
			// rounds with the conventional (width-len)/2 rule, so an
			// odd-width line places the extra column on the right.
			left = (contentLeft + right - options.String(ln)) / 2
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
		g := options.StringGraphemes(ln)
		for g.Next() {
			cluster := g.Value()
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
			width := g.Width()
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
			mainc, combc := splitCluster(cluster)
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
				cursor(textCursorX(lines[len(lines)-1], t.cursorAt, lastLineStart, contentLeft, right, tabWidth, options), y-1)
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
// combining runes that follow it.
func splitCluster(cluster string) (rune, []rune) {
	var base rune
	var combc []rune
	for i, r := range cluster {
		if i == 0 {
			base = r
		} else {
			combc = append(combc, r)
		}
	}
	return base, combc
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

// wrapLineLimited wraps a line to the given width, producing at most
// limit lines. A negative limit is unlimited. The render path passes the
// remaining visible line count, so a long line in a small box never wraps
// beyond the rows the box can show.
func wrapLineLimited(line string, width, limit int, options displaywidth.Options) []string {
	if width <= 0 || limit == 0 {
		return nil
	}
	if line == "" {
		return []string{""}
	}
	// Fast path: a line with no tabs, no leading or trailing spaces, and
	// no consecutive spaces that fits the box is returned as-is. The
	// slow path would join the same words with single spaces, so the
	// output is identical; skipping the word-splitting allocations is
	// the win.
	if !strings.Contains(line, "\t") &&
		!strings.HasPrefix(line, " ") &&
		!strings.HasSuffix(line, " ") &&
		!strings.Contains(line, "  ") &&
		options.String(line) <= width {
		return []string{line}
	}

	// Single pass over the grapheme clusters: words are packed onto the
	// current line as they complete, so no word list is built. A word
	// wider than the box is packed in chunks as it grows. The limit
	// bounds the produced lines, so a long line in a small box never
	// wraps beyond the visible count.
	var lines []string
	var cur []string
	curWidth := 0
	var word []string
	wordWidth := 0

	// flushLine appends the current line and reports whether more lines
	// may be produced.
	flushLine := func() bool {
		lines = append(lines, strings.Join(cur, ""))
		cur = cur[:0]
		curWidth = 0
		return limit < 0 || len(lines) < limit
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

	g := options.StringGraphemes(line)
	for g.Next() {
		cluster := g.Value()
		if cluster == " " || cluster == "\t" {
			if !packWord() {
				return lines
			}
			continue
		}
		w := g.Width()
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
	if len(cur) > 0 || len(lines) == 0 {
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

// textCursorX computes the x position of a cursor at the given rune
// offset within a line: the line start plus the width of the text before
// the offset, with tabs expanded to tab stops, clamped to the content
// area.
func textCursorX(line string, offset, lineLeft, contentLeft, right, tabWidth int, options displaywidth.Options) int {
	if offset < 0 {
		offset = 0
	}
	if n := utf8.RuneCountInString(line); offset > n {
		offset = n
	}
	x := lineLeft
	runeIdx := 0
	g := options.StringGraphemes(line)
	for g.Next() {
		cluster := g.Value()
		clusterRunes := utf8.RuneCountInString(cluster)
		if runeIdx+clusterRunes > offset {
			break
		}
		if cluster == "\t" {
			x = nextTabStop(x, contentLeft, tabWidth)
		} else {
			x += g.Width()
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
