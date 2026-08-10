package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/vt"
	"github.com/reusee/tai/taiui"
)

const TheoryOfANSScreen = `
taiuidemo ANSI screen theory:
- The presenter renders a Frame to the terminal by repainting whole dirty
  rows, never individual cells: a wide grapheme cluster leaves its trailing
  columns as unset cells, so a run-level repaint could leave a stale
  half-glyph when a wide cluster moves away.
- An entirely unset row is cleared with the erase-line sequence (EL) in a
  single write instead of one space per cell, and runs of unset cells
  within a row are batched into one write. A run reaching the row end is
  erased to the line end so the cursor never wraps at the last column.
  The reset parameter precedes the erase so the line clears to the
  default background. The first set cell is located in a single scan;
  the unset run before it is batched in one write, so a row with content
  is scanned once, not twice.
- Frames with no dirty rows and an unchanged cursor are skipped entirely,
  and the first frame repaints the whole screen.
- The presenter keeps the last presented frame for damage comparison by
  copying the cell slice, so the presented frame can be returned to the
  frame pool by the renderer. The copy is a plain slice copy; the
  combining-rune slices and style values are shared, which is safe
  because the renderer never mutates a cell after drawing it.
- The presenter derives the display-width options per present and measures
  clusters with taiui.ClusterWidth, so the terminal cursor advances by the
  same columns the renderer allocated even if the environment changes
  between renders.
- The presenter positions the terminal cursor at the Frame's recorded
  cursor position when the frame carries one, so a text input's cursor
  tracks the rendered text.
- Every SGR sequence starts with the reset parameter: a style is the
  complete terminal state, so an attribute absent from it is cleared.
  Without the reset, a plain cell after an underlined or overlined run
  would keep the attribute and bleed the line into any following cell
  that shares the same background.
- SGR sequences are memoized by the style's SGR-relevant fields (the
  attribute bits and the three colors): the demo's style set is small
  and bounded, so the cache stays small, and the presenter is
  single-threaded, so a plain map needs no locking.
`

type ansiScreen struct {
	w      io.Writer
	width  int
	height int
	last   taiui.Frame
	bw     *bufio.Writer
}

func (s *ansiScreen) Width() int  { return s.width }
func (s *ansiScreen) Height() int { return s.height }

func (s *ansiScreen) resize(width, height int) {
	s.width, s.height = width, height
	s.last = taiui.Frame{}
	io.WriteString(s.w, "\x1b[2J\x1b[H")
}

func (s *ansiScreen) Present(frame taiui.Frame) {
	// Whole rows are repainted, not just the dirty runs: a wide
	// cluster's trailing columns are never set cells, so a run-based
	// repaint could leave a stale half-glyph when a wide cluster moves
	// away.
	var dirtyRows []int
	if s.last.Width == 0 {
		io.WriteString(s.w, "\x1b[2J")
		dirtyRows = make([]int, frame.Height)
		for i := range dirtyRows {
			dirtyRows[i] = i
		}
	} else {
		dirtyRows = frame.DirtyRows(s.last)
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
	options := taiui.DisplayWidthOptions()
	if s.bw == nil {
		s.bw = bufio.NewWriter(s.w)
	}
	bw := s.bw
	for _, y := range dirtyRows {
		writeCursorPos(bw, 0, y)
		paintRow(bw, &frame, y, options)
	}
	bw.Flush()
	if frame.CursorSet {
		writeCursorPos(s.w, frame.CursorX, frame.CursorY)
	}
	// Retain a copy of the frame for the next damage comparison. The
	// cells are copied by value, so the presented frame can be returned
	// to the frame pool by the renderer. The combining-rune slices and
	// style values are shared with the frame's cells, which is safe
	// because the renderer never mutates a cell after drawing it.
	if cap(s.last.Cells) < len(frame.Cells) {
		s.last.Cells = make([]taiui.FrameCell, len(frame.Cells))
	} else {
		s.last.Cells = s.last.Cells[:len(frame.Cells)]
	}
	copy(s.last.Cells, frame.Cells)
	s.last.Width, s.last.Height = frame.Width, frame.Height
	s.last.CursorSet, s.last.CursorX, s.last.CursorY = frame.CursorSet, frame.CursorX, frame.CursorY
}

// ReleaseFrame returns the presented frame's cells to the frame pool.
// The screen keeps its own copy of the frame, so the cells are no longer
// needed after Present returns.
func (s *ansiScreen) ReleaseFrame(frame taiui.Frame) {
	taiui.ReleaseFrame(frame)
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

func paintRow(w io.Writer, frame *taiui.Frame, y int, options displaywidth.Options) {
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
	var lastStyle taiui.Style
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
		if lastStyle == nil || !lastStyle.Equal(cell.Style) {
			io.WriteString(w, sgr(cell.Style))
			lastStyle = cell.Style
		}
		writeCluster(w, cell.Rune, cell.Combc)
		if cw := taiui.ClusterWidth(options, cell.Rune, cell.Combc); cw > 1 {
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
	fg   color.Color
	bg   color.Color
	uc   color.Color
}

// sgrCache memoizes SGR sequences by their key. The demo's style set is
// small and bounded, so the cache stays small; the presenter is
// single-threaded, so a plain map needs no locking.
var sgrCache = make(map[sgrKey]string, 32)

// sgr renders a style as SGR parameters, memoized by the style's
// SGR-relevant fields (the attribute bits and the three colors).
func sgr(style taiui.Style) string {
	if style == nil {
		return "\x1b[0m"
	}
	key := sgrKey{
		attr: style.Attr(),
		fg:   style.Fg(),
		bg:   style.Bg(),
		uc:   style.Uc(),
	}
	if s, ok := sgrCache[key]; ok {
		return s
	}
	s := buildSGR(key.attr, key.fg, key.bg, key.uc)
	sgrCache[key] = s
	return s
}

// buildSGR renders the SGR-relevant style fields as SGR parameters.
// Every sequence starts with the reset parameter: a style describes the
// complete terminal state, so an attribute absent from it must be
// cleared, or a plain cell after an underlined or overlined run would
// keep the previous attribute and bleed the line into neighboring cells.
func buildSGR(attr vt.Attr, fg, bg, uc color.Color) string {
	var parts []string
	if attr&vt.Bold != 0 {
		parts = append(parts, "1")
	}
	if attr&vt.Dim != 0 {
		parts = append(parts, "2")
	}
	if attr&vt.Italic != 0 {
		parts = append(parts, "3")
	}
	switch u := attr & vt.UnderlineMask; u {
	case vt.PlainUnderline:
		parts = append(parts, "4")
	case vt.DoubleUnderline:
		parts = append(parts, "4:2")
	case vt.CurlyUnderline:
		parts = append(parts, "4:3")
	case vt.DottedUnderline:
		parts = append(parts, "4:4")
	case vt.DashedUnderline:
		parts = append(parts, "4:5")
	}
	if attr&vt.Blink != 0 {
		parts = append(parts, "5")
	}
	if attr&vt.Reverse != 0 {
		parts = append(parts, "7")
	}
	if attr&vt.StrikeThrough != 0 {
		parts = append(parts, "9")
	}
	if attr&vt.Overline != 0 {
		parts = append(parts, "53")
	}
	// An unset color is a valid-but-colorless sentinel in the vt style;
	// RGB() reports it as -1, so only real colors emit SGR parameters.
	if c := fg; c.Valid() {
		if r, g, b := c.RGB(); r >= 0 && g >= 0 && b >= 0 {
			parts = append(parts, colorSGR(c, "38"))
		}
	}
	if c := bg; c.Valid() {
		if r, g, b := c.RGB(); r >= 0 && g >= 0 && b >= 0 {
			parts = append(parts, colorSGR(c, "48"))
		}
	}
	if c := uc; c.Valid() {
		if r, g, b := c.RGB(); r >= 0 && g >= 0 && b >= 0 {
			parts = append(parts, colorSGR(c, "58"))
		}
	}
	if len(parts) == 0 {
		return "\x1b[0m"
	}
	return "\x1b[0;" + strings.Join(parts, ";") + "m"
}

// colorSGR renders one color as an SGR parameter: true color for RGB
// values, 256-color palette index otherwise.
func colorSGR(c color.Color, prefix string) string {
	if c&color.IsRGB != 0 {
		r, g, b := c.RGB()
		return fmt.Sprintf("%s;2;%d;%d;%d", prefix, r, g, b)
	}
	return fmt.Sprintf("%s;5;%d", prefix, int(c&0xff))
}

func writeCluster(w io.Writer, mainc rune, combc []rune) {
	if len(combc) == 0 {
		var buf [4]byte
		n := utf8.EncodeRune(buf[:], mainc)
		w.Write(buf[:n])
		return
	}
	var buf [8]byte
	b := buf[:0]
	b = utf8.AppendRune(b, mainc)
	for _, r := range combc {
		b = utf8.AppendRune(b, r)
	}
	w.Write(b)
}
