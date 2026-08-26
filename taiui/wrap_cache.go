package taiui

const TheoryOfWrapCache = `
taiui wrap cache theory:
- A WrapCache holds the wrapped display lines of one append-only content
  source (a LineBuffer, a growing []Line, a growing [][]Line of event
  groups, or a StringBuffer) at one content width. Wrapping the whole
  content on every frame is O(total) per frame — quadratic over a
  streaming session — so the cache wraps each newly completed source
  line exactly once and appends the result to the cached display lines.
- The cache is keyed by content width, and for plain buffers and event
  groups by the base background as well: a change resets the cache and
  re-wraps from scratch. Width and background changes are the supported
  invalidations; sources must be append-only, never rewritten in place.
- Event groups shade as a unit: every display line of one group carries
  the group's shade (base for even group indices, AltBG(base) for odd),
  and the caller's lines are copied before shading, so a reset
  re-derives shades from the source, never from shaded output.
- The trailing partial line of a buffer is wrapped fresh on every call
  into a scratch slice and never cached, so completing it cannot pollute
  the cache. The scratch slice is reused across calls.
- One cache per pane and per source: the Colored, Lines, Groups, and
  Plain methods must not be mixed on a single cache value, because each
  method keys and advances the same counters.
`

// WrapCache caches the wrapped display lines of one append-only content
// source, wrapping newly completed lines once so a render pass costs
// only the new content. Its zero value is empty and ready for use.
// See TheoryOfWrapCache.
type WrapCache struct {
	width int
	base  Color
	count int
	lines []Line
	buf   []Line
}

// Colored returns the wrapped display lines of a LineBuffer's content at
// the given content width, carrying each source line's colors onto its
// wrapped display lines. Completed lines are wrapped once and cached;
// the trailing partial line, if any, is wrapped fresh into the cache's
// scratch slice on every call. See TheoryOfWrapCache.
func (c *WrapCache) Colored(b *LineBuffer, contentWidth int) []Line {
	if c.width != contentWidth || c.base != NoColor {
		*c = WrapCache{width: contentWidth}
	}
	completed := b.CompletedLines()
	if len(completed) > c.count {
		c.lines = WrapLinesColoredInto(completed[c.count:], contentWidth, c.lines)
		c.count = len(completed)
	}
	partial := b.Partial()
	if partial.Text == "" {
		return c.lines
	}
	c.buf = append(c.buf[:0], c.lines...)
	c.buf = WrapLinesColoredInto([]Line{partial}, contentWidth, c.buf)
	return c.buf
}

// Plain returns the wrapped display lines of a StringBuffer's content at
// the given content width, with alternating background shades derived
// from base: even-indexed source lines use base, odd-indexed lines use
// AltBG(base). The alternation continues across calls because the cached
// count is the source's logical line index. The trailing partial line,
// if any, is wrapped fresh and carries the shade it will keep once
// completed. See TheoryOfWrapCache.
func (c *WrapCache) Plain(b *StringBuffer, contentWidth int, base Color) []Line {
	if c.width != contentWidth || c.base != base {
		*c = WrapCache{width: contentWidth, base: base}
	}
	completed := b.CompletedLines()
	if len(completed) > c.count {
		c.lines = WrapPlainLinesInto(completed[c.count:], base, contentWidth, c.count, c.lines)
		c.count = len(completed)
	}
	partial := b.Partial()
	if partial == "" {
		return c.lines
	}
	bg := base
	if len(completed)%2 == 1 {
		bg = AltBG(base)
	}
	c.buf = append(c.buf[:0], c.lines...)
	c.buf = WrapLinesColoredInto([]Line{{Text: partial, BGColor: bg}}, contentWidth, c.buf)
	return c.buf
}

// Lines returns the wrapped display lines of a growing []Line at the
// given content width, wrapping only the lines appended since the
// previous call. Unlike the buffer methods there is no partial line:
// the slice's last line is complete by construction. See
// TheoryOfWrapCache.
func (c *WrapCache) Lines(lines []Line, contentWidth int) []Line {
	if c.width != contentWidth || c.base != NoColor {
		*c = WrapCache{width: contentWidth}
	}
	if len(lines) > c.count {
		c.lines = WrapLinesColoredInto(lines[c.count:], contentWidth, c.lines)
		c.count = len(lines)
	}
	return c.lines
}

// Groups returns the wrapped display lines of growing event groups at
// the given content width, with alternating background shades derived
// from base: even-indexed groups use base, odd-indexed groups use
// AltBG(base), and every display line of a group — including wrapped
// continuation lines — carries the group's shade. The alternation
// continues across calls because the cached count is the group index.
// The caller's group lines are never mutated: each newly wrapped group
// is copied into scratch with its shade applied, so a cache reset
// re-derives every shade from the source. See TheoryOfWrapCache.
func (c *WrapCache) Groups(groups [][]Line, contentWidth int, base Color) []Line {
	if c.width != contentWidth || c.base != base {
		*c = WrapCache{width: contentWidth, base: base}
	}
	for ; c.count < len(groups); c.count++ {
		bg := base
		if c.count%2 == 1 {
			bg = AltBG(base)
		}
		c.buf = c.buf[:0]
		for _, line := range groups[c.count] {
			c.buf = append(c.buf, Line{Text: line.Text, Color: line.Color, BGColor: bg})
		}
		c.lines = WrapLinesColoredInto(c.buf, contentWidth, c.lines)
	}
	return c.lines
}
