package tree

import (
	"strings"
	"testing"
)

// TestReplayTolerantAppliesEveryApplicableOp reproduces the record whose
// stream holds a duplicate write: the reconstruction must keep the first
// write, apply every later operation, and report the skipped one beside the
// tree. See TheoryOfReplayTolerance.
func TestReplayTolerantAppliesEveryApplicableOp(t *testing.T) {
	ops := []Op{
		{Kind: OpWrite, Parent: "root", Name: "user-1", Type: TypeUser, Author: AuthorUser, Content: "hello"},
		{Kind: OpWrite, Parent: "root", Name: "user-1", Type: TypeUser, Author: AuthorUser, Content: "duplicate"},
		{Kind: OpWrite, Parent: "user-1", Name: "model-1", Type: TypeModel, Author: AuthorModel, Content: "world"},
	}
	tr, problems := ReplayTolerant(ops)
	if len(problems) != 1 {
		t.Fatalf("expected one problem, got %d: %v", len(problems), problems)
	}
	if problems[0].Index != 1 {
		t.Fatalf("expected the duplicate write to be the reported problem, got %+v", problems[0])
	}
	if !strings.Contains(problems.Error(), `"user-1"`) {
		t.Fatalf("expected the note to name the duplicate node, got %q", problems.Error())
	}
	node, ok := tr.Node("user-1")
	if !ok || node.Content != "hello" {
		t.Fatalf("expected the first write to stand, got ok=%v node=%+v", ok, node)
	}
	if _, ok := tr.Node("model-1"); !ok {
		t.Fatal("expected the operation after the skipped one to apply")
	}
}

// TestReplayTolerantKeepsRootForAnUnusableFirstOp verifies that a stream whose
// first operation cannot apply still yields a renderable tree: a viewer shows
// the root and states the reason rather than rendering nothing.
// See TheoryOfReplayTolerance.
func TestReplayTolerantKeepsRootForAnUnusableFirstOp(t *testing.T) {
	tr, problems := ReplayTolerant([]Op{
		{Kind: OpWrite, Parent: "missing", Name: "a", Type: TypeUser, Author: AuthorUser},
	})
	if len(problems) != 1 {
		t.Fatalf("expected one problem, got %d: %v", len(problems), problems)
	}
	if tr == nil || tr.Root() == nil {
		t.Fatal("expected a tree carrying at least its root")
	}
}
