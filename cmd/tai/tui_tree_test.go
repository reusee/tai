package main

import (
	"strings"
	"testing"
	"time"

	"github.com/clipperhouse/displaywidth"
	"github.com/reusee/tai/taiui"
	"github.com/reusee/tai/tree"
)

// treeWithFinishNode builds a tree whose only event node is a finish
// node, the minimal tree a setTree call consumes to clear the
// generating hint. See TheoryOfTreeTab.
func treeWithFinishNode(t *testing.T) *tree.Tree {
	t.Helper()
	tr, err := tree.New().Write("root", "finish-1", tree.TypeFinish, tree.AuthorProgram, "finish: stop")
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// treeWithMultilineNode builds a tree with one multi-line event node,
// the fixture the Tree tab's collapse tests use. See TheoryOfTreeTab.
func treeWithMultilineNode(t *testing.T) *tree.Tree {
	t.Helper()
	tr, err := tree.New().Write("root", "handoff-1", tree.TypeHandoff, tree.AuthorProgram,
		"handoff summary:\nline one\nline two\nline three")
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// TestSetTreeConsumesSignals verifies that setTree consumes the tree's
// new event nodes once: an attempt-start node opens the output section
// the attempt will fill, a finish node clears the generating hint, and
// re-delivering the same tree triggers nothing again. setTree takes
// t.mu itself, so the test calls it outside the lock and inspects the
// state under the lock. See TheoryOfTreeTab and
// TheoryOfTUIOutputSections.
func TestSetTreeConsumesSignals(t *testing.T) {
	tui := newTUIForTest()
	tui.generating = true
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "attempt-start-1", Type: tree.TypeAttemptStart, Author: tree.AuthorProgram, Content: "attempt 1 start (1/3)"},
		tree.WriteOp{Parent: "root", Name: "finish-1", Type: tree.TypeFinish, Author: tree.AuthorProgram, Content: "finish: stop"},
	)
	if err != nil {
		t.Fatal(err)
	}
	tui.setTree(tr)

	tui.mu.Lock()
	if tui.pendingOwner == nil || tui.pendingOwner.attempt != 1 {
		t.Fatalf("expected pending owner attempt 1, got %+v", tui.pendingOwner)
	}
	if tui.generating {
		t.Fatal("expected the finish node to clear the generating hint")
	}
	if !tui.tabs.Expanded[1] {
		t.Fatal("the tree tab should auto-expand on event nodes")
	}
	// Re-arm the signals, release the lock, and re-deliver the same tree:
	// the already-seen event nodes must not re-trigger anything.
	tui.pendingOwner = nil
	tui.generating = true
	tui.mu.Unlock()
	tui.setTree(tr)

	tui.mu.Lock()
	defer tui.mu.Unlock()
	if tui.pendingOwner != nil || !tui.generating {
		t.Fatal("re-delivered event nodes must not re-trigger the signals")
	}
}

// TestTreeNodeCollapsedByDefault verifies the display contract: a node
// with multi-line content renders one collapsed header line by default
// showing the category, type, and content preview while the node name
// and author stay hidden; expanding reveals the name and author on the
// header and moves the content — first line included — onto the rows
// below the header, where a content row never folds it. The display
// width is wide enough for the header and the elapsed timer to stay on
// one line. See TheoryOfTreeTab.
func TestTreeNodeCollapsedByDefault(t *testing.T) {
	tui := newTUIForTest()
	tui.treeView = treeWithMultilineNode(t)
	tui.mu.Lock()
	defer tui.mu.Unlock()
	display := tui.treeDisplay(120, panelStyle.BaseBG)
	if len(display) != 1 {
		t.Fatalf("expected 1 collapsed row, got %d: %v", len(display), displayTexts(display))
	}
	if !strings.Contains(display[0].Text, "event") ||
		!strings.Contains(display[0].Text, "handoff") ||
		!strings.Contains(display[0].Text, "3 more lines") {
		t.Fatalf("unexpected collapsed header: %q", display[0].Text)
	}
	// The collapsed row hides the node name and author.
	if strings.Contains(display[0].Text, "handoff-1") ||
		strings.Contains(display[0].Text, "program") {
		t.Fatalf("the collapsed header must hide the name and author, got %q", display[0].Text)
	}
	// Expanding reveals the name and author on the header and the
	// content rows: the header carries no content, and every content
	// line starts on the row below it.
	tui.toggleTreeNodeAtRow(0)
	display = tui.treeDisplay(120, panelStyle.BaseBG)
	if len(display) != 5 {
		t.Fatalf("expected 5 expanded rows (header + 4 content lines), got %d", len(display))
	}
	if !strings.Contains(display[0].Text, "handoff-1") ||
		!strings.Contains(display[0].Text, "program") {
		t.Fatalf("the expanded header must reveal the name and author, got %q", display[0].Text)
	}
	if strings.Contains(display[0].Text, "handoff summary") {
		t.Fatalf("the expanded header must not carry content, got %q", display[0].Text)
	}
	for _, want := range []string{"line one", "line two", "line three"} {
		found := false
		for _, line := range display {
			if strings.Contains(line.Text, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected %q in the expanded display, got %v", want, display)
		}
	}
	// A content row of an expanded node never folds it.
	tui.toggleTreeNodeAtRow(2)
	display2 := tui.treeDisplay(120, panelStyle.BaseBG)
	if len(display2) != 5 {
		t.Fatalf("a body press must not fold the expanded node, got %d rows", len(display2))
	}
}

// TestTreeProjectionCycle verifies the projection cycling: the modes
// walk all, events, summary, model, program, user; each projection
// keeps the shown nodes' ancestors so the outline stays readable; and
// the tab label states the current projection. The collapsed rows hide
// node names, so the assertions read the content previews. See
// TheoryOfTreeTab.
func TestTreeProjectionCycle(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "user-1", Type: tree.TypeUser, Author: tree.AuthorUser, Content: "task"},
		tree.WriteOp{Parent: "root", Name: "attempt-start-1", Type: tree.TypeAttemptStart, Author: tree.AuthorProgram, Content: "attempt 1 start (1/3)"},
		tree.WriteOp{Parent: "root", Name: "completed-1", Type: tree.TypeCompleted, Author: tree.AuthorProgram, Content: "attempt 1 complete"},
	)
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr

	// All mode: every node.
	tui.mu.Lock()
	display := tui.treeDisplay(60, panelStyle.BaseBG)
	if len(display) < 3 {
		t.Fatalf("expected the full outline, got %d rows", len(display))
	}
	tui.mu.Unlock()

	// Events projection: only the event nodes plus ancestors.
	tui.cycleTreeView()
	if tui.treeTab.mode != treeViewEvents {
		t.Fatalf("expected the events projection, got %d", tui.treeTab.mode)
	}
	tui.mu.Lock()
	display = tui.treeDisplay(60, panelStyle.BaseBG)
	tui.mu.Unlock()
	for _, want := range []string{"attempt 1 start", "attempt 1 complete"} {
		found := false
		for _, line := range display {
			if strings.Contains(line.Text, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected %q in the events projection, got %v", want, display)
		}
	}
	for _, line := range display {
		if strings.Contains(line.Text, "task") {
			t.Fatalf("the user node must be pruned from the events projection, got %v", display)
		}
	}
	if tui.treeTabLabel() != "Tree (events)" {
		t.Fatalf("unexpected projection label: %q", tui.treeTabLabel())
	}

	// User projection: the user node plus its ancestors.
	tui.cycleTreeView()
	tui.cycleTreeView()
	tui.cycleTreeView()
	tui.cycleTreeView()
	if tui.treeTab.mode != treeViewUser {
		t.Fatalf("expected the user projection, got %d", tui.treeTab.mode)
	}
	tui.mu.Lock()
	display = tui.treeDisplay(60, panelStyle.BaseBG)
	tui.mu.Unlock()
	found := false
	for _, line := range display {
		if strings.Contains(line.Text, "task") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the user's input node in the user projection, got %v", display)
	}

	// The projection wraps around to all.
	tui.cycleTreeView()
	if tui.treeTab.mode != treeViewAll {
		t.Fatalf("expected the projection to wrap to all, got %d", tui.treeTab.mode)
	}
}

// TestTreeToggleLastExpandable verifies that Enter expands or collapses
// the last expandable node in depth-first order — typically the latest
// handoff or completion summary. The display width keeps the collapsed
// header on one line. See TheoryOfTreeTab.
func TestTreeToggleLastExpandable(t *testing.T) {
	tui := newTUIForTest()
	tui.treeView = treeWithMultilineNode(t)
	tui.toggleLastTreeExpandable()
	tui.mu.Lock()
	display := tui.treeDisplay(120, panelStyle.BaseBG)
	tui.mu.Unlock()
	if len(display) != 5 {
		t.Fatalf("expected the last expandable node expanded, got %d rows", len(display))
	}
	tui.toggleLastTreeExpandable()
	tui.mu.Lock()
	display = tui.treeDisplay(120, panelStyle.BaseBG)
	tui.mu.Unlock()
	if len(display) != 1 {
		t.Fatalf("expected the node collapsed again, got %d rows", len(display))
	}
}

// TestTreeCollapseAllToggle verifies the c key's fold of the tree
// nodes: the first press snapshots the expanded nodes, folds every
// node to its one-line header, and scrolls the view to the top so the
// collapsed list starts below the title; the next press restores the
// expansions; a manual expand breaks the all-collapsed state, so the
// next press folds and re-snapshots again; and a node that arrives
// after the snapshot keeps the collapsed form on restore. See
// TheoryOfTreeTab.
func TestTreeCollapseAllToggle(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "wide-1", Type: tree.TypeHandoff, Author: tree.AuthorProgram, Content: "head\nbody"},
		tree.WriteOp{Parent: "root", Name: "plain-1", Type: tree.TypeCompleted, Author: tree.AuthorProgram, Content: "one line"},
	)
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr

	// Expand wide-1, then fold: every node shows one header row, and
	// the view lands on the top so the list starts below the title.
	tui.mu.Lock()
	tui.toggleTreeNodeByName("wide-1")
	tui.width, tui.height = 80, 25
	tui.tabs.Expanded[1] = true
	tui.scrolls[1].Offset = 5
	tui.mu.Unlock()
	tui.collapseAllTreeNodes()
	tui.mu.Lock()
	display := tui.treeDisplay(120, panelStyle.BaseBG)
	offset := tui.scrolls[1].Offset
	tui.mu.Unlock()
	if len(display) != 2 {
		t.Fatalf("expected every node folded to one row, got %d rows", len(display))
	}
	if offset != 0 {
		t.Fatalf("expected the fold to scroll the view to the top, got offset %d", offset)
	}

	// Press again: wide-1 expands again with its content — first line
	// included — below the header.
	tui.collapseAllTreeNodes()
	tui.mu.Lock()
	display = tui.treeDisplay(120, panelStyle.BaseBG)
	tui.mu.Unlock()
	if len(display) != 4 {
		t.Fatalf("expected wide-1 expanded again, got %d rows", len(display))
	}

	// A manual expand after the fold breaks the all-collapsed state:
	// the next press folds and re-snapshots instead of restoring.
	tui.collapseAllTreeNodes()
	tui.mu.Lock()
	tui.toggleTreeNodeByName("wide-1")
	tui.mu.Unlock()
	tui.collapseAllTreeNodes()
	tui.mu.Lock()
	display = tui.treeDisplay(120, panelStyle.BaseBG)
	tui.mu.Unlock()
	if len(display) != 2 {
		t.Fatalf("expected the manual expand to be folded, got %d rows", len(display))
	}

	// The next press restores the manual expand.
	tui.collapseAllTreeNodes()
	tui.mu.Lock()
	display = tui.treeDisplay(120, panelStyle.BaseBG)
	tui.mu.Unlock()
	if len(display) != 4 {
		t.Fatalf("expected the manual expand restored, got %d rows", len(display))
	}

	// A node that arrives after the snapshot keeps the collapsed form
	// on restore: the snapshot carries only the nodes it captured.
	tr2, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "wide-1", Type: tree.TypeHandoff, Author: tree.AuthorProgram, Content: "head\nbody"},
		tree.WriteOp{Parent: "root", Name: "late-1", Type: tree.TypeCompleted, Author: tree.AuthorProgram, Content: "late\nbody"},
	)
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr2
	tui.collapseAllTreeNodes()
	tui.collapseAllTreeNodes()
	tui.mu.Lock()
	display = tui.treeDisplay(120, panelStyle.BaseBG)
	tui.mu.Unlock()
	if len(display) != 4 {
		t.Fatalf("expected wide-1 expanded and late-1 collapsed, got %d rows", len(display))
	}
}

// TestTreeCollapseAllTruncatedHeaderExpanded verifies that the c key's
// fold counts every expanded node — including one whose single-line
// header truncates at the pane width, expandable in the display but not
// per the multi-line predicate — so a manual expand of it breaks the
// all-collapsed state and the next press folds and re-snapshots instead
// of restoring a stale snapshot. See TheoryOfTreeTab.
func TestTreeCollapseAllTruncatedHeaderExpanded(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "wide-1", Type: tree.TypeHandoff, Author: tree.AuthorProgram, Content: "head\nbody"},
		tree.WriteOp{Parent: "root", Name: "long-1", Type: tree.TypeCompleted, Author: tree.AuthorProgram, Content: strings.Repeat("x", 200)},
	)
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr
	tui.mu.Lock()
	tui.width, tui.height = 80, 25
	tui.tabs.Expanded[1] = true
	tui.toggleTreeNodeByName("wide-1")
	tui.toggleTreeNodeByName("long-1")
	tui.mu.Unlock()

	// First press folds everything.
	tui.collapseAllTreeNodes()

	// Manually expand the truncated-header node, then press again:
	// the manual expand breaks the all-collapsed state, so the press
	// folds and re-snapshots instead of restoring the stale snapshot.
	tui.mu.Lock()
	tui.toggleTreeNodeByName("long-1")
	tui.mu.Unlock()
	tui.collapseAllTreeNodes()
	tui.mu.Lock()
	display := tui.treeDisplay(120, panelStyle.BaseBG)
	expandedLong := tui.treeTab.expanded["long-1"]
	tui.mu.Unlock()
	if len(display) != 2 {
		t.Fatalf("expected every node folded to one row after the manual-expand press, got %d rows", len(display))
	}
	if expandedLong {
		t.Fatal("expected long-1 folded after the manual-expand press")
	}

	// The next press restores the manual expand.
	tui.collapseAllTreeNodes()
	tui.mu.Lock()
	expandedLong = tui.treeTab.expanded["long-1"]
	tui.mu.Unlock()
	if !expandedLong {
		t.Fatal("expected the manual expand restored")
	}
}

// TestTreeExpandScrollsToNodeStart verifies the expansion invariant:
// every expansion path — a press on the node's rows, and Enter on the
// last expandable node — scrolls the view to the node's first display
// row and stops following the tail; collapsing never scrolls. The
// fixture puts the multi-line node far enough down that its first row
// sits inside the scrollable range, so the expected offset is exact.
// See TheoryOfTreeTab.
func TestTreeExpandScrollsToNodeStart(t *testing.T) {
	tui := newTUIForTest()
	wideContent := "wide header\n" + strings.TrimRight(strings.Repeat("wide body\n", 30), "\n")
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "plain-1", Type: tree.TypeCompleted, Author: tree.AuthorProgram, Content: "one"},
		tree.WriteOp{Parent: "root", Name: "plain-2", Type: tree.TypeCompleted, Author: tree.AuthorProgram, Content: "two"},
		tree.WriteOp{Parent: "root", Name: "plain-3", Type: tree.TypeCompleted, Author: tree.AuthorProgram, Content: "three"},
		tree.WriteOp{Parent: "root", Name: "plain-4", Type: tree.TypeCompleted, Author: tree.AuthorProgram, Content: "four"},
		tree.WriteOp{Parent: "root", Name: "plain-5", Type: tree.TypeCompleted, Author: tree.AuthorProgram, Content: "five"},
		tree.WriteOp{Parent: "root", Name: "wide-1", Type: tree.TypeHandoff, Author: tree.AuthorProgram, Content: wideContent},
	)
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr
	tui.mu.Lock()
	tui.width, tui.height = 80, 25
	tui.tabs.Expanded[1] = true
	tui.scrolls[1].Offset = 0
	tui.scrolls[1].Follow = true
	box := tui.tabs.Boxes(tui.width, tui.height)[1]
	display := tui.treeDisplay(treeContentWidth(box.Width()), panelStyle.BaseBG)
	if len(display) != 6 {
		t.Fatalf("expected 6 collapsed rows, got %d", len(display))
	}
	tui.mu.Unlock()

	// A press on the wide node's row expands it and scrolls the view
	// to its first display row.
	tui.mu.Lock()
	tui.toggleTreeNodeAtRow(5)
	offset := tui.scrolls[1].Offset
	follow := tui.scrolls[1].Follow
	expanded := tui.treeTab.expanded["wide-1"]
	tui.mu.Unlock()
	if !expanded {
		t.Fatal("the row press must expand wide-1")
	}
	if offset != 5 || follow {
		t.Fatalf("expanding must scroll to the node's first row and stop following, got offset %d follow %v", offset, follow)
	}

	// Enter collapses the last expandable node without scrolling.
	tui.toggleLastTreeExpandable()
	tui.mu.Lock()
	collapsed := !tui.treeTab.expanded["wide-1"]
	collapseOffset := tui.scrolls[1].Offset
	tui.mu.Unlock()
	if !collapsed || collapseOffset != 5 {
		t.Fatalf("collapsing must not scroll, got collapsed=%v offset=%d", collapsed, collapseOffset)
	}

	// Enter expands it again, scrolling to its first row once more.
	tui.toggleLastTreeExpandable()
	tui.mu.Lock()
	reexpanded := tui.treeTab.expanded["wide-1"]
	reoffset := tui.scrolls[1].Offset
	tui.mu.Unlock()
	if !reexpanded || reoffset != 5 {
		t.Fatalf("re-expanding must scroll to the node's first row, got expanded=%v offset=%d", reexpanded, reoffset)
	}
}

// TestTreeProjectionAncestors verifies that a projection keeps the
// shown nodes' ancestors, so a summary node stays readable in its path
// context. The collapsed rows hide node names, so the assertions read
// the content previews. See TheoryOfTreeTab.
func TestTreeProjectionAncestors(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "user-1", Type: tree.TypeUser, Author: tree.AuthorUser, Content: "task"},
		tree.WriteOp{Parent: "user-1", Name: "model-1", Type: tree.TypeModel, Author: tree.AuthorModel, Content: "resp"},
		tree.WriteOp{Parent: "model-1", Name: "summary-1", Type: tree.TypeSummary, Author: tree.AuthorModel, Content: "sum"},
	)
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr
	tui.cycleTreeView() // events: nothing shown
	tui.cycleTreeView() // summary: summary-1 plus ancestors
	if tui.treeTab.mode != treeViewSummary {
		t.Fatalf("expected the summary projection, got %d", tui.treeTab.mode)
	}
	tui.mu.Lock()
	defer tui.mu.Unlock()
	display := tui.treeDisplay(60, panelStyle.BaseBG)
	for _, want := range []string{"task", "resp", "sum"} {
		found := false
		for _, line := range display {
			if strings.Contains(line.Text, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected %q in the summary projection, got %v", want, display)
		}
	}
}

// TestTreeAttemptStartJumpMarker verifies that an attempt-start node's
// header carries the jump marker and parses the attempt number its
// content carries. See TheoryOfTreeTab and TheoryOfTUIOutputSections.
func TestTreeAttemptStartJumpMarker(t *testing.T) {
	tr, err := tree.New().Write("root", "attempt-start-1", tree.TypeAttemptStart, tree.AuthorProgram,
		"attempt 3 start (1/3)")
	if err != nil {
		t.Fatal(err)
	}
	node, _ := tr.Node("attempt-start-1")
	if header := treeHeaderText(node, 0, false, "", displaywidth.Options{}, treeAlignments{}); !strings.HasSuffix(header, eventJumpMarker) {
		t.Fatalf("expected the jump marker on the attempt-start header, got %q", header)
	}
	num, ok := attemptNumberOf(node.Content)
	if !ok || num != 3 {
		t.Fatalf("attemptNumberOf(%q) = %d, %v; want 3, true", node.Content, num, ok)
	}
}

// TestTreeElapsedTimer verifies that a node's first display line
// right-aligns the elapsed timer and a wrapped header never collides
// with it. See TheoryOfTreeTab.
func TestTreeElapsedTimer(t *testing.T) {
	tui := newTUIForTest()
	tui.startTime = time.Now().Add(-70 * time.Second)
	tr, err := tree.New().Write("root", "attempt-start-1", tree.TypeAttemptStart, tree.AuthorProgram,
		"attempt 1 start (1/3)")
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr
	tui.mu.Lock()
	defer tui.mu.Unlock()
	display := tui.treeDisplay(80, panelStyle.BaseBG)
	if len(display) == 0 {
		t.Fatal("expected display rows")
	}
	if !strings.Contains(display[0].Text, "+1:10") {
		t.Fatalf("expected the elapsed timer on the header row, got %q", display[0].Text)
	}
}

// TestTreeHeaderAlignment verifies the global column alignment: every
// header pads its category fragment to the widest visible category
// fragment and its type fragment to the widest visible type fragment,
// across every indent level, so the type fragments and the content
// previews start at the same display column on every row — the
// content column never interleaves with the category/type columns.
// Emoji fragments differ in byte length, so the columns are measured
// in display width, the same measurement the renderer pads with. See
// TheoryOfTreeTab.
func TestTreeHeaderAlignment(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "a", Type: tree.TypeUser, Author: tree.AuthorUser, Content: "x"},
		tree.WriteOp{Parent: "root", Name: "longer-name", Type: tree.TypeLoop, Author: tree.AuthorProgram, Content: "y"},
		tree.WriteOp{Parent: "longer-name", Name: "deep", Type: tree.TypeUser, Author: tree.AuthorUser, Content: "z"},
	)
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr
	tui.mu.Lock()
	defer tui.mu.Unlock()
	display := tui.treeDisplay(120, panelStyle.BaseBG)
	if len(display) != 3 {
		t.Fatalf("expected 3 rows, got %d: %v", len(display), displayTexts(display))
	}
	options := taiui.DisplayWidthOptions()
	// columnOf returns the display column of the fragment's first byte:
	// the renderer pads in display columns, so the alignment contract
	// is a display-width contract, not a byte-offset one.
	columnOf := func(line taiui.Line, fragment string) (int, bool) {
		idx := strings.Index(line.Text, fragment)
		if idx < 0 {
			return 0, false
		}
		return options.String(line.Text[:idx]), true
	}
	catCol0, ok0 := columnOf(display[0], "💬")
	catCol1, ok1 := columnOf(display[1], "🔁")
	if !ok0 || !ok1 || catCol0 != catCol1 {
		t.Fatalf("expected aligned category columns, got %q and %q", display[0].Text, display[1].Text)
	}
	typeCol0, ok0 := columnOf(display[0], "user")
	typeCol1, ok1 := columnOf(display[1], "loop")
	if !ok0 || !ok1 || typeCol0 != typeCol1 {
		t.Fatalf("expected aligned type columns, got %q and %q", display[0].Text, display[1].Text)
	}
	contentCol0, ok0 := columnOf(display[0], "x")
	contentCol1, ok1 := columnOf(display[1], "y")
	if !ok0 || !ok1 || contentCol0 != contentCol1 {
		t.Fatalf("expected aligned content columns, got %q and %q", display[0].Text, display[1].Text)
	}
	// The deeper node's fragment sits right of the shallower ones, and
	// its content still starts at the one fixed content column.
	contentCol2, ok2 := columnOf(display[2], "z")
	if !ok2 || contentCol2 != contentCol0 {
		t.Fatalf("expected the deep node's content at the fixed content column, got %q", display[2].Text)
	}
}

// TestTreeExpansionWrapsBody verifies the expansion contract: a node
// collapsed renders one truncated header row, and expanding moves the
// content — first line included — onto the body rows below the header,
// wrapped at the pane width instead of truncated, so the full content
// becomes visible and no body row carries the truncation mark. See
// TheoryOfTreeTab.
func TestTreeExpansionWrapsBody(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().Write("root", "wide-1", tree.TypeHandoff, tree.AuthorProgram,
		"first\n"+strings.Repeat("x", 200)+"\n"+strings.Repeat("y", 200))
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr
	tui.mu.Lock()
	defer tui.mu.Unlock()
	// Collapsed: exactly one truncated header row.
	display := tui.treeDisplay(40, panelStyle.BaseBG)
	if len(display) != 1 {
		t.Fatalf("expected 1 collapsed row, got %d", len(display))
	}
	if !strings.Contains(display[0].Text, "…") {
		t.Fatalf("expected a truncated header, got %q", display[0].Text)
	}
	// Expanded: body lines wrap, never truncate; every row fits the
	// content width.
	tui.toggleTreeNodeAtRow(0)
	display = tui.treeDisplay(40, panelStyle.BaseBG)
	body := ""
	for i, line := range display {
		if w := displaywidth.String(line.Text); w > 40 {
			t.Fatalf("a display row must not exceed the content width, got %d: %q", w, line.Text)
		}
		if i == 0 {
			continue
		}
		if strings.Contains(line.Text, "…") {
			t.Fatalf("a body row must wrap, not truncate: %q", line.Text)
		}
		body += line.Text
	}
	// The expanded header carries no content; the body starts from the
	// first line and carries both 200-character runs in full.
	if strings.Contains(display[0].Text, "first") {
		t.Fatalf("the expanded header must not carry content, got %q", display[0].Text)
	}
	if !strings.Contains(body, "first") {
		t.Fatalf("the body must start from the first line, got %q", body)
	}
	if strings.Count(body, "x") != 200 || strings.Count(body, "y") != 200 {
		t.Fatalf("expansion must reveal the full content, got %q", body)
	}
}

// TestTreeExpandedContentStartsOnNextLine verifies the expanded form's
// layout contract: the header row carries the structural columns plus
// the name and author, and the content, first line included, starts on
// the row below the header. See TheoryOfTreeTab.
func TestTreeExpandedContentStartsOnNextLine(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().Write("root", "node-1", tree.TypeHandoff, tree.AuthorProgram,
		"first line\nsecond line")
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr
	tui.mu.Lock()
	defer tui.mu.Unlock()
	display := tui.treeDisplay(120, panelStyle.BaseBG)
	if len(display) != 1 {
		t.Fatalf("expected 1 collapsed row, got %d", len(display))
	}
	tui.toggleTreeNodeAtRow(0)
	display = tui.treeDisplay(120, panelStyle.BaseBG)
	if len(display) != 3 {
		t.Fatalf("expected 3 rows (header + 2 content lines), got %d: %v", len(display), displayTexts(display))
	}
	// The expanded header reveals the name and author and carries no
	// content. See TheoryOfTreeTab.
	if !strings.Contains(display[0].Text, "node-1") || !strings.Contains(display[0].Text, "program") {
		t.Fatalf("the expanded header must reveal the name and author, got %q", display[0].Text)
	}
	if strings.Contains(display[0].Text, "first line") || strings.Contains(display[0].Text, "second line") {
		t.Fatalf("the expanded header must not carry content, got %q", display[0].Text)
	}
	if !strings.Contains(display[1].Text, "first line") {
		t.Fatalf("the content must start on the row below the header, got %q", display[1].Text)
	}
	if !strings.Contains(display[2].Text, "second line") {
		t.Fatalf("expected the second content line on its own row, got %q", display[2].Text)
	}
}

// TestTreeExpandedContentSkipsLeadingBlankLines verifies that content
// beginning with blank lines — prompt constants conventionally begin
// with a newline after the Go raw-string backtick — previews from and
// expands to the first non-blank line: the collapsed header carries
// that line, and the expanded body renders the full content. See
// TheoryOfTreeTab.
func TestTreeExpandedContentSkipsLeadingBlankLines(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().Write("root", "node-1", tree.TypeSystem, tree.AuthorProgram,
		"\nfirst line\nsecond line")
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr
	tui.mu.Lock()
	defer tui.mu.Unlock()
	display := tui.treeDisplay(120, panelStyle.BaseBG)
	if len(display) != 1 {
		t.Fatalf("expected 1 collapsed row, got %d", len(display))
	}
	if !strings.Contains(display[0].Text, "first line") {
		t.Fatalf("the collapsed preview must carry the first non-blank line, got %q", display[0].Text)
	}
	tui.toggleTreeNodeAtRow(0)
	display = tui.treeDisplay(120, panelStyle.BaseBG)
	if len(display) != 3 {
		t.Fatalf("expected 3 rows (header + 2 content lines), got %d: %v", len(display), displayTexts(display))
	}
	if !strings.Contains(display[1].Text, "first line") {
		t.Fatalf("the expanded body must start with the first non-blank line, got %q", display[1].Text)
	}
	if !strings.Contains(display[2].Text, "second line") {
		t.Fatalf("expected the second content line on its own row, got %q", display[2].Text)
	}
}

// TestTreeFoldColumnToggles verifies the fold column's contract: an
// expandable node carries the fold glyph in its fold slot on the
// header row, right of the category/type columns; a press on the fold
// column's cells toggles the node and scrolls the view to its first
// row; a single-line node carries a blank slot; and the content
// column starts at one fixed display column on every row. See
// TheoryOfTreeTab.
func TestTreeFoldColumnToggles(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "wide-1", Type: tree.TypeHandoff, Author: tree.AuthorProgram, Content: "head\nbody"},
		tree.WriteOp{Parent: "root", Name: "plain-1", Type: tree.TypeCompleted, Author: tree.AuthorProgram, Content: "one line"},
	)
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr
	tui.mu.Lock()
	tui.width, tui.height = 80, 25
	tui.tabs.Expanded[1] = true
	tui.scrolls[1].Offset = 0
	box := tui.tabs.Boxes(tui.width, tui.height)[1]
	display := tui.treeDisplay(treeContentWidth(box.Width()), panelStyle.BaseBG)
	rows := tui.treeTab.rows
	if len(rows) != 2 || !rows[0].expandable || rows[1].expandable {
		t.Fatalf("expected wide-1 expandable and plain-1 not, got %+v", rows)
	}
	align := tui.treeTab.align
	options := taiui.DisplayWidthOptions()
	columnOf := func(line taiui.Line, fragment string) (int, bool) {
		idx := strings.Index(line.Text, fragment)
		if idx < 0 {
			return 0, false
		}
		return options.String(line.Text[:idx]), true
	}
	// The fold glyph sits at the fold column on the expandable node's
	// header row; the single-line node's slot is blank. The content
	// column starts at one fixed display column on both rows.
	glyphCol, ok := columnOf(display[rows[0].startRow], sectionGlyphCollapsed)
	if !ok || glyphCol != align.foldX {
		t.Fatalf("expected the collapsed glyph at the fold column %d, got %d", align.foldX, glyphCol)
	}
	if strings.Contains(display[rows[1].startRow].Text, sectionGlyphCollapsed) {
		t.Fatalf("the single-line node must carry a blank slot, got %q", display[rows[1].startRow].Text)
	}
	contentCol0, ok0 := columnOf(display[rows[0].startRow], "head")
	contentCol1, ok1 := columnOf(display[rows[1].startRow], "one")
	if !ok0 || !ok1 || contentCol0 != contentCol1 {
		t.Fatalf("expected aligned content columns, got %q and %q", display[rows[0].startRow].Text, display[rows[1].startRow].Text)
	}
	foldPressX := box.Left + align.foldX
	foldPressY := box.Top + 1 + rows[0].startRow
	tui.mu.Unlock()

	// A press on the fold column's cells on the header row toggles the
	// node and scrolls the view to its first row.
	tui.mu.Lock()
	consumed := tui.toggleTreeControlAtClick(foldPressX, foldPressY)
	expanded := tui.treeTab.expanded["wide-1"]
	offset := tui.scrolls[1].Offset
	tui.mu.Unlock()
	if !consumed || !expanded {
		t.Fatalf("the fold press must toggle wide-1, got consumed=%v expanded=%v", consumed, expanded)
	}
	if offset != 0 {
		t.Fatalf("expanding a collapsed node must scroll to its first row, got offset %d", offset)
	}

	// A second press collapses the node again.
	tui.mu.Lock()
	consumed = tui.toggleTreeControlAtClick(foldPressX, foldPressY)
	collapsed := !tui.treeTab.expanded["wide-1"]
	tui.mu.Unlock()
	if !consumed || !collapsed {
		t.Fatalf("the second press must collapse wide-1, got consumed=%v collapsed=%v", consumed, collapsed)
	}
}

// TestCollapseAllKeyDispatchesByFocus verifies the c key's dispatch:
// the Tree tab's focus folds the tree nodes, every other focus folds
// the Output tab's sections. See TheoryOfTreeTab and
// TheoryOfOutputControls.
func TestCollapseAllKeyDispatchesByFocus(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "wide-1", Type: tree.TypeHandoff, Author: tree.AuthorProgram, Content: "head\nbody"},
	)
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr
	tui.mu.Lock()
	tui.toggleTreeNodeByName("wide-1")
	tui.mu.Unlock()

	// The Output tab's focus folds the output sections; the tree node
	// stays expanded.
	tui.mu.Lock()
	tui.tabs.Focus = 0
	tui.mu.Unlock()
	tui.handleKey("c")
	tui.mu.Lock()
	rows := len(tui.treeDisplay(120, panelStyle.BaseBG))
	tui.mu.Unlock()
	if rows != 3 {
		t.Fatalf("expected the tree node to stay expanded under the Output tab's focus, got %d rows", rows)
	}

	// The Tree tab's focus folds the tree nodes.
	tui.mu.Lock()
	tui.tabs.Focus = 1
	tui.mu.Unlock()
	tui.handleKey("c")
	tui.mu.Lock()
	rows = len(tui.treeDisplay(120, panelStyle.BaseBG))
	tui.mu.Unlock()
	if rows != 1 {
		t.Fatalf("expected the tree node folded under the Tree tab's focus, got %d rows", rows)
	}
}

// TestTreeSingleLineTruncatedExpands verifies the width-dependent
// expandability: a single-line node whose header truncates at the
// pane width carries the fold column's glyph, and pressing the fold
// column's cells on its header row expands the node to reveal the
// full line wrapped; a single-line node whose header fits carries a
// blank slot. See TheoryOfTreeTab.
func TestTreeSingleLineTruncatedExpands(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "long-1", Type: tree.TypeUser, Author: tree.AuthorUser,
			Content: strings.Repeat("z", 120)},
		tree.WriteOp{Parent: "root", Name: "short-1", Type: tree.TypeCompleted, Author: tree.AuthorProgram,
			Content: "brief"},
	)
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr
	tui.mu.Lock()
	defer tui.mu.Unlock()
	tui.width, tui.height = 80, 25
	tui.tabs.Expanded[1] = true
	tui.scrolls[1].Offset = 0
	box := tui.tabs.Boxes(tui.width, tui.height)[1]
	contentWidth := treeContentWidth(box.Width())
	display := tui.treeDisplay(contentWidth, panelStyle.BaseBG)
	rows := tui.treeTab.rows
	if len(rows) != 2 || !rows[0].expandable || rows[1].expandable {
		t.Fatalf("expected the truncated long-1 expandable and short-1 not, got %+v", rows)
	}
	// Pressing the fold column's cells on the truncated node's header
	// row expands it; the full line renders wrapped in the body rows.
	align := tui.treeTab.align
	foldLeft := box.Left + align.foldX
	if !tui.toggleTreeControlAtClick(foldLeft, box.Top+1+rows[0].startRow) {
		t.Fatal("the fold press must toggle long-1")
	}
	display = tui.treeDisplay(contentWidth, panelStyle.BaseBG)
	body := ""
	for _, line := range display[1:] {
		if w := displaywidth.String(line.Text); w > contentWidth {
			t.Fatalf("a display row must not exceed the content width, got %d: %q", w, line.Text)
		}
		body += line.Text
	}
	if strings.Count(body, "z") != 120 {
		t.Fatalf("expansion must reveal the full single-line content, got %q", body)
	}
}

// TestTreeShowsAllLoops verifies that the display covers every goal
// loop: the walk starts at the tree root, so both loops' nodes render
// in the all projection, and the user projection keeps the matched
// nodes of every loop. The collapsed rows hide node names, so the
// assertions read the content previews. See TheoryOfTreeTab.
func TestTreeShowsAllLoops(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "loop-1", Type: tree.TypeLoop, Author: tree.AuthorProgram, Content: "loop one"},
		tree.WriteOp{Parent: "loop-1", Name: "user-1", Type: tree.TypeUser, Author: tree.AuthorUser, Content: "task one"},
		tree.WriteOp{Parent: "root", Name: "loop-2", Type: tree.TypeLoop, Author: tree.AuthorProgram, Content: "loop two"},
		tree.WriteOp{Parent: "loop-2", Name: "user-2", Type: tree.TypeUser, Author: tree.AuthorUser, Content: "task two"},
	)
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr
	tui.mu.Lock()
	display := tui.treeDisplay(120, panelStyle.BaseBG)
	tui.mu.Unlock()
	for _, want := range []string{"loop one", "task one", "loop two", "task two"} {
		found := false
		for _, line := range display {
			if strings.Contains(line.Text, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected %q in the all projection, got %v", want, displayTexts(display))
		}
	}
	tui.setTreeView(treeViewUser)
	tui.mu.Lock()
	display = tui.treeDisplay(120, panelStyle.BaseBG)
	tui.mu.Unlock()
	for _, want := range []string{"task one", "task two"} {
		found := false
		for _, line := range display {
			if strings.Contains(line.Text, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected %q in the user projection, got %v", want, displayTexts(display))
		}
	}
}

// TestTreeViewMenuActions verifies that the View menu carries one
// item per Tree projection and that dispatching an action selects it,
// mirroring the v key's cycling. See TheoryOfTreeTab.
func TestTreeViewMenuActions(t *testing.T) {
	tui := newTUIForTest()
	viewEntry := menuBarEntries[1]
	if viewEntry.title != "View" {
		t.Fatalf("expected the View category, got %q", viewEntry.title)
	}
	treeItems := 0
	for _, item := range viewEntry.items {
		var mode treeViewMode
		switch item.action {
		case controlTreeViewAll:
			mode = treeViewAll
		case controlTreeViewEvents:
			mode = treeViewEvents
		case controlTreeViewSummary:
			mode = treeViewSummary
		case controlTreeViewModel:
			mode = treeViewModel
		case controlTreeViewProgram:
			mode = treeViewProgram
		case controlTreeViewUser:
			mode = treeViewUser
		default:
			continue
		}
		treeItems++
		tui.dispatchControlBar(item.action)
		if tui.treeTab.mode != mode {
			t.Fatalf("dispatching %q should select the projection, got %d", item.action, tui.treeTab.mode)
		}
	}
	if treeItems != int(treeViewModeCount) {
		t.Fatalf("expected %d tree view menu items, got %d", treeViewModeCount, treeItems)
	}
}

// TestTreeDoubleClickTogglesText verifies the Tree pane's press
// contract: a single click on a node's text does nothing, two presses
// at the same cell within treeDoubleClickWindow toggle the node, the
// pair resets after a toggle, a press at a different cell does not
// continue the pair, and a press after the window expires it. See
// TheoryOfTreeTab.
func TestTreeDoubleClickTogglesText(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().Write("root", "wide-1", tree.TypeHandoff, tree.AuthorProgram, "head\nbody")
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr
	tui.mu.Lock()
	tui.width, tui.height = 80, 25
	tui.tabs.Expanded[1] = true
	tui.scrolls[1].Offset = 0
	box := tui.tabs.Boxes(tui.width, tui.height)[1]
	tui.treeDisplay(treeContentWidth(box.Width()), panelStyle.BaseBG)
	// Column 5 sits in the text area, clear of the fold column; the
	// row below the title is the node's header row.
	textX, textY := box.Left+5, box.Top+1
	tui.mu.Unlock()

	// A single click on the text does not toggle the node.
	tui.mu.Lock()
	tui.treeAtClick(textX, textY)
	if tui.treeTab.expanded["wide-1"] {
		t.Fatal("a single text press must not toggle the node")
	}
	// The second press at the same cell within the window toggles.
	tui.treeAtClick(textX, textY)
	if !tui.treeTab.expanded["wide-1"] {
		t.Fatal("the double-click must toggle the node")
	}
	tui.mu.Unlock()

	// The toggle reset the pair: two further presses toggle again.
	tui.mu.Lock()
	tui.treeAtClick(textX, textY)
	tui.treeAtClick(textX, textY)
	if tui.treeTab.expanded["wide-1"] {
		t.Fatal("the second double-click must toggle the node again")
	}
	tui.mu.Unlock()

	// A press at a different cell starts a new pair, not a
	// continuation.
	tui.mu.Lock()
	tui.treeAtClick(textX, textY)
	tui.treeAtClick(textX+1, textY)
	if tui.treeTab.expanded["wide-1"] {
		t.Fatal("a press at a different cell must not complete the pair")
	}
	// A press after the window expires the recorded pair.
	tui.lastTreePress = time.Now().Add(-2 * treeDoubleClickWindow)
	tui.treeAtClick(textX+1, textY)
	if tui.treeTab.expanded["wide-1"] {
		t.Fatal("a press after the window must not complete the pair")
	}
	tui.mu.Unlock()
}
