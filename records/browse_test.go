package records

import (
	"path/filepath"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/tree"
)

// TestSessionBrowsing verifies the browsing data API: the session list
// carries every session, most recent first, with its operation count, and
// the tree load reconstructs the recorded operation stream. A session
// that recorded no operations has no tree and reports an error instead.
// See TheoryOfSessionBrowsing.
func TestSessionBrowsing(t *testing.T) {
	dscope.New(
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
	).Call(func(recorder *Recorder, list ListSessionInfos, load LoadSessionTree) {
		recorder.StartSession("first")
		tr := tree.New().WithOpSink(recorder.Sink())
		var err error
		tr, err = tr.Write("root", "user-1", tree.TypeUser, tree.AuthorUser, "hello")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tr.Write("root", "model-1", tree.TypeModel, tree.AuthorModel, "world"); err != nil {
			t.Fatal(err)
		}
		recorder.EndSession(nil)
		recorder.StartSession("second")
		recorder.EndSession(nil)

		infos, err := list()
		if err != nil {
			t.Fatal(err)
		}
		if len(infos) != 2 {
			t.Fatalf("expected 2 sessions, got %d", len(infos))
		}
		if infos[0].Command != "second" || infos[1].Command != "first" {
			t.Fatalf("expected the most recent session first, got %+v", infos)
		}
		if infos[1].OpCount != 2 {
			t.Fatalf("expected 2 operations recorded, got %d", infos[1].OpCount)
		}
		if infos[1].StartTime == "" || infos[1].Status != "success" {
			t.Fatalf("expected the session's metadata, got %+v", infos[1])
		}

		replayed, err := load(infos[1].ID)
		if err != nil {
			t.Fatal(err)
		}
		user, ok := replayed.Node("user-1")
		if !ok || user.Content != "hello" || user.Type != tree.TypeUser {
			t.Fatalf("expected the recorded user node, got %+v", user)
		}
		model, ok := replayed.Node("model-1")
		if !ok || model.Content != "world" {
			t.Fatalf("expected the recorded model node, got %+v", model)
		}

		if _, err := load(infos[0].ID); err == nil {
			t.Fatal("expected an error for a session without recorded operations")
		}
	})
}
