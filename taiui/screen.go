// taiui/screen.go
package taiui

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/vt"
)

const TheoryOfTerminalScreen = `
taiui terminal screen theory:
- The terminal screen renders a Frame to a terminal by repainting whole
  dirty rows, never individual cells: a wide grapheme cluster leaves
  its trailing columns as unset cells, so a finer-grained repaint could
  leave a stale half-glyph when a wide cluster moves away.
- Blank regions are written efficiently: an entirely unset row is
  cleared in a single operation, runs of unset cells are batched, and a
  run reaching the row end is erased to the line end so the cursor
  never wraps at the last column. The reset precedes the erase so the
  line clears to the default background.
- Frames with no dirty rows and an unchanged cursor are skipped
  entirely, and the first frame repaints the whole screen. The cursor
  position is written only when it changed.
- The screen retains a copy of the last presented frame for damage
  comparison and reuses its comparison buffer across presents, so a
  frame comparison allocates nothing. It implements FrameReleaser: it
  copies the presented frame's cells into a retained buffer and returns
  the presented cells to the pool, so the renderer reuses them and the
  per-pass frame allocation is eliminated.
- The screen derives the display-width options per present and measures
  clusters with the shared exported measurement, so the terminal cursor
  advances by the same columns the renderer allocated even if the
  environment changes between renders.
- The screen positions the terminal cursor at the Frame's recorded
  cursor position when the frame carries one, so a text input's cursor
  tracks the rendered text.
- Every style sequence starts with the reset parameter: a style is the
  complete terminal state, so an attribute absent from it is cleared.
  Without the reset, a plain cell after an underlined run would keep
  the attribute and bleed the line into any following cell that shares
  the same background.
- Style sequences are memoized by the style's SGR-relevant fields: a
  screen's style set is small and bounded, and the screen is
  single-threaded, so a plain map needs no locking.
`

const TheoryOfTerminalScreenRetention = `
taiui terminal screen retention theory:
- The retained frame copy is updated only for the dirty rows: rows not
  in the dirty set are already identical to the retained frame, so a
  partial repaint copies cells proportional to the damage instead of
  the whole screen.
- The per-row style comparison checks pointer identity before the
  equality method: a render pass shares style values, so most style
  changes are detected by pointer comparison, and the equality method
  is called only for distinct style values.
`

// TerminalScreen renders Frames to a terminal via ANSI escape sequences.
// It repaints whole dirty rows, so a wide grapheme cluster's trailing
// columns never leave a stale half-glyph when the cluster moves away.
type TerminalScreen struct {
	w         io.Writer
	width     int
	height    int
	last      Frame
	bw        *bufio.Writer
	dirtyRows []int
	sgrCache  map[sgrKey]string
}

// NewTerminalScreen creates a terminal screen that renders to w with the
// given dimensions.
func NewTerminalScreen(w io.Writer, width, height int) *TerminalScreen {
	return &TerminalScreen{
		w:        w,
		width:    width,
		height:   height,
		sgrCache: make(map[sgrKey]string, 32),
	}
}

func (s *TerminalScreen) Width() int  { return s.width }
func (s *TerminalScreen) Height() int { return s.height }

// Resize changes the screen dimensions and clears the terminal.
func (s *TerminalScreen) Resize(width, height int) {
	s.width, s.height = width, height
	s.last = Frame{}
	io.WriteString(s.w, "\x1b[2J\x1b[H")
}

func (s *TerminalScreen) Present(frame Frame) {
	// Whole rows are repainted, not just the dirty runs: a wide
	// cluster's trailing columns are never set cells, so a run-based
	// repaint could leave a stale half-glyph when a wide cluster moves
	// away.
	var dirtyRows []int
	if s.last.Width == 0 {
		io.WriteString(s.w, "\x1b[2J")
		// The dirty-rows buffer is reused across presents, so a frame
		// comparison allocates nothing.
		s.dirtyRows = s.dirtyRows[:0]
		for i := 0; i < frame.Height; i++ {
			s.dirtyRows = append(s.dirtyRows, i)
		}
		dirtyRows = s.dirtyRows
	} else {
		// The dirty-rows buffer is reused across presents, so a frame
		// comparison allocates nothing.
		s.dirtyRows = frame.DirtyRowsInto(s.last, s.dirtyRows[:0])
		dirtyRows = s.dirtyRows
		// A frame with no dirty rows and an unchanged cursor is
		// identical to the last presented frame: skip the repaint.
		// DirtyRows alone cannot detect a cursor-only change, so the
		// cursor state is compared here.
		if len(dirtyRows) == 0 &&
			frame.CursorSet == s.last.CursorSet &&
			frame.CursorX == s.last.CursorX &&
			frame.CursorY == s.last.CursorY {
			return
		}
	}
	// The options are derived per present, so the terminal cursor advances
	// by the same columns the renderer allocated even if the environment
	// changes between renders.
	options := DisplayWidthOptions()
	if s.bw == nil {
		s.bw = bufio.NewWriter(s.w)
	}
	bw := s.bw
	for _, y := range dirtyRows {
		writeCursorPos(bw, 0, y)
		s.paintRow(bw, &frame, y, options)
	}
	if frame.CursorSet && (!s.last.CursorSet || frame.CursorX != s.last.CursorX || frame.CursorY != s.last.CursorY) {
		// The cursor position is written to the buffered writer, so it
		// is flushed with the rows in one write. It is written only when
		// it changed: a repaint with a stationary cursor does not
		// rewrite the position.
		writeCursorPos(bw, frame.CursorX, frame.CursorY)
	}
	bw.Flush()
	// Retain a copy of the frame for the next damage comparison. The
	// retained buffer is reused across presents, so the per-pass frame
	// allocation is eliminated; ReleaseFrame returns the frame's cells
	// to the pool. Rows not in dirtyRows are already identical to the
	// retained frame, so only the dirty rows are copied: a partial
	// repaint copies O(dirty) cells instead of the whole screen.
	if s.last.Width != frame.Width || s.last.Height != frame.Height {
		s.last = Frame{
			Width:  frame.Width,
			Height: frame.Height,
			Cells:  make([]FrameCell, len(frame.Cells)),
		}
		copy(s.last.Cells, frame.Cells)
	} else {
		for _, y := range dirtyRows {
			copy(s.last.Cells[y*frame.Width:(y+1)*frame.Width], frame.Cells[y*frame.Width:(y+1)*frame.Width])
		}
	}
	s.last.CursorSet = frame.CursorSet
	s.last.CursorX = frame.CursorX
	s.last.CursorY = frame.CursorY
}

// ReleaseFrame returns the presented frame's cells to the pool. The
// screen retains its own copy of the frame, so the cells are never
// referenced after Present returns.
func (s *TerminalScreen) ReleaseFrame(frame Frame) {
	ReleaseFrame(frame)
}

// writeCursorPos writes a cursor-position sequence to w without
// allocating.
func writeCursorPos(w io.Writer, x, y int) {
	var buf [16]byte
	b := append(buf[:0], '\x1b', '[')
	b = strconv.AppendInt(b, int64(y+1), 10)
	b = append(b, ';')
	b = strconv.AppendInt(b, int64(x+1), 10)
	b = append(b, 'H')
	w.Write(b)
}

// spaces is a pre-allocated run of spaces for batching unset cells.
// Runs longer than this are written in chunks, so a long unset run
// never allocates a fresh string.
var spaces = strings.Repeat(" ", 64)

func (s *TerminalScreen) paintRow(w io.Writer, frame *Frame, y int, options displaywidth.Options) {
	row := frame.Cells[y*frame.Width : (y+1)*frame.Width]
	// Locate the first set cell in a single scan; a row with none is
	// cleared with the erase-line sequence in a single write instead of
	// one space per cell. The reset parameter precedes the erase so the
	// line clears to the default background.
	firstSet := -1
	for i := range row {
		if row[i].Set {
			firstSet = i
			break
		}
	}
	if firstSet < 0 {
		io.WriteString(w, "\x1b[0m\x1b[2K")
		return
	}
	var lastStyle Style
	// The unset run before the first set cell is batched into one write.
	// It never reaches the row end, so no erase is needed here.
	if firstSet > 0 {
		writeUnsetRun(w, firstSet)
	}
	for x := firstSet; x < frame.Width; x++ {
		cell := row[x]
		if !cell.Set {
			if lastStyle != nil {
				io.WriteString(w, "\x1b[0m")
				lastStyle = nil
			}
			// Consecutive unset cells are batched into one write. A run
			// reaching the row end is erased to the line end instead, so
			// the cursor never wraps at the last column.
			start := x
			for x+1 < frame.Width && !row[x+1].Set {
				x++
			}
			if x == frame.Width-1 {
				io.WriteString(w, "\x1b[K")
			} else {
				writeUnsetRun(w, x-start+1)
			}
			continue
		}
		// The style comparison checks pointer identity first: a render
		// pass shares style values, so most style changes are detected
		// by pointer comparison, and Equal is called only for distinct
		// style values.
		if lastStyle == nil || (lastStyle != cell.Style && !lastStyle.Equal(cell.Style)) {
			io.WriteString(w, s.sgr(cell.Style))
			lastStyle = cell.Style
		}
		writeCluster(w, cell.Rune, cell.Combc)
		if cw := ClusterWidth(options, cell.Rune, cell.Combc); cw > 1 {
			x += cw - 1
		}
	}
	if lastStyle != nil {
		io.WriteString(w, "\x1b[0m")
	}
}

// writeUnsetRun writes n blank cells in chunks of the pre-allocated
// spaces run, so a long unset run never allocates a fresh string.
func writeUnsetRun(w io.Writer, n int) {
	for n > len(spaces) {
		io.WriteString(w, spaces)
		n -= len(spaces)
	}
	io.WriteString(w, spaces[:n])
}

// sgrKey is the canonical key for the SGR output of a style: the sequence
// depends only on the attribute bits and the three colors.
type sgrKey struct {
	attr vt.Attr
	fg   Color
	bg   Color
	uc   Color
}

// sgr renders a style as SGR parameters, memoized by the style's
// SGR-relevant fields (the attribute bits and the three colors).
func (s *TerminalScreen) sgr(style Style) string {
	if style == nil {
		return "\x1b[0m"
	}
	key := sgrKey{
		attr: style.Attr(),
		fg:   style.Fg(),
		bg:   style.Bg(),
		uc:   style.Uc(),
	}
	if s.sgrCache == nil {
		s.sgrCache = make(map[sgrKey]string, 32)
	}
	if seq, ok := s.sgrCache[key]; ok {
		return seq
	}
	seq := buildSGR(key.attr, key.fg, key.bg, key.uc)
	s.sgrCache[key] = seq
	return seq
}

// appendColorSGR appends one color as an SGR parameter: true color for
// RGB values, 256-color palette index otherwise.
func appendColorSGR(b []byte, c Color, prefix string) []byte {
	b = append(b, ';')
	b = append(b, prefix...)
	if c&color.IsRGB != 0 {
		r, g, bl := c.RGB()
		b = append(b, ';', '2', ';')
		b = strconv.AppendInt(b, int64(r), 10)
		b = append(b, ';')
		b = strconv.AppendInt(b, int64(g), 10)
		b = append(b, ';')
		b = strconv.AppendInt(b, int64(bl), 10)
		return b
	}
	b = append(b, ';', '5', ';')
	b = strconv.AppendInt(b, int64(c&0xff), 10)
	return b
}

// buildSGR renders the SGR-relevant style fields as SGR parameters.
// Every sequence starts with the reset parameter: a style describes the
// complete terminal state, so an attribute absent from it must be
// cleared, or a plain cell after an underlined or overlined run would
// keep the previous attribute and bleed the line into neighboring cells.
func buildSGR(attr vt.Attr, fg, bg, uc Color) string {
	// The stack buffer covers the worst case: every attribute plus three
	// true-color parameters, so a style never allocates.
	var buf [96]byte
	s := append(buf[:0], '\x1b', '[', '0')
	if attr&vt.Bold != 0 {
		s = append(s, ';', '1')
	}
	if attr&vt.Dim != 0 {
		s = append(s, ';', '2')
	}
	if attr&vt.Italic != 0 {
		s = append(s, ';', '3')
	}
	switch u := attr & vt.UnderlineMask; u {
	case vt.PlainUnderline:
		s = append(s, ';', '4')
	case vt.DoubleUnderline:
		s = append(s, ';', '4', ':', '2')
	case vt.CurlyUnderline:
		s = append(s, ';', '4', ':', '3')
	case vt.DottedUnderline:
		s = append(s, ';', '4', ':', '4')
	case vt.DashedUnderline:
		s = append(s, ';', '4', ':', '5')
	}
	if attr&vt.Blink != 0 {
		s = append(s, ';', '5')
	}
	if attr&vt.Reverse != 0 {
		s = append(s, ';', '7')
	}
	if attr&vt.StrikeThrough != 0 {
		s = append(s, ';', '9')
	}
	if attr&vt.Overline != 0 {
		s = append(s, ';', '5', '3')
	}
	// An unset color is a valid-but-colorless sentinel in the vt style;
	// RGB() reports it as -1, so only real colors emit SGR parameters.
	if c := fg; c.Valid() {
		if r, g, b := c.RGB(); r >= 0 && g >= 0 && b >= 0 {
			s = appendColorSGR(s, c, "38")
		}
	}
	if c := bg; c.Valid() {
		if r, g, b := c.RGB(); r >= 0 && g >= 0 && b >= 0 {
			s = appendColorSGR(s, c, "48")
		}
	}
	if c := uc; c.Valid() {
		if r, g, b := c.RGB(); r >= 0 && g >= 0 && b >= 0 {
			s = appendColorSGR(s, c, "58")
		}
	}
	if len(s) == 3 {
		return "\x1b[0m"
	}
	s = append(s, 'm')
	return string(s)
}

func writeCluster(w io.Writer, mainc rune, combc []rune) {
	if len(combc) == 0 {
		var buf [4]byte
		n := utf8.EncodeRune(buf[:], mainc)
		w.Write(buf[:n])
		return
	}
	var buf [16]byte
	b := buf[:0]
	b = utf8.AppendRune(b, mainc)
	for _, r := range combc {
		b = utf8.AppendRune(b, r)
	}
	w.Write(b)
}

// DiscardScreen is a Screen that discards every presented frame and
// returns its cells to the pool. It is useful for benchmarks and for
// rendering to no visible target.
type DiscardScreen struct{}

func (DiscardScreen) Width() int    { return 1 }
func (DiscardScreen) Height() int   { return 1 }
func (DiscardScreen) Present(Frame) {}

// ReleaseFrame returns the discarded frame's cells to the pool: the
// screen renders and discards, so the cells are never retained.
func (DiscardScreen) ReleaseFrame(frame Frame) {
	ReleaseFrame(frame)
}
