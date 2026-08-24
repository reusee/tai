package taiui

const TheoryOfSessionChrome = `
taiui session chrome theory:
- QuitConfirm implements the two-press quit pattern: the first quit key
  press arms a pending confirmation, the second confirms, and any other
  key cancels. The armed state is rendered as a confirmation bar
  (QuitConfirmBar) drawn over the bottom row of the screen, so an
  accidental quit key press never loses the session.
- HelpOverlay renders a centered, bordered key-binding list over the
  content; TabWidth aligns each line's key and description columns.
  Both overlays are part of the element tree, derived from state, so no
  imperative layer management is needed.
`

// QuitConfirm implements the two-press quit pattern: the first quit key
// press arms a pending confirmation, the second press confirms, and any
// other key cancels it. See TheoryOfSessionChrome.
type QuitConfirm struct {
	armed bool
}

// QuitKeyPressed processes one quit key press: the first press arms the
// confirmation and reports false, the second confirms and reports true.
func (q *QuitConfirm) QuitKeyPressed() bool {
	if q.armed {
		return true
	}
	q.armed = true
	return false
}

// Cancel disarms a pending quit confirmation. Applications call it for
// every non-quit key, so an accidental quit press is undone by the next
// key. See TheoryOfSessionChrome.
func (q *QuitConfirm) Cancel() {
	q.armed = false
}

// Pending reports whether a quit confirmation is armed.
func (q *QuitConfirm) Pending() bool {
	return q.armed
}

// QuitConfirmBar returns the confirmation bar drawn over the bottom row
// of a width×height screen while a quit confirmation is pending. See
// TheoryOfSessionChrome.
func QuitConfirmBar(width, height int) Element {
	return Rect(
		Box{Top: height - 1, Left: 0, Bottom: height, Right: width},
		Fill(true),
		BGColor(HexColor(0x800000)),
		Bold(true),
		Text(" Quit? Press q again to confirm, any other key to cancel "),
	)
}

// HelpOverlay returns a centered, bordered overlay listing the given
// help lines. tabWidth aligns the key column of each line. See
// TheoryOfSessionChrome.
func HelpOverlay(lines []string, tabWidth, width, height int) Element {
	overlayHeight := min(len(lines)+4, max(height-2, 1))
	overlayWidth := min(72, max(width-4, 1))
	top := max((height-overlayHeight)/2, 0)
	left := max((width-overlayWidth)/2, 0)
	return Rect(
		Box{
			Top:    top,
			Left:   left,
			Bottom: top + overlayHeight,
			Right:  left + overlayWidth,
		},
		Border(true),
		Fill(true),
		BGColor(HexColor(0x202020)),
		Title(" Help "),
		Padding(1),
		Text(lines, TabWidth(tabWidth)),
	)
}
