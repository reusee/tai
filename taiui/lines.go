package taiui

import (
	"strings"

	"github.com/gdamore/tcell/v3/color"
)

const TheoryOfLines = `
taiui TUI content lines theory:
- Line carries display text with optional foreground and background
  colors. The zero color value means the default foreground or no
  background, so a Line with no colors is plain text.
- The line buffer accumulates streamed output chunks into complete
  lines, retaining the incomplete trailing line and the color of the
  chunk that started it. Newlines split lines; a partial line keeps its
  color until the next newline arrives. Lines are bounded by a maximum
  count, so a runaway stream cannot grow the buffer without limit.
  Completed lines and the partial line are exposed without copying for
  incremental processing. A plain-text variant offers the same
  accumulation for line-oriented inspection.
- Wrapping carries each source line's foreground and background colors
  onto every wrapped display line, so a wrapped line keeps its role
  color; a variant appends the wrapped lines to an existing slice
  instead of allocating a fresh one.
- Plain text lines can be converted into lines with alternating
  background shades so consecutive log entries are visually distinct.
  The alternate shade shifts each channel of the base background toward
  the mid-gray, so the alternation stays visible on both light and dark
  tab backgrounds. An unset base (NoColor) disables the alternation:
  AltBG returns the base unchanged when no background is set, so
  backgrounds and their alternation appear only when configured.
- Rendering turns a set of lines into a single element: consecutive
  lines with identical colors are grouped into one text carrying the
  group's colors, so a wrapped log line keeps one background across its
  display rows and consecutive groups are visually distinct.
`

// AltBGDiff is the per-channel shift of the alternate log line background
// toward the mid-gray. It is small enough to stay subtle on dark tab
// backgrounds and large enough to be visible.
const AltBGDiff = 12

// Line is one display line with optional foreground and background colors.
// The zero Color value means the default foreground or no background.
type Line struct {
	Text    string
	Color   Color
	BGColor Color
}

// NoColor is the zero color value: default foreground or no background.
var NoColor = color.Default

// LineBuffer accumulates streamed colored output into complete lines,
// retaining the incomplete trailing line. See TheoryOfLines.
type LineBuffer struct {
	lines    []Line
	partial  Line
	maxLines int
}

// NewLineBuffer creates a LineBuffer. A maxLines of 0 means unlimited.
func NewLineBuffer(maxLines int) *LineBuffer {
	return &LineBuffer{maxLines: maxLines}
}

// Append adds output bytes, splitting them into lines. A line keeps the
// color of the chunk that started it; a partial line retains its color
// until the next newline arrives.
func (b *LineBuffer) Append(color Color, text string) {
	if b.partial.Text == "" {
		b.partial.Color = color
	}
	b.partial.Text += text
	for {
		idx := strings.IndexByte(b.partial.Text, '\n')
		if idx < 0 {
			break
		}
		line := b.partial.Text[:idx]
		b.lines = append(b.lines, Line{Text: line, Color: b.partial.Color, BGColor: NoColor})
		b.partial.Text = b.partial.Text[idx+1:]
		b.partial.Color = color
		if b.maxLines > 0 && len(b.lines) > b.maxLines {
			b.lines = append([]Line(nil), b.lines[len(b.lines)-b.maxLines:]...)
		}
	}
}

// Lines returns the complete lines, including the partial trailing line
// when one exists.
func (b *LineBuffer) Lines() []Line {
	if b.partial.Text == "" {
		return b.lines
	}
	ret := make([]Line, 0, len(b.lines)+1)
	ret = append(ret, b.lines...)
	ret = append(ret, b.partial)
	return ret
}

// CompletedLines returns the slice of completed lines directly, without
// copying.
func (b *LineBuffer) CompletedLines() []Line {
	return b.lines
}

// Partial returns the incomplete trailing line.
func (b *LineBuffer) Partial() Line {
	return b.partial
}

// HasPartial reports whether an incomplete trailing line exists.
func (b *LineBuffer) HasPartial() bool {
	return b.partial.Text != ""
}

// StringBuffer accumulates plain text into complete lines, exposing newly
// completed lines to the caller. See TheoryOfLines.
type StringBuffer struct {
	lines    []string
	partial  string
	maxLines int
}

// NewStringBuffer creates a StringBuffer. A maxLines of 0 means unlimited.
func NewStringBuffer(maxLines int) *StringBuffer {
	return &StringBuffer{maxLines: maxLines}
}

// Append adds bytes and returns the newly completed lines. The incomplete
// trailing line is retained for the next Append.
func (b *StringBuffer) Append(p []byte) []string {
	if len(p) == 0 {
		return nil
	}
	b.partial += string(p)
	var completed []string
	for {
		idx := strings.IndexByte(b.partial, '\n')
		if idx < 0 {
			break
		}
		line := b.partial[:idx]
		b.lines = append(b.lines, line)
		completed = append(completed, line)
		b.partial = b.partial[idx+1:]
		if b.maxLines > 0 && len(b.lines) > b.maxLines {
			b.lines = append([]string(nil), b.lines[len(b.lines)-b.maxLines:]...)
		}
	}
	return completed
}

// Lines returns the complete lines, including the partial trailing line
// when one exists.
func (b *StringBuffer) Lines() []string {
	if b.partial == "" {
		return b.lines
	}
	ret := make([]string, 0, len(b.lines)+1)
	ret = append(ret, b.lines...)
	ret = append(ret, b.partial)
	return ret
}

// CompletedLines returns the slice of completed lines directly, without
// copying.
func (b *StringBuffer) CompletedLines() []string {
	return b.lines
}

// Partial returns the incomplete trailing line.
func (b *StringBuffer) Partial() string {
	return b.partial
}

// HasPartial reports whether an incomplete trailing line exists.
func (b *StringBuffer) HasPartial() bool {
	return b.partial != ""
}

// WrapLinesColored wraps each source line at the given width and carries
// the line's foreground and background colors onto every wrapped display
// line.
func WrapLinesColored(lines []Line, width int) []Line {
	return WrapLinesColoredInto(lines, width, make([]Line, 0, len(lines)))
}

// WrapLinesColoredInto wraps each source line at the given width and
// appends the wrapped display lines to out, reusing a single grapheme
// iterator across all lines.
func WrapLinesColoredInto(lines []Line, width int, out []Line) []Line {
	if len(lines) == 0 {
		return out
	}
	options := DisplayWidthOptions()
	iter := getGraphemeIter()
	defer putGraphemeIter(iter)
	var wrapped []string
	for _, line := range lines {
		wrapped = wrapLineLimitedIter(line.Text, width, -1, options, iter, defaultTabWidth, wrapped[:0])
		for _, text := range wrapped {
			out = append(out, Line{Text: text, Color: line.Color, BGColor: line.BGColor})
		}
	}
	return out
}

// WrapPlainLinesInto wraps plain text lines with alternating background
// shades and appends the wrapped display lines to out. startIdx is the
// logical line index of the first line in lines, ensuring alternating
// background continuity across batches.
func WrapPlainLinesInto(lines []string, base Color, width int, startIdx int, out []Line) []Line {
	if len(lines) == 0 {
		return out
	}
	options := DisplayWidthOptions()
	iter := getGraphemeIter()
	defer putGraphemeIter(iter)
	alt := AltBG(base)
	var wrapped []string
	for i, line := range lines {
		bg := base
		if (startIdx+i)%2 == 1 {
			bg = alt
		}
		wrapped = wrapLineLimitedIter(line, width, -1, options, iter, defaultTabWidth, wrapped[:0])
		for _, text := range wrapped {
			out = append(out, Line{Text: text, BGColor: bg, Color: NoColor})
		}
	}
	return out
}

// AltBG returns the alternate shade for odd-numbered log lines: each
// channel of the base background is shifted toward the mid-gray, so
// the alternation stays visible on both light and dark backgrounds.
// An unset base (NoColor) returns NoColor: with no background there
// is nothing to alternate, so the alternation is inert by default and
// activates only when a base background is configured.
func AltBG(base Color) Color {
	if base == NoColor {
		return NoColor
	}
	shift := func(x int32) int32 {
		if x > 128 {
			return x - AltBGDiff
		}
		return x + AltBGDiff
	}
	r, g, b := base.RGB()
	return color.NewRGBColor(shift(r), shift(g), shift(b))
}

// PlainLines converts plain text lines into Lines with alternating
// background shades, so consecutive log entries are visually distinct.
// The base shade is the tab's background; odd-numbered lines get the
// shifted alternate.
func PlainLines(lines []string, base Color) []Line {
	out := make([]Line, 0, len(lines))
	for i, line := range lines {
		bg := base
		if i%2 == 1 {
			bg = AltBG(base)
		}
		out = append(out, Line{Text: line, BGColor: bg, Color: NoColor})
	}
	return out
}

// LinesElement renders Lines as a single element: consecutive lines with
// identical colors are grouped into one Text with the group's foreground
// and background. A background color fills the whole group box, so a
// wrapped log line keeps one background across its display rows and
// consecutive groups are visually distinct.
func LinesElement(lines []Line, box Box) Element {
	if len(lines) == 0 {
		return Text("")
	}
	var children []any
	start := 0
	for i := 1; i <= len(lines); i++ {
		if i == len(lines) || lines[i].Color != lines[start].Color || lines[i].BGColor != lines[start].BGColor {
			count := i - start
			texts := make([]string, 0, count)
			for j := start; j < i; j++ {
				texts = append(texts, lines[j].Text)
			}
			groupBox := Box{Top: box.Top + start, Left: box.Left, Bottom: box.Top + i, Right: box.Right}
			specs := []any{Box(groupBox), texts}
			if lines[start].Color != 0 {
				specs = append(specs, FGColor(lines[start].Color))
			}
			if lines[start].BGColor != 0 {
				specs = append(specs, BGColor(lines[start].BGColor), Fill(true))
			}
			children = append(children, Text(specs...))
			start = i
		}
	}
	return Overlay(children...)
}
