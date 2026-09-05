package main

import (
	"strings"
	"testing"
	"time"

	"github.com/clipperhouse/displaywidth"
	"github.com/reusee/tai/tree"
)

// treeWithFinishNode builds a tree whose only event node is a finish
// node, the minimal tree a setTree call consumes to clear the
// generating hint. See TheoryOfTreeTab.
func treeWithFinishNode(t *testing.T) *tree.Tree {
	t.Helper()
	tr, err := tree.New().Write("root", "finish-1", tree.TypeEvent, tree.AuthorProgram, "finish: stop")
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// treeWithMultilineNode builds a tree with one multi-line event node,
// the fixture the Tree tab's collapse tests use. See TheoryOfTreeTab.
func treeWithMultilineNode(t *testing.T) *tree.Tree {
	t.Helper()
	tr, err := tree.New().Write("root", "handoff-1", tree.TypeEvent, tree.AuthorProgram,
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
		tree.WriteOp{Parent: "root", Name: "attempt-start-1", Type: tree.TypeEvent, Author: tree.AuthorProgram, Content: "attempt 1 start (1/3)"},
		tree.WriteOp{Parent: "root", Name: "finish-1", Type: tree.TypeEvent, Author: tree.AuthorProgram, Content: "finish: stop"},
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
// with multi-line content renders one collapsed header line by default,
// expanding reveals the body lines, and a body row of an expanded node
// never folds it. The display width is wide enough for the header and
// the elapsed timer to stay on one line. See TheoryOfTreeTab.
func TestTreeNodeCollapsedByDefault(t *testing.T) {
	tui := newTUIForTest()
	tui.treeView = treeWithMultilineNode(t)
	tui.mu.Lock()
	defer tui.mu.Unlock()
	display := tui.treeDisplay(120, panelStyle.BaseBG)
	if len(display) != 1 {
		t.Fatalf("expected 1 collapsed row, got %d: %v", len(display), displayTexts(display))
	}
	if !strings.Contains(display[0].Text, "handoff-1") ||
		!strings.Contains(display[0].Text, "3 more lines") {
		t.Fatalf("unexpected collapsed header: %q", display[0].Text)
	}
	// Expanding reveals the header and the body lines.
	tui.toggleTreeNodeAtRow(0)
	display = tui.treeDisplay(120, panelStyle.BaseBG)
	if len(display) != 4 {
		t.Fatalf("expected 4 expanded rows (header + 3 body), got %d", len(display))
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
	// A body row of an expanded node never folds it.
	tui.toggleTreeNodeAtRow(2)
	display2 := tui.treeDisplay(120, panelStyle.BaseBG)
	if len(display2) != 4 {
		t.Fatalf("a body press must not fold the expanded node, got %d rows", len(display2))
	}
}

// TestTreeProjectionCycle verifies the projection cycling: the modes
// walk all, events, summary, model, program, user; each projection
// keeps the shown nodes' ancestors so the outline stays readable; and
// the tab label states the current projection. See TheoryOfTreeTab.
func TestTreeProjectionCycle(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "input-1", Type: tree.TypeInput, Author: tree.AuthorUser, Content: "task"},
		tree.WriteOp{Parent: "root", Name: "attempt-start-1", Type: tree.TypeEvent, Author: tree.AuthorProgram, Content: "attempt 1 start (1/3)"},
		tree.WriteOp{Parent: "root", Name: "completed-1", Type: tree.TypeEvent, Author: tree.AuthorProgram, Content: "attempt 1 complete"},
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
	for _, want := range []string{"attempt-start-1", "completed-1"} {
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
	for _, gone := range []string{"input-1"} {
		for _, line := range display {
			if strings.Contains(line.Text, gone) {
				t.Fatalf("%q must be pruned from the events projection, got %v", gone, display)
			}
		}
	}
	if tui.treeTabLabel() != "Tree (events)" {
		t.Fatalf("unexpected projection label: %q", tui.treeTabLabel())
	}

	// User projection: the input node plus its ancestors.
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
		if strings.Contains(line.Text, "input-1") {
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
	if len(display) != 4 {
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

// TestTreeProjectionKeepsAncestors verifies that a projection keeps the
// shown nodes' ancestors, so a summary node stays readable in its path
// context. See TheoryOfTreeTab.
func TestTreeProjectionAncestors(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "input-1", Type: tree.TypeInput, Author: tree.AuthorUser, Content: "task"},
		tree.WriteOp{Parent: "input-1", Name: "response-1", Type: tree.TypeResponse, Author: tree.AuthorModel, Content: "resp"},
		tree.WriteOp{Parent: "response-1", Name: "summary-1", Type: tree.TypeSummary, Author: tree.AuthorModel, Content: "sum"},
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
	for _, want := range []string{"input-1", "response-1", "summary-1"} {
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
	tr, err := tree.New().Write("root", "attempt-start-1", tree.TypeEvent, tree.AuthorProgram,
		"attempt 3 start (1/3)")
	if err != nil {
		t.Fatal(err)
	}
	node, _ := tr.Node("attempt-start-1")
	if header := treeHeaderText(node, false, displaywidth.Options{}, 0, 0); !strings.HasSuffix(header, eventJumpMarker) {
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
	tr, err := tree.New().Write("root", "attempt-start-1", tree.TypeEvent, tree.AuthorProgram,
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

// TestTreeHeaderAlignment verifies the per-level column alignment: at
// one indent level, every header pads its name to the level's widest
// visible name and its [type/author] to the level's widest meta, so
// the meta fragments and the content previews start at the same
// column. See TheoryOfTreeTab.
func TestTreeHeaderAlignment(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "a", Type: tree.TypeInput, Author: tree.AuthorUser, Content: "x"},
		tree.WriteOp{Parent: "root", Name: "longer-name", Type: tree.TypeEvent, Author: tree.AuthorProgram, Content: "y"},
	)
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr
	tui.mu.Lock()
	defer tui.mu.Unlock()
	display := tui.treeDisplay(120, panelStyle.BaseBG)
	if len(display) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(display), displayTexts(display))
	}
	// The meta fragments start at the level's widest name column.
	metaCol := strings.Index(display[0].Text, "[")
	if metaCol < 0 || strings.Index(display[1].Text, "[") != metaCol {
		t.Fatalf("expected aligned meta columns, got %q and %q", display[0].Text, display[1].Text)
	}
	// The content previews start at the same column: the meta start
	// plus the level's widest meta plus one separator.
	contentCol := strings.Index(display[0].Text, "x")
	if contentCol < 0 || strings.Index(display[1].Text, "y") != contentCol {
		t.Fatalf("expected aligned content columns, got %q and %q", display[0].Text, display[1].Text)
	}
}

// TestTreeLinesDoNotWrap verifies the no-wrap contract: a node whose
// header or body lines exceed the pane width renders one truncated
// display row per line, never wrapped rows, and expansion reveals one
// row per content line. See TheoryOfTreeTab.
func TestTreeLinesDoNotWrap(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().Write("root", "wide-1", tree.TypeEvent, tree.AuthorProgram,
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
	// Expanded: one truncated row per content line, never wrapped.
	tui.toggleTreeNodeAtRow(0)
	display = tui.treeDisplay(40, panelStyle.BaseBG)
	if len(display) != 3 {
		t.Fatalf("expected 3 expanded rows (header + 2 body), got %d", len(display))
	}
	for _, line := range display {
		if w := displaywidth.String(line.Text); w > 40 {
			t.Fatalf("a display row must not exceed the content width, got %d: %q", w, line.Text)
		}
	}
}

// TestTreeStatusColumnToggles verifies the status column's contract:
// an expandable node carries the fold control pinned to its first
// display row, a press on the control's cells toggles the node and
// scrolls the view to its first row, a single-line node carries no
// control, and the collapsed glyph precedes the expanded one. See
// TheoryOfTreeTab.
func TestTreeStatusColumnToggles(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "wide-1", Type: tree.TypeEvent, Author: tree.AuthorProgram, Content: "head\nbody"},
		tree.WriteOp{Parent: "root", Name: "plain-1", Type: tree.TypeEvent, Author: tree.AuthorProgram, Content: "one line"},
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
	display := tui.treeDisplay(treeContentWidth(true, box.Width()), panelStyle.BaseBG)
	rows := tui.treeControlRows(box, display, 0)
	if len(rows) != 1 || rows[0].name != "wide-1" {
		t.Fatalf("expected one control row on wide-1, got %+v", rows)
	}
	wide, _ := tui.treeView.Node("wide-1")
	if glyph := tui.treeNodeControls(wide)[0].Glyph; glyph != sectionGlyphCollapsed {
		t.Fatalf("expected the collapsed glyph, got %q", glyph)
	}
	tui.mu.Unlock()

	// A press on the control's cells toggles the node and scrolls the
	// view to its first row.
	tui.mu.Lock()
	consumed := tui.toggleTreeControlAtClick(box.Left, rows[0].row)
	expanded := tui.treeTab.expanded["wide-1"]
	offset := tui.scrolls[1].Offset
	tui.mu.Unlock()
	if !consumed || !expanded {
		t.Fatalf("the control press must toggle wide-1, got consumed=%v expanded=%v", consumed, expanded)
	}
	if offset != 0 {
		t.Fatalf("expanding a collapsed node must scroll to its first row, got offset %d", offset)
	}

	// A second press collapses the node again.
	tui.mu.Lock()
	consumed = tui.toggleTreeControlAtClick(box.Left, rows[0].row)
	collapsed := !tui.treeTab.expanded["wide-1"]
	tui.mu.Unlock()
	if !consumed || !collapsed {
		t.Fatalf("the second press must collapse wide-1, got consumed=%v collapsed=%v", consumed, collapsed)
	}
}

// TestTreeShowsAllLoops verifies that the display covers every goal
// loop: the walk starts at the tree root, so both loops' nodes render
// in the all projection, and the user projection keeps the matched
// nodes of every loop. See TheoryOfTreeTab.
func TestTreeShowsAllLoops(t *testing.T) {
	tui := newTUIForTest()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "loop-1", Type: tree.TypeLoop, Author: tree.AuthorProgram, Content: "loop one"},
		tree.WriteOp{Parent: "loop-1", Name: "input-1", Type: tree.TypeInput, Author: tree.AuthorUser, Content: "task one"},
		tree.WriteOp{Parent: "root", Name: "loop-2", Type: tree.TypeLoop, Author: tree.AuthorProgram, Content: "loop two"},
		tree.WriteOp{Parent: "loop-2", Name: "input-2", Type: tree.TypeInput, Author: tree.AuthorUser, Content: "task two"},
	)
	if err != nil {
		t.Fatal(err)
	}
	tui.treeView = tr
	tui.mu.Lock()
	display := tui.treeDisplay(120, panelStyle.BaseBG)
	tui.mu.Unlock()
	for _, want := range []string{"loop-1", "input-1", "loop-2", "input-2"} {
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
	for _, want := range []string{"input-1", "input-2"} {
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
