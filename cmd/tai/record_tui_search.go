package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/reusee/tai/records"
	"github.com/reusee/tai/taiui"
)

// recordPressPair records the previous left press at one cell, so a second
// press at the same cell within treeDoubleClickWindow is one double click.
// The record list and the match list each carry one pair.
// See TheoryOfRecordBrowser.
type recordPressPair struct {
	at   time.Time
	x, y int
}

// hit records one press and reports whether it completed a double click at
// the same cell. The pair resets after a hit, so a third press starts a new
// cycle. See TheoryOfRecordBrowser.
func (p *recordPressPair) hit(x, y int, now time.Time) bool {
	if now.Sub(p.at) <= treeDoubleClickWindow && x == p.x && y == p.y {
		p.at = time.Time{}
		return true
	}
	p.at = now
	p.x, p.y = x, y
	return false
}

// listRowIndex maps a press onto the row of a List of the given count and
// selection, through the same window the List renders with: the view
// centers on the selection and clamps to the content extent
// (taiui.ListWindow). ok is false for a press outside the list's box or past
// its entries. See TheoryOfRecordBrowser.
func listRowIndex(box taiui.Box, count, selected, x, y int) (int, bool) {
	if count == 0 {
		return 0, false
	}
	if x < box.Left || x >= box.Right || y < box.Top || y >= box.Bottom {
		return 0, false
	}
	from := taiui.ListWindow(count, box.Height(), selected)
	idx := from + (y - box.Top)
	if idx < 0 || idx >= count {
		return 0, false
	}
	return idx, true
}

// recordMatchListItem renders one global-search row: the session that wrote
// the node, the node's type, author, and name, and the matched line as the
// preview. The List truncates the row at the pane width, so the preview
// needs no width of its own. See TheoryOfRecordBrowser and
// records.TheoryOfGlobalRecordSearch.
func recordMatchListItem(m records.RecordSearchMatch) string {
	return fmt.Sprintf("#%-4d %-17s %-8s %-22s %s",
		m.SessionID, m.NodeType, m.NodeAuthor, m.NodeName, m.Preview)
}

// openSearch opens the keyword bar over the current view, with the previous
// keyword still in place for editing. See TheoryOfRecordBrowser.
func (b *RecordBrowser) openSearch() {
	b.searching = true
	b.searchBar.Prompt = "/"
}

// handleSearchKey handles one key while the keyword bar is open: Enter runs
// the search, Esc and Ctrl-C close the bar, and every other key edits the
// keyword. The bar holds the keyboard, so a plain character such as "q"
// edits the keyword instead of running a navigation action.
// See TheoryOfRecordBrowser.
func (b *RecordBrowser) handleSearchKey(key string) bool {
	switch key {
	case "enter":
		b.runSearch()
	case "esc", "ctrl-c":
		b.searching = false
	}
	if b.searching {
		b.searchBar.HandleKey(key)
	}
	return false
}

// runSearch searches every recorded session for the typed keyword and shows
// the match list. An empty keyword keeps the bar open: there is nothing to
// search for. A failed search reports the reason and keeps the current view.
// See TheoryOfRecordBrowser and records.TheoryOfGlobalRecordSearch.
func (b *RecordBrowser) runSearch() {
	keyword := strings.TrimSpace(b.searchBar.Line())
	if keyword == "" {
		return
	}
	b.searching = false
	if b.searchNodes == nil {
		b.err = "record search unavailable"
		return
	}
	matches, err := b.searchNodes(keyword)
	if err != nil {
		b.err = err.Error()
		return
	}
	b.err = ""
	if b.currentID != 0 {
		b.backToList()
	}
	b.searchKeyword = keyword
	b.matches = matches
	b.matchSelected = 0
	b.showingMatches = true
}

// matchesView renders the global match list: the panel with the tab's title
// row plus a List over the matches, whose window centers on the selection
// like the record list's. See TheoryOfRecordBrowser.
func (b *RecordBrowser) matchesView(box taiui.Box) taiui.Element {
	if len(b.matches) == 0 {
		text := "no nodes match the keyword"
		if b.searchKeyword != "" {
			text = fmt.Sprintf("no nodes match %q", b.searchKeyword)
		}
		return taiui.TabPanel(box, recordTabTitle, recordTabTitle, true, true, false,
			[]taiui.Line{{Text: text}}, taiui.ScrollState{}, panelStyle)
	}
	items := make([]string, 0, len(b.matches))
	for _, m := range b.matches {
		items = append(items, recordMatchListItem(m))
	}
	panel := taiui.TabPanel(box, recordTabTitle, recordTabTitle, true, true, false, nil, taiui.ScrollState{}, panelStyle)
	list := taiui.List(items, b.matchSelected,
		b.listBox(),
		taiui.ListStyle(taiui.SameStyle.SetReverse(true)),
	)
	return taiui.Overlay(panel, list)
}

// searchElement renders the keyword bar on the screen's bottom row. The
// terminal cursor shows at the editing position while the bar is open;
// syncSearchCursor hides it again when the bar closes.
// See TheoryOfRecordBrowser.
func (b *RecordBrowser) searchElement(width, height int) taiui.Element {
	row := max(height-1, 0)
	return b.searchBar.Element(
		taiui.Box{Top: row, Left: 0, Bottom: row + 1, Right: max(width, 1)},
		true, true, inputBarStyle,
	)
}

// syncSearchCursor shows the terminal cursor while the keyword bar is open
// and hides it when the bar closes, written after the frame is presented so
// the sequence stays serial with the screen's output: the bar renders an
// Input element whose cursor position is recorded in the frame.
// See TheoryOfRecordBrowser.
func (b *RecordBrowser) syncSearchCursor() {
	if b.tty == nil || b.searchCursorShown == b.searching {
		return
	}
	b.searchCursorShown = b.searching
	if b.searching {
		fmt.Fprint(b.tty, taiui.CursorRestoreSequence)
		return
	}
	fmt.Fprint(b.tty, taiui.CursorHideSequence)
}

// matchesPress handles a left press in the match list: the press selects the
// match under it, and a second press at the same cell within the
// double-click window opens the match's record at the node.
// See TheoryOfRecordBrowser.
func (b *RecordBrowser) matchesPress(x, y int) {
	idx, ok := listRowIndex(b.listBox(), len(b.matches), b.matchSelected, x, y)
	if !ok {
		return
	}
	if b.matchPress.hit(x, y, time.Now()) {
		b.openMatch(b.matches[idx])
		return
	}
	b.matchSelected = idx
}

// selectMatch moves the match selection to idx, clamped to the list.
func (b *RecordBrowser) selectMatch(idx int) {
	if len(b.matches) == 0 {
		b.matchSelected = 0
		return
	}
	b.matchSelected = min(max(idx, 0), len(b.matches)-1)
}

// openMatch opens the match's session when it is not the current one and
// jumps the tree to the matched node, so the record list, the match list,
// and the tree are one navigation: the match list is where a global search
// lands and the tree is where the node reads. A load failure keeps the
// current view and reports the reason. See TheoryOfRecordBrowser.
func (b *RecordBrowser) openMatch(m records.RecordSearchMatch) {
	if b.currentID != m.SessionID {
		b.openRecord(m.SessionID)
		if b.currentID != m.SessionID {
			return
		}
	}
	b.jumpToNode(m)
}

// jumpToNode opens the tree on the matched node and scrolls its first row to
// the pane top. The match may live outside the view on screen — the v-key
// projection, a folded subtree, a collapsed body — so the jump shows the
// structure the node lives in: the whole projection, every subtree fold on
// the node's path cleared, and the node expanded when the keyword was found
// in its content. The tree's own expansion invariant supplies the last step
// (see TheoryOfTreeTab). See TheoryOfRecordBrowser.
func (b *RecordBrowser) jumpToNode(m records.RecordSearchMatch) {
	p := b.treePane
	if p == nil || p.treeView == nil {
		return
	}
	b.syncTreePane()
	p.mu.Lock()
	defer p.mu.Unlock()
	node, ok := p.treeView.Node(m.NodeName)
	if !ok {
		return
	}
	p.treeTab.mode = treeViewAll
	p.treeTab.modeExplicit = false
	for cur := node; cur.Parent != ""; {
		parent, ok := p.treeView.Node(cur.Parent)
		if !ok {
			break
		}
		delete(p.treeTab.subtreeCollapsed, parent.Name)
		cur = parent
	}
	if m.ContentLine >= 0 {
		if p.treeTab.expanded == nil {
			p.treeTab.expanded = make(map[string]bool)
		}
		p.treeTab.expanded[m.NodeName] = true
	}
	p.scrollToTreeNode(m.NodeName)
}

// stepBack returns one navigation level: an opened record closes to the view
// that opened it — the match list or the record list — and the match list
// closes to the record list. See TheoryOfRecordBrowser.
func (b *RecordBrowser) stepBack() {
	if b.currentID != 0 {
		b.backToList()
		return
	}
	b.showingMatches = false
}
