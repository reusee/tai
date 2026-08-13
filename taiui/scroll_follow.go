package taiui

const TheoryOfScrollFollow = `
taiui scroll follow theory:
- ScrollFollow is the scroll-and-follow state of a pane whose content
  arrives incrementally. It is extracted from cmd/tai, where each pane
  maintained its own offset, follow flag, and clamping.
- The offset is the desired first visible row; the 1<<30 sentinel marks the
  end, and Handle clamps every offset — sentinel included — to the valid
  range.
- While follow is on, the offset sticks to the newest row as content grows
  or the window resizes. Manual scrolling and jumps (ScrollTo) clear
  follow; scrolling or jumping down to the tail re-enables it.
- The offset is bounded to [0, contentHeight-windowHeight]: at the maximum
  offset the last display line lands on the pane's last row. Offsets beyond
  the content clamp to the maximum; short content clamps to 0. Re-enabling
  follow (e.g. re-expanding a collapsed pane) resumes the tail.
- ScrollFollow is the state holder only; the wrapped display-line count and
  the window height are supplied per handle call, so the caller owns word
  wrapping and box geometry.
`

// ScrollFollow is the scroll-and-follow state of an incrementally fed pane.
// See TheoryOfScrollFollow.
type ScrollFollow struct {
	// Offset is the desired first visible line, as a row index in the
	// wrapped display lines. A large sentinel value (1<<30) means the view
	// is initially at the tail; Handle clamps it to the valid range.
	Offset int
	// Follow reports whether the view sticks to the newest row.
	Follow bool
	// windowHeight is the pane scroll window height of the most recent
	// handle; page scrolling uses it. See TheoryOfScrollFollow.
	windowHeight int
}

// Handle adjusts the state for the given wrapped display-line count and
// window height, clamping the offset to the valid range, sticking the view
// to the tail when follow is on, and re-enabling follow when the offset
// reaches the tail. It returns the effective offset.
func (s *ScrollFollow) Handle(contentHeight, windowHeight int) int {
	s.windowHeight = max(0, windowHeight)
	contentHeight = max(0, contentHeight)
	maxOffset := max(0, contentHeight-s.windowHeight)
	if s.Offset > maxOffset {
		s.Offset = maxOffset
	}
	if s.Offset < 0 {
		s.Offset = 0
	}
	if s.Follow {
		s.Offset = maxOffset
	}
	if s.Offset == maxOffset {
		s.Follow = true
	}
	return s.Offset
}

// Scroll moves the offset by delta rows, clamping to the valid range and
// clearing follow when scrolling away from the tail. See
// TheoryOfScrollFollow.
func (s *ScrollFollow) Scroll(delta int) {
	newOffset := s.Offset + delta
	if newOffset < 0 {
		newOffset = 0
	}
	s.Offset = newOffset
	// Reaching the tail re-enables follow only when the handle validates
	// it; scrolling away clears it. A temporary offset above the maximum
	// is clamped by the next handle, where reaching the tail re-enables
	// follow. The follow flag here is cleared conservatively: the next
	// handle recomputes it. See TheoryOfScrollFollow.
	s.Follow = false
}

// Page scrolls by one page: the window height minus one row, so one line
// of the previous view remains visible. The page size is derived from the
// most recent handle's window height. See TheoryOfScrollFollow.
func (s *ScrollFollow) Page(direction int) {
	s.Scroll(direction * max(0, s.windowHeight-1))
}

// ResumeFollow re-enables follow and immediately sticks the offset to the
// tail under the given content dimensions, matching the behavior of
// re-expanding a collapsed pane. See TheoryOfScrollFollow.
func (s *ScrollFollow) ResumeFollow(contentHeight, windowHeight int) int {
	s.Follow = true
	return s.Handle(contentHeight, windowHeight)
}

// ScrollTo jumps to the given desired first visible row: 0 for the start,
// the 1<<30 sentinel for the end. The jump clears follow; the next Handle
// clamps the offset and re-enables follow when the effective offset lands
// on the tail, so an end jump resumes following and any other jump stops
// following. See TheoryOfScrollFollow.
func (s *ScrollFollow) ScrollTo(offset int) {
	s.Offset = offset
	s.Follow = false
}
