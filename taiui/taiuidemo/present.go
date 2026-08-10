package main

import (
	"bufio"
	"fmt"
	"io"
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
  default background.
- Frames that Equal the last presented frame are skipped entirely, and the
  first frame repaints the whole screen.
- The presenter derives the display-width options per present, so the
  terminal cursor advances by the same columns the renderer allocated
  even if the environment changes between renders.
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
	last   *taiui.Frame
	bw     *bufio.Writer
}

func (s *ansiScreen) Width() int  { return s.width }
func (s *ansiScreen) Height() int { return s.height }

func (s *ansiScreen) resize(width, height int) {
	s.width, s.height = width, height
	s.last = nil
	io.WriteString(s.w, "\x1b[2J\x1b[H")
}

func (s *ansiScreen) Present(frame taiui.Frame) {
	if s.last != nil && frame.Equal(*s.last) {
		return
	}
	// Whole rows are repainted, not just the dirty runs: a wide cluster's
	// trailing columns are never set cells, so a run-based repaint could
	// leave a stale half-glyph when a wide cluster moves away.
	var dirtyRows []int
	if s.last == nil {
		io.WriteString(s.w, "\x1b[2J")
		dirtyRows = make([]int, frame.Height)
		for i := range dirtyRows {
			dirtyRows[i] = i
		}
	} else {
		dirtyRows = frame.DirtyRows(*s.last)
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
		fmt.Fprintf(bw, "\x1b[%d;1H", y+1)
		paintRow(bw, &frame, y, options)
	}
	bw.Flush()
	if frame.CursorSet {
		fmt.Fprintf(s.w, "\x1b[%d;%dH", frame.CursorY+1, frame.CursorX+1)
	}
	s.last = &frame
}

func paintRow(w io.Writer, frame *taiui.Frame, y int, options displaywidth.Options) {
	row := frame.Cells[y*frame.Width : (y+1)*frame.Width]
	// Fast path: an entirely unset row is cleared with the erase-line
	// sequence in a single write instead of one space per cell. The
	// reset parameter precedes the erase so the line clears to the
	// default background.
	allUnset := true
	for i := range row {
		if row[i].Set {
			allUnset = false
			break
		}
	}
	if allUnset {
		io.WriteString(w, "\x1b[0m\x1b[2K")
		return
	}
	var lastStyle taiui.Style
	for x := 0; x < frame.Width; x++ {
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
			} else if x == start {
				io.WriteString(w, " ")
			} else {
				io.WriteString(w, strings.Repeat(" ", x-start+1))
			}
			continue
		}
		if lastStyle == nil || !lastStyle.Equal(cell.Style) {
			io.WriteString(w, sgr(cell.Style))
			lastStyle = cell.Style
		}
		writeCluster(w, cell.Rune, cell.Combc)
		if cw := clusterWidth(cell, options); cw > 1 {
			x += cw - 1
		}
	}
	if lastStyle != nil {
		io.WriteString(w, "\x1b[0m")
	}
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

func clusterWidth(cell taiui.FrameCell, options displaywidth.Options) int {
	if len(cell.Combc) == 0 {
		return options.Rune(cell.Rune)
	}
	var buf [8]byte
	b := buf[:0]
	b = utf8.AppendRune(b, cell.Rune)
	for _, r := range cell.Combc {
		b = utf8.AppendRune(b, r)
	}
	return options.Bytes(b)
}
