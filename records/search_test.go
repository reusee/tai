package records

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/tree"
)

// TestSearchRecordNodes verifies the global record search: it spans every
// recorded session, matches content and metadata case-insensitively, reports
// the node rather than the substring, skips a node the session deleted,
// matches a rewritten node by its final content, and lists the most recent
// session first. See TheoryOfGlobalRecordSearch.
func TestSearchRecordNodes(t *testing.T) {
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
		stubGetDefaultGenerator,
		stubBuildGenerate,
	).Fork(
		func() DBPath {
			return DBPath(filepath.Join(t.TempDir(), "test.db"))
		},
		func() Enabled {
			return Enabled(true)
		},
	)
	recorder := scope.Get[*Recorder]()
	search := scope.Get[SearchRecordNodes]()

	// The older session: a kept node, a deletion, and a rewrite.
	recorder.StartSession("older")
	tr := tree.New().WithOpSink(recorder.Sink())
	var err error
	tr, err = tr.Write("root", "kept", tree.TypeModel, tree.AuthorModel, "the Widget needs a guard")
	if err != nil {
		t.Fatal(err)
	}
	tr, err = tr.Write("root", "gone", tree.TypeModel, tree.AuthorModel, "widget draft")
	if err != nil {
		t.Fatal(err)
	}
	tr, err = tr.Write("root", "rewritten", tree.TypeModel, tree.AuthorModel, "old widget text")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Modify("rewritten", "new text"); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Delete("gone"); err != nil {
		t.Fatal(err)
	}
	recorder.EndSession(nil)

	// The newer session matches through the node's name.
	recorder.StartSession("newer")
	tr = tree.New().WithOpSink(recorder.Sink())
	if _, err := tr.Write("root", "widget-plan", tree.TypePlan, tree.AuthorModel, "plan body"); err != nil {
		t.Fatal(err)
	}
	recorder.EndSession(nil)

	matches, err := search("WIDGET")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %+v", len(matches), matches)
	}
	newer, kept := matches[0], matches[1]
	if newer.Command != "newer" || newer.NodeName != "widget-plan" ||
		newer.ContentLine != -1 || newer.Preview != "plan body" {
		t.Fatalf("expected the newer session's metadata match first, got %+v", newer)
	}
	if kept.Command != "older" || kept.NodeName != "kept" ||
		kept.ContentLine != 0 || kept.Preview != "the Widget needs a guard" {
		t.Fatalf("expected the older session's content match second, got %+v", kept)
	}
	if newer.StartTime == "" || newer.SessionID == kept.SessionID {
		t.Fatalf("expected the sessions' identities, got %+v and %+v", newer, kept)
	}

	// An empty keyword searches nothing.
	none, err := search("  ")
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("an empty keyword must match nothing, got %d", len(none))
	}
}

// TestQueryRecord verifies the structured record query: the type filter
// narrows by full type, name segment, prefix, and single-colon spelling; the
// keyword filter matches content and metadata; the session filter selects one
// record and rejects an unknown id; content is opt-in; and a limit reports that
// it stopped the query. See TheoryOfRecordQuery.
func TestQueryRecord(t *testing.T) {
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
		stubGetDefaultGenerator,
		stubBuildGenerate,
	).Fork(
		func() DBPath {
			return DBPath(filepath.Join(t.TempDir(), "test.db"))
		},
		func() Enabled {
			return Enabled(true)
		},
	)
	recorder := scope.Get[*Recorder]()
	query := scope.Get[QueryRecord]()

	recorder.StartSession("older")
	tr := tree.New().WithOpSink(recorder.Sink())
	var err error
	tr, err = tr.Write("root", "summary-1", tree.TypeSummary, tree.AuthorModel, "- did the work")
	if err != nil {
		t.Fatal(err)
	}
	tr, err = tr.Write("root", "run-error-1", tree.TypeRunError, tree.AuthorProgram, "the build failed")
	if err != nil {
		t.Fatal(err)
	}
	recorder.EndSession(nil)

	recorder.StartSession("newer")
	tr = tree.New().WithOpSink(recorder.Sink())
	if _, err := tr.Write("root", "summary-2", tree.TypeSummary, tree.AuthorModel, "- second"); err != nil {
		t.Fatal(err)
	}
	recorder.EndSession(nil)

	// A full type filter selects one kind across sessions, most recent
	// session first.
	result, err := query(RecordQuery{Types: []string{"block::summary"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 {
		t.Fatalf("expected 2 summary nodes, got %+v", result.Nodes)
	}
	if result.Nodes[0].Command != "newer" || result.Nodes[1].Command != "older" {
		t.Fatalf("expected the most recent session first, got %+v", result.Nodes)
	}
	if len(result.Sessions) != 2 {
		t.Fatalf("expected 2 matched sessions, got %+v", result.Sessions)
	}

	// The name segment, the prefix, and the single-colon spelling select
	// the same kind.
	for _, typeFilter := range []string{"summary", "block", "block:summary"} {
		result, err = query(RecordQuery{Types: []string{typeFilter}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Nodes) != 2 {
			t.Fatalf("type filter %q: expected 2 nodes, got %+v", typeFilter, result.Nodes)
		}
	}

	// The keyword filter matches content, case-insensitively.
	result, err = query(RecordQuery{Keyword: "BUILD"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 1 || result.Nodes[0].Name != "run-error-1" {
		t.Fatalf("expected the run-error node, got %+v", result.Nodes)
	}

	// Content is opt-in.
	if result.Nodes[0].Content != "" {
		t.Fatalf("content must be opt-in, got %q", result.Nodes[0].Content)
	}
	result, err = query(RecordQuery{Keyword: "build", Content: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Nodes[0].Content != "the build failed" {
		t.Fatalf("expected the node content, got %q", result.Nodes[0].Content)
	}

	// The session filter selects one record; an unknown id is an error.
	sessionID := result.Nodes[0].SessionID
	result, err = query(RecordQuery{SessionID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 {
		t.Fatalf("expected the session's 2 nodes, got %+v", result.Nodes)
	}
	for _, n := range result.Nodes {
		if n.SessionID != sessionID {
			t.Fatalf("session filter leaked a node of session %d", n.SessionID)
		}
	}
	if _, err := query(RecordQuery{SessionID: sessionID + 100}); err == nil {
		t.Fatal("expected an error for an unknown record id")
	}

	// A limit that stops the query reports it; a limit equal to the match
	// count does not.
	result, err = query(RecordQuery{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 1 || !result.Truncated {
		t.Fatalf("expected one node and a truncation note, got %+v", result)
	}
	result, err = query(RecordQuery{Types: []string{"block::summary"}, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 || result.Truncated {
		t.Fatalf("expected the complete 2 nodes, got %+v", result.Nodes)
	}
}

// TestParseRecordQuery verifies the query-record parameter parsing: the
// documented keys map onto the query, the session-tree parent parameter is
// ignored, an empty header queries everything, and an unknown key or a
// malformed value is rejected. See TheoryOfRecordQuery.
func TestParseRecordQuery(t *testing.T) {
	query, err := ParseRecordQuery(map[string]string{
		"session": "12",
		"keyword": "widget guard",
		"types":   "event, block::summary",
		"authors": "model",
		"names":   "attempt-1,attempt-2",
		"limit":   "20",
		"content": "true",
		"parent":  "root",
	})
	if err != nil {
		t.Fatal(err)
	}
	if query.SessionID != 12 || query.Keyword != "widget guard" ||
		query.Limit != 20 || !query.Content {
		t.Fatalf("unexpected query: %+v", query)
	}
	if len(query.Types) != 2 || query.Types[1] != "block::summary" {
		t.Fatalf("unexpected types: %+v", query.Types)
	}
	if len(query.Authors) != 1 || query.Authors[0] != "model" {
		t.Fatalf("unexpected authors: %+v", query.Authors)
	}
	if len(query.Names) != 2 || query.Names[0] != "attempt-1" {
		t.Fatalf("unexpected names: %+v", query.Names)
	}

	// An empty header queries everything.
	empty, err := ParseRecordQuery(nil)
	if err != nil {
		t.Fatal(err)
	}
	if empty.SessionID != 0 || empty.Keyword != "" || len(empty.Types) != 0 ||
		len(empty.Authors) != 0 || len(empty.Names) != 0 ||
		empty.Limit != 0 || empty.Content {
		t.Fatalf("expected the zero query, got %+v", empty)
	}

	// An unknown parameter is rejected with the accepted list.
	_, err = ParseRecordQuery(map[string]string{"type": "event"})
	if err == nil {
		t.Fatal("expected an error for an unknown parameter")
	}
	if !strings.Contains(err.Error(), "names") {
		t.Fatalf("the error must name the accepted parameters, got %v", err)
	}

	// A malformed value is rejected.
	if _, err := ParseRecordQuery(map[string]string{"session": "zero"}); err == nil {
		t.Fatal("expected an error for a malformed record id")
	}
	if _, err := ParseRecordQuery(map[string]string{"limit": "-1"}); err == nil {
		t.Fatal("expected an error for a negative limit")
	}
}
