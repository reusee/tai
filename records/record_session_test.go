package records

import (
	"path/filepath"
	"testing"

	"github.com/reusee/tai/tree"
)

// TestRecorderSessionStartsWithFirstOperation pins the lazy session start:
// a run that records no operation leaves no session row, so the record
// list never shows an empty record that cannot be opened.
// See TheoryOfRecordedSessionLazyStart.
func TestRecorderSessionStartsWithFirstOperation(t *testing.T) {
	recorder := (Module{}).Recorder(
		DBPath(filepath.Join(t.TempDir(), "tai-tree.db")),
		Enabled(true),
	)
	if recorder == nil {
		t.Fatal("expected a recorder on a temporary database")
	}

	recorder.StartSession("goal")
	recorder.EndSession(nil)
	infos, err := sessionInfos(recorder, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 0 {
		t.Fatalf("a run that recorded nothing must leave no session row, got %+v", infos)
	}

	recorder.StartSession("goal")
	tr := tree.New().WithOpSink(recorder.Sink())
	if _, err := tr.Write("root", "op-1", tree.TypeUser, tree.AuthorUser, "hello"); err != nil {
		t.Fatal(err)
	}
	recorder.EndSession(nil)
	infos, err = sessionInfos(recorder, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected one session, got %+v", infos)
	}
	if infos[0].OpCount != 1 || infos[0].Status != "success" {
		t.Fatalf("expected one recorded operation and a closed session, got %+v", infos[0])
	}
}

// TestRecorderStopsAtFailedOperation pins the write-failure rule: a hole
// in the middle of the stream would make the whole record unreplayable,
// so a failed operation write truncates the session at the last recorded
// operation and the recorded prefix still replays. See
// TheoryOfRecordedSessionLazyStart.
func TestRecorderStopsAtFailedOperation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tai-tree.db")
	recorder := (Module{}).Recorder(DBPath(path), Enabled(true))
	if recorder == nil {
		t.Fatal("expected a recorder on a temporary database")
	}
	recorder.StartSession("goal")
	tr := tree.New().WithOpSink(recorder.Sink())
	var err error
	tr, err = tr.Write("root", "user-1", tree.TypeUser, tree.AuthorUser, "hello")
	if err != nil {
		t.Fatal(err)
	}
	// A closed database makes the next operation write fail.
	if err := recorder.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Write("user-1", "model-1", tree.TypeModel, tree.AuthorModel, "world"); err != nil {
		t.Fatal(err)
	}
	recorder.EndSession(nil)

	reader := (Module{}).Recorder(DBPath(path), Enabled(false))
	if reader == nil {
		t.Fatal("expected to reopen the database")
	}
	infos, err := sessionInfos(reader, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].OpCount != 1 {
		t.Fatalf("expected one session with the single recorded operation, got %+v", infos)
	}
	replayed, err := (Module{}).LoadSessionTree(reader)(infos[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := replayed.Node("user-1"); !ok {
		t.Fatal("expected the recorded user node in the replayed prefix")
	}
	if _, ok := replayed.Node("model-1"); ok {
		t.Fatal("the failed operation must not be recorded")
	}
}
