package taiui

const TheoryOfScrollState = `
taiui scroll state theory:
- The scroll state encapsulates the view state of a scrollable pane:
  the offset, whether the view follows the tail, and the maximum offset
  computed from the wrapped display-line count and the pane height.
- Updating recomputes the maximum offset (never negative), sticks the
  offset to the latest row while following, clamps the offset, and
  resumes following when the view reaches the latest row.
- A large sentinel offset sticks the view to the tail.
- Relative, page, and absolute moves clamp against the content extent
  and update the follow state the same way: reaching the latest row
  resumes following, scrolling away stops it; only a jump to the latest
  row resumes following.
`

// ScrollState is the view state of a scrollable pane. See
// TheoryOfScrollState.
type ScrollState struct {
	Offset    int
	Follow    bool
	MaxOffset int
}

// Update recomputes the maximum offset from the wrapped display-line
// count and the pane height, sticks the offset to the latest row while
// following, clamps the offset, and resumes following when the view
// reaches the latest row.
func (s *ScrollState) Update(displayLines, paneHeight int) {
	if paneHeight < 1 {
		paneHeight = 1
	}
	maxOffset := max(displayLines-paneHeight, 0)
	s.MaxOffset = maxOffset
	if s.Follow {
		s.Offset = maxOffset
	}
	s.Offset = ClampOffset(s.Offset, displayLines, paneHeight)
	if s.Offset == maxOffset {
		s.Follow = true
	}
}

// ClampOffset clamps a pane scroll offset against the pane's wrapped
// display lines. The maximum offset is displayLines - paneHeight: at the
// maximum offset the last display line lands on the pane's last row.
// Offsets beyond the content clamp to the maximum; negative offsets clamp
// to 0. A large sentinel offset (1<<30) therefore sticks the view to the
// tail.
func ClampOffset(offset, displayLines, paneHeight int) int {
	if displayLines <= paneHeight {
		return 0
	}
	maxOffset := displayLines - paneHeight
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// Scroll moves the offset by a delta, clamped to the content extent, and
// updates the follow state: reaching the latest row resumes following,
// scrolling away stops it.
func (s *ScrollState) Scroll(delta int) {
	newTop := s.Offset + delta
	if newTop < 0 {
		newTop = 0
	}
	if newTop > s.MaxOffset {
		newTop = s.MaxOffset
	}
	s.Offset = newTop
	s.Follow = newTop >= s.MaxOffset
}

func (s *ScrollState) ScrollTo(top int) {
	// The offset is clamped to the content extent, so a large sentinel
	// offset (e.g., 1<<30, used by the "end" key) sticks the view to the
	// tail.
	if top < 0 {
		top = 0
	}
	if top > s.MaxOffset {
		top = s.MaxOffset
	}
	s.Offset = top
	s.Follow = top >= s.MaxOffset
}

// PageScroll scrolls by one page: the page size is paneHeight - 1, so
// one line of the previous view remains on screen.
func (s *ScrollState) PageScroll(direction, pageSize int) {
	s.Scroll(direction * (pageSize - 1))
}
