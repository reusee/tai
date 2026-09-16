package main

import (
	"slices"
	"strconv"
	"strings"

	"github.com/reusee/tai/taiui"
	"github.com/reusee/tai/tree"
)

const TheoryOfTreeSearch = `
Tree tab search theory (cmd/tai):
- The "/" key opens a one-row search bar above the focused tree-shaped
  pane (Tree or Plan); Esc closes it. The bar is pane state: each pane
  keeps its own keyword and match cursor. Keyword edits reuse
  taiui.InputBar; this command owns the open/close policy and navigation.
  The raw key reaches the search before mapTUIKey, so plain characters —
  "q", "s", "1" — edit the keyword instead of running the TUI's
  navigation actions, and the terminal cursor follows the focused pane's
  search bar exactly as it follows the chat input bar.
- A keyword matches a node when it is a substring of its content or any
  metadata value: Name, Type, or Author. While searching, the displayed
  tree is the match set plus every ancestor (tree.Extract), so a matched
  node's full path stays visible; an empty keyword shows the current
  projection. Search composes with the v-key projection. The flat
  stream projection shows only matched nodes in insert-time order.
- The jump unit is one matched substring, not one node: every keyword
  occurrence in metadata and on every content line is its own match in
  display order. Prev/Next/First/Last move a wrap-around cursor through
  that list; the bar shows "index/total".
- A content-line jump expands the node first and then scrolls to the
  occurrence's exact wrapped display row; header matches need no
  expansion. Expansion never changes the match set.
- An expansion the search made lasts exactly as long as the match: when
  the keyword stops matching the node, the node returns to the
  expansion state it had before the search expanded it, so continued
  typing collapses content the new keyword no longer matches while a
  manual expansion or a type default survives untouched. The same
  restore runs when the keyword is emptied and when the search closes,
  because neither state holds a match; a manual toggle and the
  collapse-all key discard the search's record of the node's earlier
  state, so the user's explicit structure is never overwritten.
- While searching, the whole panel shifts down one row and the search
  bar occupies the freed row. tabRenderBox is the one geometry source
  for the inset panel, fold controls, toolbar, status overlay, and
  pointer tests, so what is drawn is what is pressed. A press anywhere
  on the search row is consumed by the search and never reaches the tab
  machine: a button press runs its action, any other cell on the row is
  inert.
`

const (
	treeSearchActionPrev   = "search-prev"
	treeSearchActionNext   = "search-next"
	treeSearchActionFirst  = "search-first"
	treeSearchActionLast   = "search-last"
	treeSearchActionCancel = "search-cancel"
)

var treeSearchButtons = []taiui.ToolbarButton{
	{Label: "Cancel", Action: treeSearchActionCancel},
	{Label: "Last", Action: treeSearchActionLast},
	{Label: "First", Action: treeSearchActionFirst},
	{Label: "Next", Action: treeSearchActionNext},
	{Label: "Prev", Action: treeSearchActionPrev},
}

// treeMatch is one matched substring. contentLine -1 means the match is
// in header metadata; otherwise it is the zero-based content line and
// col its display column.
type treeMatch struct {
	name        string
	contentLine int
	col         int
}

func nodeMatchesSearch(n *tree.Node, keyword string) bool {
	return strings.Contains(n.Content, keyword) ||
		strings.Contains(n.Name, keyword) ||
		strings.Contains(string(n.Type), keyword) ||
		strings.Contains(string(n.Author), keyword)
}

// keywordColumns returns the display column of every non-overlapping
// keyword occurrence in text.
func keywordColumns(text, keyword string) []int {
	if keyword == "" {
		return nil
	}
	options := taiui.DisplayWidthOptions()
	var cols []int
	from := 0
	for {
		idx := strings.Index(text[from:], keyword)
		if idx < 0 {
			return cols
		}
		cols = append(cols, options.String(text[:from+idx]))
		from += idx + len(keyword)
	}
}

func appendNodeMatches(ms []treeMatch, n *tree.Node, keyword string) []treeMatch {
	for _, field := range []string{n.Name, string(n.Type), string(n.Author)} {
		for range keywordColumns(field, keyword) {
			ms = append(ms, treeMatch{name: n.Name, contentLine: -1})
		}
	}
	for line, text := range treeContentLines(n) {
		for _, col := range keywordColumns(text, keyword) {
			ms = append(ms, treeMatch{name: n.Name, contentLine: line, col: col})
		}
	}
	return ms
}

// refreshTreeSearchLocked recomputes the filtered tree and matches for
// the active pane, and returns the nodes the search auto-expanded to
// their earlier state once the keyword stops matching them. Callers
// run inside withPane and hold t.mu. See TheoryOfTreeSearch.
func (t *TUI) refreshTreeSearchLocked() {
	st := &t.treeTab
	st.searchTree = nil
	st.searchNames = nil
	st.matches = nil
	st.matchIndex = 0
	if !st.searching || t.treeView == nil {
		t.restoreSearchExpansionsLocked(nil)
		return
	}
	keyword := st.searchBar.Line()
	if keyword == "" {
		t.restoreSearchExpansionsLocked(nil)
		return
	}
	t.collectTreeSearchLocked(keyword)
	t.restoreSearchExpansionsLocked(st.searchNames)
}

// collectTreeSearchLocked builds the filtered tree and the match list
// of the current keyword. Callers run inside withPane and hold t.mu.
// See TheoryOfTreeSearch.
func (t *TUI) collectTreeSearchLocked(keyword string) {
	st := &t.treeTab
	pred := func(n *tree.Node) bool { return nodeMatchesSearch(n, keyword) }

	if st.mode == treeViewStream {
		nodes := t.treeView.Filter(func(n *tree.Node) bool { return n.Type != tree.TypeRoot })
		slices.SortStableFunc(nodes, func(a, b *tree.Node) int {
			return a.InsertTime.Compare(b.InsertTime)
		})
		st.searchNames = map[string]bool{}
		for _, n := range nodes {
			if pred(n) {
				st.searchNames[n.Name] = true
				st.matches = appendNodeMatches(st.matches, n, keyword)
			}
		}
		return
	}

	projected := t.treeView
	if st.mode != treeViewAll {
		projected = projected.Extract(st.mode.predicate())
	}
	st.searchTree = projected.Extract(pred)
	st.searchNames = map[string]bool{}
	var walk func(*tree.Node)
	walk = func(n *tree.Node) {
		if pred(n) {
			st.searchNames[n.Name] = true
			st.matches = appendNodeMatches(st.matches, n, keyword)
		}
		for _, c := range n.Children() {
			walk(c)
		}
	}
	for _, c := range st.searchTree.Root().Children() {
		walk(c)
	}
}

// restoreSearchExpansionsLocked returns the search-expanded nodes that
// the current match set no longer holds to the expansion state they
// had before the search expanded them. An expansion the search made
// exists to reveal a matched occurrence, so it lasts exactly as long
// as the match: a node the user had already expanded returns expanded,
// and a collapsed node stays collapsed. keep is the current match set;
// nil restores every tracked node, because an empty keyword and a
// closed search hold no match. The caller holds t.mu. See
// TheoryOfTreeSearch.
func (t *TUI) restoreSearchExpansionsLocked(keep map[string]bool) {
	st := &t.treeTab
	if len(st.searchExpanded) == 0 {
		return
	}
	if st.expanded == nil {
		st.expanded = make(map[string]bool)
	}
	for name, wasExpanded := range st.searchExpanded {
		if keep[name] {
			continue
		}
		st.expanded[name] = wasExpanded
		delete(st.searchExpanded, name)
	}
}

func (t *TUI) openTreeSearchLocked() {
	st := &t.treeTab
	st.searching = true
	st.searchBar.Reset()
	st.searchBar.Prompt = "/"
	t.refreshTreeSearchLocked()
}

func (t *TUI) closeTreeSearchLocked() {
	st := &t.treeTab
	st.searching = false
	st.searchBar.Reset()
	st.searchTree = nil
	st.searchNames = nil
	st.matches = nil
	st.matchIndex = 0
	// A closed search holds no match, so it restores the expansions
	// the search made. See TheoryOfTreeSearch.
	t.restoreSearchExpansionsLocked(nil)
}

func (t *TUI) paneSearching(kind tuiTab) bool {
	searching := false
	t.withPane(kind, func() { searching = t.treeTab.searching })
	return searching
}

// treeSearchPressLocked routes a left press on a searching tree pane's
// search row: a press on one of the row's buttons runs the button's
// action, and any other cell on the row is inert. Both consume the
// press, so it never reaches the tab machine; a press anywhere else
// reports false. The caller holds t.mu. See TheoryOfTreeSearch.
func (t *TUI) treeSearchPressLocked(x, y int) bool {
	for _, kind := range t.tabKinds() {
		if kind != tabTree && kind != tabPlan {
			continue
		}
		action, hit := t.treeSearchHit(kind, x, y)
		if !hit {
			continue
		}
		if action != "" {
			t.applyTreeSearchAction(kind, action)
		}
		return true
	}
	return false
}

// focusedTreeKind returns the focused expanded tree-shaped pane.
func (t *TUI) focusedTreeKind() (tuiTab, bool) {
	kind, ok := t.tabKindAt(t.tabs.Focus)
	if !ok || (kind != tabTree && kind != tabPlan) {
		return 0, false
	}
	if t.tabs.Focus < 0 || !t.tabs.Expanded[t.tabs.Focus] {
		return 0, false
	}
	return kind, true
}

// handleTreeSearchKey routes "/" and search editing to the focused
// tree pane. It reports whether the key was consumed.
func (t *TUI) handleTreeSearchKey(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	kind, ok := t.focusedTreeKind()
	if !ok {
		return false
	}
	consumed := false
	t.withPane(kind, func() {
		st := &t.treeTab
		if st.searching {
			switch key {
			case "esc":
				t.closeTreeSearchLocked()
				consumed = true
				return
			case "enter", "down":
				t.moveTreeSearchLocked(1)
				consumed = true
				return
			case "up":
				t.moveTreeSearchLocked(-1)
				consumed = true
				return
			}
			if st.searchBar.HandleKey(key) {
				t.refreshTreeSearchLocked()
				t.firstTreeSearchLocked()
				consumed = true
			}
			return
		}
		if key == "/" {
			t.openTreeSearchLocked()
			consumed = true
		}
	})
	return consumed
}

func (t *TUI) moveTreeSearchLocked(delta int) {
	st := &t.treeTab
	n := len(st.matches)
	if n == 0 {
		st.matchIndex = 0
		return
	}
	st.matchIndex = ((st.matchIndex+delta)%n + n) % n
	t.jumpTreeSearchLocked()
}

func (t *TUI) firstTreeSearchLocked() {
	st := &t.treeTab
	if len(st.matches) == 0 {
		st.matchIndex = 0
		return
	}
	st.matchIndex = 0
	t.jumpTreeSearchLocked()
}

func (t *TUI) lastTreeSearchLocked() {
	st := &t.treeTab
	if len(st.matches) == 0 {
		st.matchIndex = 0
		return
	}
	st.matchIndex = len(st.matches) - 1
	t.jumpTreeSearchLocked()
}

func (t *TUI) applyTreeSearchAction(kind tuiTab, action string) {
	t.withPane(kind, func() {
		switch action {
		case treeSearchActionCancel:
			t.closeTreeSearchLocked()
		case treeSearchActionPrev:
			t.moveTreeSearchLocked(-1)
		case treeSearchActionNext:
			t.moveTreeSearchLocked(1)
		case treeSearchActionFirst:
			t.firstTreeSearchLocked()
		case treeSearchActionLast:
			t.lastTreeSearchLocked()
		}
	})
}

func (t *TUI) searchNodeLocked(name string) (*tree.Node, bool) {
	if t.treeTab.searchTree != nil {
		return t.treeTab.searchTree.Node(name)
	}
	return t.treeView.Node(name)
}

// jumpTreeSearchLocked expands a content match's node and scrolls to
// the occurrence's exact display row.
func (t *TUI) jumpTreeSearchLocked() {
	st := &t.treeTab
	if len(st.matches) == 0 {
		return
	}
	m := st.matches[st.matchIndex]
	if m.contentLine >= 0 {
		if st.expanded == nil {
			st.expanded = map[string]bool{}
		}
		if st.searchExpanded == nil {
			st.searchExpanded = map[string]bool{}
		}
		// The first auto-expansion of a node records the state it had
		// before, so an unmatched node can return to it. See
		// TheoryOfTreeSearch.
		if _, tracked := st.searchExpanded[m.name]; !tracked {
			st.searchExpanded[m.name] = st.expanded[m.name]
		}
		st.expanded[m.name] = true
	}
	idx := st.paneIdx
	if idx < 0 || idx >= len(t.tabs.Expanded) || !t.tabs.Expanded[idx] || t.treeView == nil {
		return
	}
	box := t.tabRenderBox(idx, t.tabs.Boxes(t.width, t.height)[idx])
	if box.Width() <= 0 || box.Height() <= 0 {
		return
	}
	display := wrappedDisplay(t, idx, box)
	sc := t.paneScrollLocked(idx)
	if sc == nil || len(display) == 0 {
		return
	}
	row, ok := t.treeMatchRowLocked(m, box, display)
	if !ok {
		return
	}
	sc.Offset = taiui.ClampOffset(row, len(display), t.tuiPaneHeight(idx, box))
	sc.Follow = false
}

func (t *TUI) treeMatchRowLocked(m treeMatch, box taiui.Box, display []taiui.Line) (int, bool) {
	var rng *treeRowRange
	for i := range t.treeTab.rows {
		if t.treeTab.rows[i].name == m.name {
			rng = &t.treeTab.rows[i]
			break
		}
	}
	if rng == nil {
		return 0, false
	}
	if m.contentLine < 0 {
		return rng.startRow, true
	}
	n, ok := t.searchNodeLocked(m.name)
	if !ok {
		return rng.startRow, true
	}
	lines := treeContentLines(n)
	if m.contentLine >= len(lines) {
		return rng.startRow, true
	}
	contentWidth := treeContentWidth(box.Width())
	bodyPad := min(t.treeTab.align.contentX, max(contentWidth-1, 0))
	bodyWidth := max(contentWidth-bodyPad, 1)
	row := rng.startRow + 1
	for li := 0; li < m.contentLine; li++ {
		row += len(taiui.WrapLines([]string{lines[li]}, bodyWidth))
	}
	segments := taiui.WrapLines([]string{lines[m.contentLine]}, bodyWidth)
	options := taiui.DisplayWidthOptions()
	column := 0
	for _, seg := range segments {
		width := options.String(seg)
		if m.col >= column && m.col < column+width {
			break
		}
		row++
		column += width
	}
	if row >= rng.endRow {
		return rng.startRow, true
	}
	return row, true
}

func searchIndicator(st *treeTabState) string {
	if !st.searching || st.searchBar.Line() == "" {
		return "-"
	}
	if len(st.matches) == 0 {
		return "0/0"
	}
	return strconv.Itoa(st.matchIndex+1) + "/" + strconv.Itoa(len(st.matches))
}
