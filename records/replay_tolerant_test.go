package records

import (
	"path/filepath"
	"testing"

	"github.com/reusee/tai/tree"
)

// TestLoadSessionTreeRendersAnInconsistentStream reproduces the reported
// failure: a recorded stream holds a duplicate write, and the loader must
// return the tree that applied together with the skipped operation as the
// error, so the viewer renders the session instead of reporting that the
// record has no nodes. See tree.TheoryOfReplayTolerance.
func TestLoadSessionTreeRendersAnInconsistentStream(t *testing.T) {
	rec := Module{}.Recorder(DBPath(filepath.Join(t.TempDir(), "tai-tree.db")), Enabled(true))
	if rec == nil {
		t.Fatal("expected a recorder")
	}
	rec.StartSession("test")
	sink := rec.Sink()
	sink(tree.Op{Kind: tree.OpWrite, Parent: "root", Name: "api_error-1", Type: tree.TypeAPIError, Author: tree.AuthorProgram, Content: "first"})
	// The second write of the same name is what a diverged stream carries.
	sink(tree.Op{Kind: tree.OpWrite, Parent: "root", Name: "api_error-1", Type: tree.TypeAPIError, Author: tree.AuthorProgram, Content: "second"})
	sink(tree.Op{Kind: tree.OpWrite, Parent: "root", Name: "api_error-2", Type: tree.TypeAPIError, Author: tree.AuthorProgram, Content: "third"})
	id := rec.sessionID
	rec.EndSession(nil)
	if id == 0 {
		t.Fatal("expected a recorded session")
	}

	tr, err := Module{}.LoadSessionTree(rec)(id)
	if tr == nil {
		t.Fatal("expected a tree even though the stream held a duplicate write")
	}
	if err == nil {
		t.Fatal("expected the skipped operation to be reported beside the tree")
	}
	node, ok := tr.Node("api_error-1")
	if !ok || node.Content != "first" {
		t.Fatalf("expected the first write to stand, got ok=%v node=%+v", ok, node)
	}
	if _, ok := tr.Node("api_error-2"); !ok {
		t.Fatal("expected the operation after the skipped one to apply")
	}
}

// TestRecordQueryToleratesAnInconsistentStream verifies that the query and
// the global search reach the matches of a record whose stream holds an
// operation the tree cannot apply: skipping the record would hide recorded
// data, the same failure the viewer reported. See
// tree.TheoryOfReplayTolerance and TheoryOfGlobalRecordSearch.
func TestRecordQueryToleratesAnInconsistentStream(t *testing.T) {
	rec := Module{}.Recorder(DBPath(filepath.Join(t.TempDir(), "tai-tree.db")), Enabled(true))
	if rec == nil {
		t.Fatal("expected a recorder")
	}
	rec.StartSession("test")
	sink := rec.Sink()
	sink(tree.Op{Kind: tree.OpWrite, Parent: "root", Name: "user-1", Type: tree.TypeUser, Author: tree.AuthorUser, Content: "find me"})
	sink(tree.Op{Kind: tree.OpWrite, Parent: "root", Name: "user-1", Type: tree.TypeUser, Author: tree.AuthorUser, Content: "duplicate"})
	sink(tree.Op{Kind: tree.OpWrite, Parent: "root", Name: "model-1", Type: tree.TypeModel, Author: tree.AuthorModel, Content: "also find me"})
	rec.EndSession(nil)

	result, err := queryRecord(rec, RecordQuery{Keyword: "find me", Content: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 {
		t.Fatalf("expected the query to reach both matching nodes, got %d", len(result.Nodes))
	}
	matches, err := searchRecordNodes(rec, "find me")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected the search to reach both matching nodes, got %d", len(matches))
	}
}
