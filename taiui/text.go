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

// _Text is an aligned multi-line text block with optional word wrapping.
// It is a pure value: specs are interpreted at construction into typed
// fields, and rendering reads those fields.
type _Text struct {
	elementBase
	lines           []string
	align           Align
	padding         [4]int
	offsetStyleFunc OffsetStyleFunc
	wrap            bool
}

// Text creates a text element from specs. Bare strings and []string values
// are accepted as shorthands for lines; all other specs are interpreted as
// element specs. Wrap enables word wrapping to the box width. Unknown specs
// panic here, at construction.
func Text(specs ...any) _Text {
	t := &_Text{}
	buildElement(t, specs)
	return *t
}

func (_Text) element() {}

func (Wrap) spec() {}

// applySpec interprets one spec value into _Text fields.
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
	case _Padding:
		t.padding = applyBoxModel(v)
	case OffsetStyleFunc:
		t.offsetStyleFunc = v
	case Wrap:
		t.wrap = bool(v)
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

	contentLeft := box.Left + t.padding[3]
	wrapWidth := box.Width() - t.padding[1] - t.padding[3]
	right := box.Right - t.padding[1]
	maxY := box.Bottom - t.padding[2]
	y := box.Top + t.padding[0]
	for _, line := range t.lines {
		lines := []string{line}
		if t.wrap {
			lines = wrapLine(line, wrapWidth, options)
		}
		for _, ln := range lines {
			if y >= maxY {
				// The box is full; remaining lines are never processed.
				return
			}
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

	// Split the line into words at space clusters.
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
			if cluster == " " {
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
