package taiui

const TheoryOfTabLayout = `
taiui tab layout theory:
- TabLayout computes the pane boxes of a multi-pane tabbed layout. It is
  extracted from the TUI layer of the tai command, where the same weighted
  algorithm decided how much terminal space each pane received.
- Two split directions are supported: side by side (SplitVertical, one
  vertical divider between panes) and stacked (SplitHorizontal, one
  horizontal divider). The direction names describe the divider, following
  the cmd/tai convention.
- Expanded panes share the remaining space proportionally to their weights:
  2 for the focused pane and 1 for every other expanded pane. The last
  expanded pane absorbs the rounding remainder.
- Collapsed panes take one column (SplitVertical) or one row
  (SplitHorizontal) each and stay in their index order, so a collapsed
  pane between expanded ones keeps its original position.
`

// TabLayout computes the pane boxes of a multi-pane tabbed layout.
// It is independent of TabState: the caller passes the expanded flags
// and the focused index, which typically come from a TabState.
// See TheoryOfTabLayout.
type TabLayout struct {
	// Expanded reports which panes are expanded. Panes not listed are
	// collapsed strips.
	Expanded []bool
	// Focus is the index of the focused pane; -1 means no focus. The
	// focused pane receives twice the weight of every other expanded pane.
	Focus int
	// VerticalSplit selects side-by-side panes with one column strips
	// (true) or stacked panes with one row strips (false).
	VerticalSplit bool
}

// Boxes computes the pane boxes for a window of the given dimensions,
// in index order. The number of boxes equals len(Expanded).
func (l TabLayout) Boxes(width, height int) []Box {
	num := len(l.Expanded)
	boxes := make([]Box, num)

	// Count expanded panes and the total weight.
	expandedCount := 0
	totalWeight := 0
	for i, expanded := range l.Expanded {
		if expanded {
			expandedCount++
			if i == l.Focus {
				totalWeight += 2
			} else {
				totalWeight++
			}
		}
	}
	collapsedCount := num - expandedCount
	if totalWeight <= 0 {
		totalWeight = 1
	}

	if l.VerticalSplit {
		// Collapsed panes take one column each; expanded panes share the
		// remaining width. See TheoryOfTabLayout.
		expandedWidth := max(0, width-collapsedCount)
		edge := 0
		expandedEdge := 0
		expandedPos := 0
		for i, expanded := range l.Expanded {
			if expanded {
				weight := 1
				if i == l.Focus {
					weight = 2
				}
				var size int
				if expandedPos == expandedCount-1 {
					// The last expanded pane absorbs the rounding
					// remainder. See TheoryOfTabLayout.
					size = expandedWidth - expandedEdge
				} else {
					size = expandedWidth * weight / totalWeight
				}
				boxes[i] = Box{Top: 0, Left: edge, Bottom: height, Right: edge + size}
				edge += size
				expandedEdge += size
				expandedPos++
			} else {
				// A collapsed pane stays in place with one column.
				boxes[i] = Box{Top: 0, Left: edge, Bottom: height, Right: edge + 1}
				edge++
			}
		}
		return boxes
	}

	// Stacked panes: collapsed panes take one row each; expanded panes
	// share the remaining height. See TheoryOfTabLayout.
	expandedHeight := max(0, height-collapsedCount)
	edge := 0
	expandedEdge := 0
	expandedPos := 0
	for i, expanded := range l.Expanded {
		if expanded {
			weight := 1
			if i == l.Focus {
				weight = 2
			}
			var size int
			if expandedPos == expandedCount-1 {
				size = expandedHeight - expandedEdge
			} else {
				size = expandedHeight * weight / totalWeight
			}
			boxes[i] = Box{Top: edge, Left: 0, Bottom: edge + size, Right: width}
			edge += size
			expandedEdge += size
			expandedPos++
		} else {
			boxes[i] = Box{Top: edge, Left: 0, Bottom: edge + 1, Right: width}
			edge++
		}
	}
	return boxes
}
