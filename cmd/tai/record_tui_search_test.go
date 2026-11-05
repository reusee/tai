package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/reusee/tai/records"
	"github.com/reusee/tai/tree"
)

func TestRecordBrowserGlobalSearch(t *testing.T) {
	var out strings.Builder
	var ops []tree.WriteOp
	for i := 0; i < 15; i++ {
		ops = append(ops, tree.WriteOp{
			Parent:  "root",
			Name:    fmt.Sprintf("node-%d", i),
			Type:    tree.TypeUsage,
			Author:  tree.AuthorProgram,
			Content: fmt.Sprintf("event %d", i),
		})
	}
	ops = append(ops, tree.WriteOp{
		Parent:  "root",
		Name:    "needle",
		Type:    tree.TypeModel,
		Author:  tree.AuthorModel,
		Content: "carries the Widget keyword\nsecond line",
	})
	tr, err := tree.New().WriteAll(ops...)
	if err != nil {
		t.Fatal(err)
	}
	b := newRecordBrowserForTest(
		func() ([]records.SessionInfo, error) {
			return []records.SessionInfo{
				{ID: 2, Command: "go_module", StartTime: "2026-01-02T03:04:05Z", Status: "success", OpCount: 16},
			}, nil
		},
		func(int64) (*tree.Tree, error) { return tr, nil },
		0,
		&out,
	)
	// A short pane makes the tree overflow, so the jump has somewhere to
	// scroll, and the screen matches the pane's height.
	b.screen.Resize(80, 8)
	b.height = 8
	var asked string
	b.searchNodes = func(keyword string) ([]records.RecordSearchMatch, error) {
		asked = keyword
		return []records.RecordSearchMatch{{
			SessionID:   2,
			Command:     "go_module",
			StartTime:   "2026-01-02T03:04:05Z",
			NodeName:    "needle",
			NodeType:    tree.TypeModel,
			NodeAuthor:  tree.AuthorModel,
			ContentLine: 0,
			Preview:     "carries the Widget keyword",
		}}, nil
	}
	b.loadList()

	// The "/" key opens the keyword bar; a plain character edits the
	// keyword instead of running a navigation action.
	b.handleKey("/")
	if !b.searching {
		t.Fatal("expected the search bar to open")
	}
	for _, key := range []string{"w", "i", "d", "g", "e", "t"} {
		b.handleKey(key)
	}
	// A plain character that is also a TUI binding edits the keyword, and
	// the quit key typed there must not quit the browser.
	if quit := b.handleKey("q"); quit {
		t.Fatal("typing the quit key while searching must not quit")
	}
	if line := b.searchBar.Line(); line != "widgetq" {
		t.Fatalf("expected the typed keyword, got %q", line)
	}
	b.handleKey("backspace")
	b.handleKey("enter")
	if b.searching {
		t.Fatal("the search must close the keyword bar")
	}
	if asked != "widget" {
		t.Fatalf("expected the search for %q, got %q", "widget", asked)
	}
	if !b.showingMatches || len(b.matches) != 1 {
		t.Fatalf("expected one match, got %d", len(b.matches))
	}
	out.Reset()
	b.render()
	// The match row carries the session, the node's classification, and
	// the matched line as the preview. The List truncates the row at the
	// pane width, so the assertion covers the preview's visible prefix.
	for _, want := range []string{"count 1 / matches 1", "needle", "carries the Widget"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q in the match list, got %q", want, out.String())
		}
	}
	out.Reset()

	// Enter opens the match's record and jumps the tree to the node.
	b.handleKey("enter")
	if b.currentID != 2 {
		t.Fatalf("expected the match to open record 2, got %d", b.currentID)
	}
	if off := b.treeScroll().Offset; off == 0 {
		t.Fatal("expected the jump to scroll the tree to the matched node")
	}
	b.render()
	if !strings.Contains(out.String(), "count 1 / matches 1 / id 2") {
		t.Fatalf("expected the tree view's status, got %q", out.String())
	}
	out.Reset()

	// Esc steps back one level: the tree closes to the match list, and
	// the match list closes to the record list.
	b.handleKey("esc")
	if b.currentID != 0 || !b.showingMatches {
		t.Fatalf("expected the tree to close to the match list, got record %d matches %v",
			b.currentID, b.showingMatches)
	}
	b.render()
	if !strings.Contains(out.String(), "matches 1") {
		t.Fatalf("expected the match list after esc, got %q", out.String())
	}
	out.Reset()
	b.handleKey("esc")
	if b.showingMatches {
		t.Fatal("expected the match list to close to the record list")
	}
	b.render()
	// The record list's status carries the count alone: no match list is
	// the view and no record is open, so neither segment renders.
	for _, want := range []string{"count 1", "go_module"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q in the record list after esc, got %q", want, out.String())
		}
	}
}
