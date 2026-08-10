package taiui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
)

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
}

// Text creates a text element from specs. Bare strings and []string values
// are accepted as shorthands for lines; all other specs are interpreted as
// element specs. Wrap enables word wrapping to the box width; VAlign
// selects the vertical alignment. Unknown specs panic here, at
// construction.
func Text(specs ...any) _Text {
	t := &_Text{tabWidth: 8}
	buildElement(t, specs)
	return *t
}

func (_Text) element() {}

func (Wrap) spec() {}

func (TabWidth) spec() {}

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

func renderText(t _Text, box Box, style Style, draw drawFunc, options displaywidth.Options) {
	box = t.effectiveBox(box)
	style = t.styled(style)

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
	var lines []string
	for _, line := range t.lines {
		wrapped := []string{line}
		if t.wrap {
			wrapped = wrapLine(line, wrapWidth, options)
		}
		for _, ln := range wrapped {
			if len(lines) >= maxLines {
				break
			}
			lines = append(lines, ln)
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

	for _, ln := range lines {
		left := contentLeft
		switch t.align {
		case AlignRight:
			left = right - options.String(ln)
		case AlignCenter:
			// Centering is relative to the padded content area and
			// rounds with the conventional (width-len)/2 rule, so an
			// odd-width line places the extra column on the right.
			left = (contentLeft + right - options.String(ln)) / 2
		}
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
			for left < right {
				draw(left, y, ' ', nil, style)
				left++
			}
		}
		y++
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

// word is a whitespace-free run of grapheme clusters, with the display
// width of each cluster.
type word struct {
	clusters []string
	widths   []int
	width    int
}

// wrapLine wraps line to at most width display columns, breaking at space
// runs (which act as separators and are dropped) and hard-breaking words
// wider than the box at cluster boundaries. A cluster never splits across
// lines, so a cluster wider than width occupies its own line.
func wrapLine(line string, width int, options displaywidth.Options) []string {
	if width <= 0 {
		return nil
	}
	if line == "" {
		return []string{""}
	}

	// Split the line into words at whitespace clusters.
	var words []word
	{
		g := options.StringGraphemes(line)
		var clusters []string
		var widths []int
		wordWidth := 0
		flushWord := func() {
			if len(clusters) > 0 {
				words = append(words, word{
					clusters: clusters,
					widths:   widths,
					width:    wordWidth,
				})
				clusters = nil
				widths = nil
				wordWidth = 0
			}
		}
		for g.Next() {
			cluster := g.Value()
			if cluster == " " || cluster == "\t" {
				flushWord()
				continue
			}
			w := g.Width()
			clusters = append(clusters, cluster)
			widths = append(widths, w)
			wordWidth += w
		}
		flushWord()
	}

	var lines []string
	cur := []string(nil)
	curWidth := 0
	for _, w := range words {
		if w.width > width {
			if len(cur) > 0 {
				lines = append(lines, strings.Join(cur, ""))
				cur = nil
				curWidth = 0
			}
			lines = append(lines, breakWord(w, width)...)
			continue
		}
		if len(cur) > 0 && curWidth+1+w.width > width {
			lines = append(lines, strings.Join(cur, ""))
			cur = nil
			curWidth = 0
		}
		if len(cur) > 0 {
			cur = append(cur, " ")
			curWidth++
		}
		cur = append(cur, w.clusters...)
		curWidth += w.width
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

// breakWord chunks the clusters of a word into lines no wider than width.
// A cluster wider than width occupies its own line.
func breakWord(w word, width int) []string {
	var lines []string
	cur := []string(nil)
	curWidth := 0
	for i, cluster := range w.clusters {
		clusterWidth := w.widths[i]
		if curWidth+clusterWidth > width && len(cur) > 0 {
			lines = append(lines, strings.Join(cur, ""))
			cur = nil
			curWidth = 0
		}
		cur = append(cur, cluster)
		curWidth += clusterWidth
	}
	if len(cur) > 0 {
		lines = append(lines, strings.Join(cur, ""))
	}
	return lines
}
