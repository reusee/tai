package taiui

const TheoryOfWrapCache = `
taiui wrap cache theory:
- A WrapCache holds the wrapped display lines of one append-only
  content source at one content width. Wrapping the whole content on
  every frame is quadratic over a streaming session, so the cache wraps
  each newly completed source line exactly once and appends the result
  to the cached display lines.
- The cache is keyed by content width, and for plain and grouped
  sources by the base background as well: a change resets the cache
  and re-wraps from scratch. Width and background changes are the
  supported invalidations; sources must be append-only, never
  rewritten in place.
- Event groups shade as a unit: every display line of one group carries
  the group's shade, and the caller's lines are copied before shading,
  so a reset re-derives shades from the source, never from shaded
  output.
- The trailing partial line of a buffer is wrapped fresh on every call
  and never cached. The cache keeps a stable display-line boundary for
  completed source lines, truncates the previous partial result in
  place, and appends the fresh one, so a streaming frame copies no
  stable display lines.
- One cache per pane and per source: the cache's entry points must not
  be mixed on a single cache value, because they share the same cached
  state.
`

// WrapCache caches the wrapped display lines of one append-only content
// source, wrapping newly completed lines once so a render pass costs
// only the new content. Its zero value is empty and ready for use.
// See TheoryOfWrapCache.
type WrapCache struct {
	width int
	base  Color
	// count is the number of completed source lines already wrapped.
	count int
	// stable is the display-line boundary of the completed source
	// lines. Lines beyond it are the transient wrapped form of a
	// buffer's trailing partial line and are discarded on the next
	// call. See TheoryOfWrapCache.
	stable int
	lines  []Line
	// buf is the per-group scratch of Groups; the buffer entry points
	// append directly to lines so a partial line never copies the
	// stable display lines.
	buf []Line
}

// Colored returns the wrapped display lines of a LineBuffer's content at
// the given content width, carrying each source line's colors onto its
// wrapped display lines. Completed lines are wrapped once and cached;
// the trailing partial line, if any, is wrapped fresh in place after the
// previous partial result is truncated. See TheoryOfWrapCache.
func (c *WrapCache) Colored(b *LineBuffer, contentWidth int) []Line {
	if c.width != contentWidth || c.base != NoColor {
		*c = WrapCache{width: contentWidth}
	}
	completed := b.CompletedLines()
	if len(completed) > c.count {
		c.lines = c.lines[:c.stable]
		c.lines = WrapLinesColoredInto(completed[c.count:], contentWidth, c.lines)
		c.count = len(completed)
		c.stable = len(c.lines)
	}
	partial := b.Partial()
	c.lines = c.lines[:c.stable]
	if partial.Text == "" {
		return c.lines
	}
	c.lines = WrapLinesColoredInto([]Line{partial}, contentWidth, c.lines)
	return c.lines
}

// Plain returns the wrapped display lines of a StringBuffer's content at
// the given content width, with alternating background shades derived
// from base: even-indexed source lines use base, odd-indexed lines use
// AltBG(base). The alternation continues across calls because the cached
// count is the source's logical line index. The trailing partial line,
// if any, is wrapped fresh in place after the previous partial result is
// truncated and carries the shade it will keep once completed. See
// TheoryOfWrapCache.
func (c *WrapCache) Plain(b *StringBuffer, contentWidth int, base Color) []Line {
	if c.width != contentWidth || c.base != base {
		*c = WrapCache{width: contentWidth, base: base}
	}
	completed := b.CompletedLines()
	if len(completed) > c.count {
		c.lines = c.lines[:c.stable]
		c.lines = WrapPlainLinesInto(completed[c.count:], base, contentWidth, c.count, c.lines)
		c.count = len(completed)
		c.stable = len(c.lines)
	}
	partial := b.Partial()
	c.lines = c.lines[:c.stable]
	if partial == "" {
		return c.lines
	}
	bg := base
	if len(completed)%2 == 1 {
		bg = AltBG(base)
	}
	c.lines = WrapLinesColoredInto([]Line{{Text: partial, BGColor: bg}}, contentWidth, c.lines)
	return c.lines
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
		c.lines = c.lines[:c.stable]
		c.lines = WrapLinesColoredInto(lines[c.count:], contentWidth, c.lines)
		c.count = len(lines)
		c.stable = len(c.lines)
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
	c.lines = c.lines[:c.stable]
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
	c.stable = len(c.lines)
	return c.lines
}
