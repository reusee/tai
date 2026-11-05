package records

import (
	"path/filepath"
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
