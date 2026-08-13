package taiui

const TheoryOfTabs = `
taiui tab state theory:
- TabState is the focus and expansion state machine of a multi-pane tabbed
  interface. It is extracted from cmd/tai's TUI, generalizing the logic of
  toggleTab, cycleFocus, focusLastExpanded and autoExpand for any tab count.
- Each tab tracks whether it is expanded and whether content has arrived.
- The first content arrival expands a collapsed tab without stealing focus:
  only when no tab is focused does the first auto-expanded tab become the
  focus. Subsequent arrivals never re-expand a tab the user collapsed.
- Toggling a focused tab collapses it and moves focus to the most recently
  focused expanded tab; toggling a non-focused tab expands it and takes
  focus. Cycling focus skips collapsed tabs and wraps. Ties among tabs that
  were never focused break by index order.
`

// TabState is the focus and expansion state machine of a multi-pane tabbed
// interface. Collapsed tabs are thin strips handled by TabLayout; this type
// owns only the structure states. See TheoryOfTabs.
type TabState struct {
	// Count is the number of tabs.
	Count int
	// Expanded reports whether each tab is expanded. Tabs not listed are
	// collapsed.
	Expanded []bool
	// HasContent reports whether each tab has ever received content; the
	// first content arrival auto-expands a collapsed tab once.
	HasContent []bool
	// Focus is the index of the focused tab, -1 when no tab is expanded.
	Focus int

	// lastFocus is the focus recency order of each tab; a higher value
	// means the tab was focused more recently. It decides where focus
	// returns after the focused tab collapses. See TheoryOfTabs.
	lastFocus []int
	// focusOrder is the counter assigning lastFocus values.
	focusOrder int
	// initialized reports whether lastFocus is sized; zero TabState is
	// usable for toggling before any content arrives.
	initialized bool
}

// NewTabState creates a tab state for the given number of tabs, all
// collapsed and unfocused.
func NewTabState(num int) TabState {
	return TabState{
		Count:       num,
		Expanded:    make([]bool, num),
		HasContent:  make([]bool, num),
		Focus:       -1,
		lastFocus:   make([]int, num),
		focusOrder:  0,
		initialized: true,
	}
}

// AutoExpand expands a tab the first time content arrives for it, never
// changing an established focus. Subsequent arrivals do not re-expand a
// tab the user collapsed. See TheoryOfTabs.
func (s *TabState) AutoExpand(idx int) {
	if idx < 0 || idx >= s.Count {
		return
	}
	if !s.initialized {
		s.lastFocus = make([]int, s.Count)
		s.initialized = true
	}
	// Only the first content arrival auto-expands; subsequent content does
	// not re-expand a tab the user collapsed. See TheoryOfTabs.
	if s.HasContent[idx] {
		return
	}
	s.HasContent[idx] = true
	if s.Expanded[idx] {
		return
	}
	s.Expanded[idx] = true
	// Auto-expansion never changes an existing focus: only when no tab is
	// focused does the first auto-expanded tab become the focus, so keyboard
	// navigation remains usable. See TheoryOfTabs.
	if s.Focus == -1 {
		s.Focus = idx
		s.lastFocus[idx] = s.focusOrder
		s.focusOrder++
	}
}

// Toggle collapses the focused tab and moves focus to the most recently
// focused expanded tab; for any other tab it expands it (if collapsed)
// and takes focus. See TheoryOfTabs.
func (s *TabState) Toggle(idx int) {
	if idx < 0 || idx >= s.Count {
		return
	}
	if !s.initialized {
		s.lastFocus = make([]int, s.Count)
		s.initialized = true
	}
	if s.Focus == idx {
		// A focused tab's toggle collapses it and moves the focus to the
		// expanded tab that was last focused. See TheoryOfTabs.
		s.Expanded[idx] = false
		s.focusLastExpanded()
		return
	}
	if !s.Expanded[idx] {
		s.Expanded[idx] = true
	}
	s.Focus = idx
	s.lastFocus[idx] = s.focusOrder
	s.focusOrder++
}

// Cycle advances the focus to the next expanded tab, wrapping around and
// skipping collapsed tabs. When no tab is expanded the focus clears.
func (s *TabState) Cycle() {
	if !s.initialized {
		s.lastFocus = make([]int, s.Count)
		s.initialized = true
	}
	if s.Focus >= 0 {
		for i := 1; i <= s.Count; i++ {
			f := (s.Focus + i) % s.Count
			if f >= 0 && f < s.Count && s.Expanded[f] {
				s.Focus = f
				s.lastFocus[f] = s.focusOrder
				s.focusOrder++
				return
			}
		}
	}
	for i := 0; i < s.Count; i++ {
		if s.Expanded[i] {
			s.Focus = i
			s.lastFocus[i] = s.focusOrder
			s.focusOrder++
			return
		}
	}
	s.Focus = -1
}

// focusLastExpanded moves the focus to the expanded tab that was last
// focused. Tabs that were never focused tie-break by index order.
// See TheoryOfTabs.
func (s *TabState) focusLastExpanded() {
	best := -1
	bestOrder := -2
	for i := 0; i < s.Count; i++ {
		if !s.Expanded[i] {
			continue
		}
		if s.lastFocus[i] > bestOrder {
			bestOrder = s.lastFocus[i]
			best = i
		}
	}
	s.Focus = best
}
