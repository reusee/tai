package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/reusee/tai/taiui"
	"github.com/reusee/tai/tree"
)

// newSearchTestTUI builds a minimal TUI whose layout is the three
// permanent tabs — Tree, Output, Logs — with the Tree tab expanded and
// focused, so the search keys reach the tree pane. See
// TheoryOfTreeSearch.
func newSearchTestTUI() *TUI {
	tu := &TUI{
		output:  taiui.NewLineBuffer(0),
		tabs:    taiui.NewTabs(3),
		scrolls: []taiui.ScrollState{{}, {}, {}},
		width:   100,
		height:  40,
	}
	tu.tabs.FocusTab(0)
	return tu
}

// searchTestTree builds a small session tree: a loop branch carrying an
// attempt node whose content holds the keyword twice on two lines, and
// a nested non-matching leaf. The keyword occurs only in the attempt
// node's content, so a match count above one proves the jump unit is
// the substring, not the node.
func searchTestTree(t *testing.T) *tree.Tree {
	t.Helper()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "loop-1", Type: tree.TypeLoop, Author: tree.AuthorProgram, Content: "loop one"},
		tree.WriteOp{Parent: "loop-1", Name: "attempt-1", Type: tree.TypeAttempt, Author: tree.AuthorProgram, Content: "attempt 1\nneedle first\nneedle second"},
		tree.WriteOp{Parent: "attempt-1", Name: "summary-1", Type: tree.TypeSummary, Author: tree.AuthorModel, Content: "no match here"},
	)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// searchDisplayText joins a display's lines for content assertions.
func searchDisplayText(display []taiui.Line) string {
	var sb strings.Builder
	for _, line := range display {
		sb.WriteString(line.Text)
		sb.WriteString("\n")
	}
	return sb.String()
}

// typeSearchKeyword types a keyword into the open search bar.
func typeSearchKeyword(tu *TUI, keyword string) {
	for _, r := range keyword {
		tu.handleKey(string(r))
	}
}

// TestTreeSearchOpensAndCloses verifies the "/" key opens the search
// bar on the focused tree pane, the bar's row is the one the pane's
// panel frees, and esc closes the search and restores the full tree.
// See TheoryOfTreeSearch.
func TestTreeSearchOpensAndCloses(t *testing.T) {
	tu := newSearchTestTUI()
	tu.treeView = searchTestTree(t)

	tu.handleKey("/")
	tu.mu.Lock()
	searching := tu.treeTab.searching
	boxes := tu.tabs.Boxes(tu.width, tu.height)
	inset := tu.tabRenderBox(0, boxes[0])
	searchEl := tu.treeSearchElement(tabTree, boxes[0])
	tu.mu.Unlock()
	if !searching {
		t.Fatal("/ must open the search bar")
	}
	// The search row occupies the row the inset panel freed, so the
	// panel, its toolbar, and its press tests all shift down one row.
	if inset.Top != boxes[0].Top+1 {
		t.Fatalf("searching pane must be inset one row, got top %d want %d", inset.Top, boxes[0].Top+1)
	}
	if searchEl == nil {
		t.Fatal("searching pane must render its search row")
	}

	tu.handleKey("esc")
	tu.mu.Lock()
	searching = tu.treeTab.searching
	line := tu.treeTab.searchBar.Line()
	rows := len(tu.treeDisplay(80, panelStyle.BaseBG))
	tu.mu.Unlock()
	if searching {
		t.Fatal("esc must close the search bar")
	}
	if line != "" {
		t.Fatalf("closing must clear the keyword, got %q", line)
	}
	if rows != 3 {
		t.Fatalf("expected the full tree after closing, got %d rows", rows)
	}
}

// TestTreeSearchKeepsAncestorPaths verifies the filtered display is the
// match set plus every ancestor, so a matched node's full path stays
// visible, and non-matching siblings are pruned. See
// TheoryOfTreeSearch.
func TestTreeSearchKeepsAncestorPaths(t *testing.T) {
	tu := newSearchTestTUI()
	tu.treeView = searchTestTree(t)

	tu.handleKey("/")
	typeSearchKeyword(tu, "needle")

	tu.mu.Lock()
	display := tu.treeDisplay(80, panelStyle.BaseBG)
	tu.mu.Unlock()
	text := searchDisplayText(display)
	for _, want := range []string{"needle first", "needle second"} {
		if !strings.Contains(text, want) {
			t.Fatalf("filtered display must contain %q, got:\n%s", want, text)
		}
	}
	// The match's ancestor is kept although it does not match.
	if !strings.Contains(text, "loop one") {
		t.Fatalf("the match's ancestor must stay visible, got:\n%s", text)
	}
	// The non-matching nested leaf is pruned.
	if strings.Contains(text, "no match here") {
		t.Fatalf("non-matching siblings must be pruned, got:\n%s", text)
	}
}

// TestTreeSearchJumpUnitIsSubstring verifies the jump unit is one
// matched substring: the keyword's two occurrences in one node's
// content are two matches, the cursor walks them, and a content match
// expands its node so the occurrence is visible. See
// TheoryOfTreeSearch.
func TestTreeSearchJumpUnitIsSubstring(t *testing.T) {
	tu := newSearchTestTUI()
	tu.treeView = searchTestTree(t)

	tu.handleKey("/")
	typeSearchKeyword(tu, "needle")

	tu.mu.Lock()
	matches := len(tu.treeTab.matches)
	first := tu.treeTab.matchIndex
	expanded := tu.treeTab.expanded["attempt-1"]
	tu.mu.Unlock()
	if matches != 2 {
		t.Fatalf("expected one match per occurrence, got %d", matches)
	}
	if first != 0 {
		t.Fatalf("typing must jump to the first match, got index %d", first)
	}
	if !expanded {
		t.Fatal("a content match must expand its node")
	}

	tu.handleKey("down")
	tu.mu.Lock()
	second := tu.treeTab.matchIndex
	tu.mu.Unlock()
	if second != 1 {
		t.Fatalf("the down key must move to the next occurrence, got index %d", second)
	}
	// The up key wraps back to the first occurrence.
	tu.handleKey("up")
	tu.mu.Lock()
	wrapped := tu.treeTab.matchIndex
	tu.mu.Unlock()
	if wrapped != 0 {
		t.Fatalf("the up key must wrap to the first occurrence, got index %d", wrapped)
	}
}

// TestTreeSearchButtonsNavigateAndConsumePress verifies a press on the
// search row's navigation buttons runs the button's action through the
// ordinary mouse path and that every press on the row is consumed by
// the search, never reaching the tab machine. See TheoryOfTreeSearch.
func TestTreeSearchButtonsNavigateAndConsumePress(t *testing.T) {
	tu := newSearchTestTUI()
	tu.treeView = searchTestTree(t)

	tu.handleKey("/")
	typeSearchKeyword(tu, "needle")

	boxes := tu.tabs.Boxes(tu.width, tu.height)
	row := taiui.Box{Top: boxes[0].Top, Left: boxes[0].Left, Bottom: boxes[0].Top + 1, Right: boxes[0].Right}
	slots := taiui.ToolbarLayout(row, treeSearchButtons)
	if len(slots) < 2 {
		t.Fatalf("search row must carry its buttons, got %d slots", len(slots))
	}
	buttonX := func(action string) int {
		for _, slot := range slots {
			if treeSearchButtons[slot.Index].Action == action {
				return slot.X0
			}
		}
		return -1
	}

	// The Next button advances the cursor from the first match.
	nextX := buttonX(treeSearchActionNext)
	if nextX < 0 {
		t.Fatal("the Next button is missing from the row")
	}
	tu.handleMouseKey(fmt.Sprintf("mouse-left@%d,%d", nextX, row.Top))
	tu.mu.Lock()
	index := tu.treeTab.matchIndex
	tu.mu.Unlock()
	if index != 1 {
		t.Fatalf("the Next button must advance the match cursor, got index %d", index)
	}

	// A press on a non-button cell of the row is consumed too, so the
	// tab machine never sees it and the tree stays focused.
	tu.handleMouseKey(fmt.Sprintf("mouse-left@%d,%d", row.Left, row.Top))
	tu.mu.Lock()
	focus := tu.tabs.Focus
	tu.mu.Unlock()
	if focus != 0 {
		t.Fatalf("a row press must not reach the tab machine, focus moved to %d", focus)
	}

	// The Cancel button closes the search.
	cancelX := buttonX(treeSearchActionCancel)
	if cancelX < 0 {
		t.Fatal("the Cancel button is missing from the row")
	}
	tu.handleMouseKey(fmt.Sprintf("mouse-left@%d,%d", cancelX, row.Top))
	tu.mu.Lock()
	searching := tu.treeTab.searching
	tu.mu.Unlock()
	if searching {
		t.Fatal("the Cancel button must close the search")
	}
}

// TestTreeSearchCursorFollowsSearch verifies the terminal cursor
// follows a focused searching pane, the same way it follows the chat
// input bar. See TheoryOfTreeSearch.
func TestTreeSearchCursorFollowsSearch(t *testing.T) {
	tu := newSearchTestTUI()
	tu.treeView = searchTestTree(t)

	if tu.treeSearchCursorWanted() {
		t.Fatal("the cursor must stay hidden while the pane does not search")
	}
	tu.handleKey("/")
	if !tu.treeSearchCursorWanted() {
		t.Fatal("the cursor must follow the focused searching pane")
	}
	tu.handleKey("esc")
	if tu.treeSearchCursorWanted() {
		t.Fatal("closing the search must release the cursor")
	}
}

// TestTreeFoldClickAlignsWhileSearching verifies a press on the fold
// column of a searching pane toggles the node its row shows: the pane
// insets below the search row, so the press row must map through the
// same inset box the panel renders in. See TheoryOfTreeSearch.
func TestTreeFoldClickAlignsWhileSearching(t *testing.T) {
	tu := newSearchTestTUI()
	tu.treeView = searchTestTree(t)

	tu.handleKey("/")
	typeSearchKeyword(tu, "needle")

	tu.mu.Lock()
	outer := tu.tabs.Boxes(tu.width, tu.height)[0]
	box := tu.tabRenderBox(0, outer)
	// Refresh the display so the recorded rows and fold columns are
	// the ones the render would record.
	_ = wrappedDisplay(tu, 0, outer)
	var target treeRowRange
	found := false
	for _, r := range tu.treeTab.rows {
		if r.expandable {
			target = r
			found = true
			break
		}
	}
	foldX := tu.treeTab.align.foldX
	tu.mu.Unlock()
	if !found {
		t.Fatal("no expandable node in the filtered tree")
	}
	if target.name != "attempt-1" {
		t.Fatalf("expected the attempt node as the expandable row, got %q", target.name)
	}
	if box.Top != outer.Top+1 {
		t.Fatalf("searching pane must be inset one row, top %d want %d", box.Top, outer.Top+1)
	}

	// The display row sits one panel-title row below the inset box's
	// top, so the screen y of the node's fold control is
	// box.Top + 1 + target.startRow.
	tu.handleMouseKey(fmt.Sprintf("mouse-left@%d,%d", box.Left+foldX, box.Top+1+target.startRow))

	tu.mu.Lock()
	expanded := tu.treeTab.expanded[target.name]
	tu.mu.Unlock()
	if expanded {
		t.Fatal("the fold press must toggle the node its row shows")
	}
}

// TestTreeSearchCollapsesUnmatchedAutoExpansion verifies an expansion
// the search made lasts exactly as long as the match: typing on until
// the keyword no longer matches the node restores the node's earlier
// collapsed state, and shortening the keyword expands it again. See
// TheoryOfTreeSearch.
func TestTreeSearchCollapsesUnmatchedAutoExpansion(t *testing.T) {
	tu := newSearchTestTUI()
	tu.treeView = searchTestTree(t)

	tu.handleKey("/")
	typeSearchKeyword(tu, "needle")
	tu.mu.Lock()
	expanded := tu.treeTab.expanded["attempt-1"]
	tu.mu.Unlock()
	if !expanded {
		t.Fatal("a content match must expand its node")
	}

	// Typing on past the match collapses the stale expansion and
	// leaves no tracking behind.
	tu.handleKey("z")
	tu.mu.Lock()
	matches := len(tu.treeTab.matches)
	expanded = tu.treeTab.expanded["attempt-1"]
	tracked := len(tu.treeTab.searchExpanded)
	tu.mu.Unlock()
	if matches != 0 {
		t.Fatalf("the extended keyword must match nothing, got %d matches", matches)
	}
	if expanded {
		t.Fatal("an unmatched auto-expanded node must collapse")
	}
	if tracked != 0 {
		t.Fatalf("the restored node must leave the tracking set, got %d entries", tracked)
	}

	// Deleting the extra character re-matches the node and the jump
	// expands it again.
	tu.handleKey("backspace")
	tu.mu.Lock()
	matches = len(tu.treeTab.matches)
	expanded = tu.treeTab.expanded["attempt-1"]
	tu.mu.Unlock()
	if matches != 2 {
		t.Fatalf("the shortened keyword must match both occurrences, got %d", matches)
	}
	if !expanded {
		t.Fatal("re-matching must expand the node again")
	}
}

// TestTreeSearchKeepsManualExpansionOnRestore verifies the restore
// returns an unmatched node to the expansion state it had before the
// search expanded it: a manual expansion survives, so the search never
// collapses content the user opened. See TheoryOfTreeSearch.
func TestTreeSearchKeepsManualExpansionOnRestore(t *testing.T) {
	tu := newSearchTestTUI()
	tu.treeView = searchTestTree(t)

	tu.mu.Lock()
	tu.toggleTreeNodeByName("attempt-1")
	before := tu.treeTab.expanded["attempt-1"]
	tu.mu.Unlock()
	if !before {
		t.Fatal("setup: the node must be expanded before the search")
	}

	tu.handleKey("/")
	typeSearchKeyword(tu, "needle")
	tu.handleKey("z")
	tu.mu.Lock()
	after := tu.treeTab.expanded["attempt-1"]
	tu.mu.Unlock()
	if !after {
		t.Fatal("the manual expansion must survive the search")
	}
}
