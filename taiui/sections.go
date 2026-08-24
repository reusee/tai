package taiui

const TheoryOfSectionNavigation = `
taiui section navigation theory:
- TransitionBoundaries locates the display lines where one section ends
  and the next begins: a display line whose color differs from the
  previous line's color. WrapLinesColored carries a source line's color
  onto every wrapped display line, so transitions are identified in
  display coordinates, the same coordinate space as the scroll offsets.
  The first display line is never a boundary: there is no previous
  section to transition from.
- TransitionJumpStops expands each boundary into two scroll offsets: the
  exit stop anchors the previous section's last line at the bottom of
  the pane (boundary - paneHeight, clamped to the content start), and
  the entry stop anchors the new section's first line at the top of the
  pane (the boundary itself). Both sides of every change are reachable,
  not only section starts. The stops stay in transition order, not
  sorted by offset — sections shorter than the pane interleave the two —
  so the selection compares offsets, not list positions.
- JumpStopOffset selects the stop nearest to the current offset in the
  given direction. Backward navigation falls back to the very beginning
  of the content, so the start of the first section — a display line
  that is never itself a transition — is always reachable.
`

// TransitionBoundaries returns the display-line indices where the
// sections change: a display line whose color differs from the previous
// line's color. The first display line is never a boundary. See
// TheoryOfSectionNavigation.
func TransitionBoundaries(display []Line) []int {
	var indices []int
	for i := 1; i < len(display); i++ {
		if display[i].Color != display[i-1].Color {
			indices = append(indices, i)
		}
	}
	return indices
}

// TransitionJumpStops returns the scroll offsets section navigation
// walks through, one pair per transition: the exit stop
// (boundary - paneHeight, clamped to the content start) and the entry
// stop (the boundary itself), in transition order. See
// TheoryOfSectionNavigation.
func TransitionJumpStops(display []Line, paneHeight int) []int {
	boundaries := TransitionBoundaries(display)
	stops := make([]int, 0, len(boundaries)*2)
	for _, b := range boundaries {
		stops = append(stops, max(b-paneHeight, 0), b)
	}
	return stops
}

// JumpStopOffset selects from stops the offset nearest to from in the
// given direction: a negative direction returns the largest stop below
// from, falling back to 0 so the beginning of the content is always
// reachable; a positive direction returns the smallest stop above from.
// It reports false when no stop exists in the positive direction. See
// TheoryOfSectionNavigation.
func JumpStopOffset(stops []int, from, direction int) (int, bool) {
	target := -1
	if direction < 0 {
		for _, s := range stops {
			if s < from && (target < 0 || s > target) {
				target = s
			}
		}
		if target < 0 {
			return 0, true
		}
		return target, true
	}
	for _, s := range stops {
		if s > from && (target < 0 || s < target) {
			target = s
		}
	}
	if target < 0 {
		return 0, false
	}
	return target, true
}
