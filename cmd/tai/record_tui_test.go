package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/records"
	"github.com/reusee/tai/taiui"
	"github.com/reusee/tai/tree"
)

// newRecordBrowserForTest builds a browser over a fake session set; the
// terminal screen writes into output. See TheoryOfRecordBrowser.
func newRecordBrowserForTest(
	list records.ListSessionInfos,
	load records.LoadSessionTree,
	openSession int64,
	output *strings.Builder,
) *RecordBrowser {
	tabs := taiui.NewTabs(1)
	tabs.FocusTab(0)
	return &RecordBrowser{
		screen:         taiui.NewTerminalScreen(output, 80, 24),
		updateCh:       make(chan struct{}, 1),
		width:          80,
		height:         24,
		tabs:           tabs,
		listSessions:   list,
		loadTree:       load,
		openSession:    openSession,
		treePane:       newRecordTreePane(tabs, 80, 24),
		mouseReporting: true,
	}
}

// recordBrowserFixture builds a browser whose fake session set carries
// three records, most recent first: #3 and #2 replay a recorded tree, #1
// recorded no operations. See TheoryOfRecordBrowser.
func recordBrowserFixture(t *testing.T, output *strings.Builder) (*RecordBrowser, *tree.Tree) {
	t.Helper()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "attempt-1", Type: tree.TypeAttempt, Author: tree.AuthorProgram, Content: "attempt 1"},
		tree.WriteOp{Parent: "attempt-1", Name: "user-1", Type: tree.TypeUser, Author: tree.AuthorUser, Content: "make a plan"},
		tree.WriteOp{Parent: "attempt-1", Name: "model-1", Type: tree.TypeModel, Author: tree.AuthorModel, Content: "line one\nline two\nline three"},
	)
	if err != nil {
		t.Fatal(err)
	}
	browser := newRecordBrowserForTest(
		func() ([]records.SessionInfo, error) {
			return []records.SessionInfo{
				{ID: 3, Command: "go_module", StartTime: "2026-01-03T03:04:05Z", Status: "success", OpCount: 3},
				{ID: 2, Command: "next", StartTime: "2026-01-02T03:04:05Z", Status: "success", OpCount: 3},
				{ID: 1, Command: "ai", StartTime: "2026-01-01T03:04:05Z", Status: "error", OpCount: 0},
			}, nil
		},
		func(id int64) (*tree.Tree, error) {
			if id == 1 {
				return nil, fmt.Errorf("session %d has no recorded operations", id)
			}
			return tr, nil
		},
		0,
		output,
	)
	return browser, tr
}

// TestRecordBrowserListsAndOpensRecords verifies the browser's two views:
// the list shows every record and opens the selected one, the open view
// renders the record's tree with the shared Tree tab rendering and states
// the record in the navigation and the tab label, and esc returns to the
// list. See TheoryOfRecordBrowser.
func TestRecordBrowserListsAndOpensRecords(t *testing.T) {
	var out strings.Builder
	b, tr := recordBrowserFixture(t, &out)
	b.loadList()
	if b.err != "" {
		t.Fatal(b.err)
	}
	if len(b.infos) != 3 || b.selected != 0 {
		t.Fatalf("expected 3 records with the first selected, got %d selected %d", len(b.infos), b.selected)
	}
	b.render()
	rendered := out.String()
	for _, want := range []string{"go_module", "next", "ai", "Record (3)"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected %q in the list view, got %q", want, rendered)
		}
	}
	out.Reset()

	// Enter opens the selected record.
	b.activateCursor()
	if b.currentID != 3 {
		t.Fatalf("expected record 3 opened, got %d", b.currentID)
	}
	if b.treePane.treeView != tr {
		t.Fatal("expected the opened record's tree in the tree pane")
	}
	if b.nodeCursor != "attempt-1" {
		t.Fatalf("expected the cursor on the first node, got %q", b.nodeCursor)
	}
	b.render()
	rendered = out.String()
	for _, want := range []string{"records › #3", "Record #3", "make a plan"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected %q in the tree view, got %q", want, rendered)
		}
	}
	out.Reset()

	// Esc returns to the list.
	if quit := b.handleKey("esc"); quit {
		t.Fatal("esc must not quit the browser")
	}
	if b.currentID != 0 {
		t.Fatalf("expected the list view after esc, got record %d", b.currentID)
	}
	b.render()
	if !strings.Contains(out.String(), "go_module") {
		t.Fatalf("expected the record list after esc, got %q", out.String())
	}
}

// TestRecordBrowserListPress verifies the list's pointer contract: a
// single press selects the record under it without opening, and a second
// press at the same cell within the double-click window opens it, mapped
// through the same window the List renders with. See
// TheoryOfRecordBrowser.
func TestRecordBrowserListPress(t *testing.T) {
	var out strings.Builder
	b, _ := recordBrowserFixture(t, &out)
	b.loadList()
	box := b.listBox()
	// The second row holds record #2.
	b.listPress(box.Left+5, box.Top+1)
	if b.selected != 1 || b.currentID != 0 {
		t.Fatalf("expected the second record selected without opening, got selected %d current %d", b.selected, b.currentID)
	}
	b.listPress(box.Left+5, box.Top+1)
	if b.currentID != 2 {
		t.Fatalf("expected the double click to open record 2, got %d", b.currentID)
	}
	// A press outside the list's box is inert.
	b.backToList()
	b.listPress(box.Left+5, box.Bottom+5)
	if b.currentID != 0 || b.selected != 1 {
		t.Fatalf("a press outside the list must be inert, got selected %d current %d", b.selected, b.currentID)
	}
}

// TestRecordBrowserNavReturnsToList verifies the navigation bar: its
// drawn cells on the title row are the press target, and a press there
// returns an opened record to the list. See TheoryOfRecordBrowser.
func TestRecordBrowserNavReturnsToList(t *testing.T) {
	var out strings.Builder
	b, _ := recordBrowserFixture(t, &out)
	b.loadList()
	b.openRecord(2)
	if b.currentID != 2 {
		t.Fatalf("expected record 2 opened, got %d", b.currentID)
	}
	box := b.tabBox()
	x := box.Left + recordNavIndent + 3
	if !b.navHit(x, box.Top) {
		t.Fatal("expected the navigation bar's cells to be a hit")
	}
	if b.navHit(x, box.Top+1) {
		t.Fatal("a press below the title row must not hit the navigation")
	}
	b.handleMouseKey(fmt.Sprintf("mouse-left@%d,%d", x, box.Top))
	if b.currentID != 0 {
		t.Fatalf("expected the navigation press to return to the list, got record %d", b.currentID)
	}
}

// TestRecordBrowserTreeCursor verifies the keyboard's tree navigation:
// the cursor moves over the visible nodes, Enter toggles the node under
// it through the Tree tab's own expansion path, and the page and end keys
// land on the last node. See TheoryOfRecordBrowser.
func TestRecordBrowserTreeCursor(t *testing.T) {
	var out strings.Builder
	b, _ := recordBrowserFixture(t, &out)
	b.loadList()
	b.openRecord(2)

	if b.nodeCursor != "attempt-1" {
		t.Fatalf("expected the cursor on the first node, got %q", b.nodeCursor)
	}
	b.moveCursor(1)
	if b.nodeCursor != "user-1" {
		t.Fatalf("expected the cursor on the second node, got %q", b.nodeCursor)
	}
	b.moveCursor(-1)
	if b.nodeCursor != "attempt-1" {
		t.Fatalf("expected the cursor back on the first node, got %q", b.nodeCursor)
	}
	b.moveTo(true)
	if b.nodeCursor != "model-1" {
		t.Fatalf("expected the end key to land on the last node, got %q", b.nodeCursor)
	}

	// Enter toggles the node under the cursor: the model node has a
	// multi-line body, so the expansion is observable.
	b.activateCursor()
	if !b.treePane.treeTab.expanded["model-1"] {
		t.Fatal("expected Enter to expand the node under the cursor")
	}
	b.activateCursor()
	if b.treePane.treeTab.expanded["model-1"] {
		t.Fatal("expected Enter to collapse the node under the cursor")
	}

	// The expanded node renders through the shared tree display: its
	// body lines appear in the pane.
	b.activateCursor()
	box := b.tabBox()
	display := b.treePane.treeDisplay(treeContentWidth(box.Width()), panelStyle.FocusBG)
	body := ""
	for _, line := range display {
		body += line.Text + "\n"
	}
	for _, want := range []string{"line one", "line two", "line three"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in the shared tree display, got %q", want, body)
		}
	}
}

// TestRecordBrowserOpenFailureKeepsList verifies that a record without a
// tree reports the reason and keeps the list view, so a broken record
// never blanks the browser. See TheoryOfRecordBrowser.
func TestRecordBrowserOpenFailureKeepsList(t *testing.T) {
	var out strings.Builder
	b, _ := recordBrowserFixture(t, &out)
	b.loadList()
	// Record 1 has no recorded operations.
	b.selectRecord(2)
	b.activateCursor()
	if b.currentID != 0 {
		t.Fatalf("expected the failed open to keep the list view, got record %d", b.currentID)
	}
	if b.err == "" {
		t.Fatal("expected the failed open to report the reason")
	}
	b.render()
	if !strings.Contains(out.String(), "no recorded operations") {
		t.Fatalf("expected the error in the pane, got %q", out.String())
	}
}

// TestRecordBrowserEnabledFollowsAnalyze verifies the browser marker: the
// record command's browsing modes select the dedicated browser, and the
// analysis mode withdraws the marker so the analysis keeps the generation
// TUI. The marker is a provider of the command's own Analyze flag, so the
// decision follows the parsed flags. See TheoryOfRecordBrowser.
func TestRecordBrowserEnabledFollowsAnalyze(t *testing.T) {
	scope := RecordCommand.Scope(dscope.New(dscope.Methods(new(Module))...))
	if !bool(scope.Get[RecordBrowserEnabled]()) {
		t.Fatal("a browsing record command must select the browser")
	}
	analyzing := scope.Fork(func() records.Analyze { return true })
	if bool(analyzing.Get[RecordBrowserEnabled]()) {
		t.Fatal("the analysis mode must keep the generation TUI")
	}
}
