package tree

import "testing"

// opCollector returns an op slice and a sink appending to it.
func opCollector() (*[]Op, OpSink) {
	ops := &[]Op{}
	return ops, func(op Op) {
		*ops = append(*ops, op)
	}
}

func TestOpSinkRecordsWrites(t *testing.T) {
	ops, sink := opCollector()
	tr := New().WithOpSink(sink)
	tr, err := tr.Write("root", "a", TypeSystem, AuthorProgram, "hello")
	if err != nil {
		t.Fatal(err)
	}
	tr, name, err := tr.WriteAuto("a", "child", TypeUser, AuthorUser, "content")
	if err != nil {
		t.Fatal(err)
	}
	if len(*ops) != 2 {
		t.Fatalf("got %d operations, want 2", len(*ops))
	}
	first := (*ops)[0]
	if first.Kind != OpWrite || first.Parent != "root" || first.Name != "a" ||
		first.Type != TypeSystem || first.Author != AuthorProgram || first.Content != "hello" {
		t.Fatalf("unexpected operation: %+v", first)
	}
	node, ok := tr.Node("a")
	if !ok || node.InsertTime != first.Time {
		t.Fatal("write operation time does not match the node's insert time")
	}
	second := (*ops)[1]
	if second.Kind != OpWrite || second.Name != name || second.Parent != "a" ||
		second.Type != TypeUser || second.Author != AuthorUser || second.Content != "content" {
		t.Fatalf("unexpected auto operation: %+v", second)
	}
}

func TestOpSinkRecordsModifyAndDelete(t *testing.T) {
	ops, sink := opCollector()
	tr := New().WithOpSink(sink)
	tr, err := tr.Write("root", "a", TypePlan, AuthorModel, "old")
	if err != nil {
		t.Fatal(err)
	}
	tr, err = tr.Modify("a", "new")
	if err != nil {
		t.Fatal(err)
	}
	tr, err = tr.Delete("a")
	if err != nil {
		t.Fatal(err)
	}
	if len(*ops) != 3 {
		t.Fatalf("got %d operations, want 3", len(*ops))
	}
	modified := (*ops)[1]
	if modified.Kind != OpModify || modified.Name != "a" || modified.Parent != "root" ||
		modified.Type != TypePlan || modified.Content != "new" {
		t.Fatalf("unexpected modify operation: %+v", modified)
	}
	deleted := (*ops)[2]
	if deleted.Kind != OpDelete || deleted.Name != "a" || deleted.Parent != "root" ||
		deleted.Content != "new" {
		t.Fatalf("unexpected delete operation: %+v", deleted)
	}
}

func TestOpSinkEmitsNothingForFailedOperations(t *testing.T) {
	ops, sink := opCollector()
	tr := New().WithOpSink(sink)
	tr, err := tr.Write("root", "a", TypeUser, AuthorUser, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Write("missing", "b", TypeUser, AuthorUser, ""); err == nil {
		t.Fatal("a write to an unknown parent did not fail")
	}
	if _, err := tr.Write("root", "a", TypeUser, AuthorUser, ""); err == nil {
		t.Fatal("a duplicate write did not fail")
	}
	if _, err := tr.Modify("missing", "content"); err == nil {
		t.Fatal("a modify of a missing node did not fail")
	}
	if _, err := tr.WriteAll(
		WriteOp{Parent: "root", Name: "b", Type: TypeUser, Author: AuthorUser},
		WriteOp{Parent: "missing", Name: "c", Type: TypeUser, Author: AuthorUser},
	); err == nil {
		t.Fatal("a failed batch did not fail")
	}
	if len(*ops) != 1 {
		t.Fatalf("got %d operations, want 1", len(*ops))
	}
}

func TestOpSinkRecordsBatchAndKeepsReporting(t *testing.T) {
	ops, sink := opCollector()
	tr := New().WithOpSink(sink)
	tr, err := tr.WriteAll(
		WriteOp{Parent: "root", Name: "a", Type: TypePlan, Author: AuthorModel, Content: "one"},
		WriteOp{Parent: "a", Name: "b", Type: TypePlan, Author: AuthorModel, Content: "two"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(*ops) != 2 {
		t.Fatalf("got %d operations after the batch, want 2", len(*ops))
	}
	if (*ops)[0].Name != "a" || (*ops)[1].Name != "b" {
		t.Fatalf("batch operations are out of order: %+v", *ops)
	}
	if _, err := tr.Write("b", "c", TypePlan, AuthorModel, "three"); err != nil {
		t.Fatal(err)
	}
	if len(*ops) != 3 {
		t.Fatalf("a write after the batch did not report: got %d operations", len(*ops))
	}
}

func TestOpSinkRecordsMergeGrafts(t *testing.T) {
	ops, sink := opCollector()
	tr := New().WithOpSink(sink)
	tr, err := tr.Write("root", "a", TypePlan, AuthorModel, "one")
	if err != nil {
		t.Fatal(err)
	}

	other := New()
	other, err = other.Write("root", "a", TypePlan, AuthorModel, "one")
	if err != nil {
		t.Fatal(err)
	}
	other, err = other.Write("a", "b", TypePlan, AuthorModel, "two")
	if err != nil {
		t.Fatal(err)
	}

	merged, err := tr.Merge(other)
	if err != nil {
		t.Fatal(err)
	}
	if len(*ops) != 2 {
		t.Fatalf("got %d operations after the merge, want 2", len(*ops))
	}
	grafted := (*ops)[1]
	if grafted.Kind != OpWrite || grafted.Name != "b" || grafted.Parent != "a" ||
		grafted.Content != "two" {
		t.Fatalf("unexpected graft operation: %+v", grafted)
	}
	if _, err := merged.Merge(other); err != nil {
		t.Fatal(err)
	}
	if len(*ops) != 2 {
		t.Fatalf("an identical merge reported operations: got %d", len(*ops))
	}

	conflicting := New()
	conflicting, err = conflicting.Write("root", "a", TypePlan, AuthorModel, "different")
	if err != nil {
		t.Fatal(err)
	}
	conflicting, err = conflicting.Write("root", "z", TypePlan, AuthorModel, "extra")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := merged.Merge(conflicting); err == nil {
		t.Fatal("a conflicting merge did not fail")
	}
	if len(*ops) != 2 {
		t.Fatalf("a failed merge reported operations: got %d", len(*ops))
	}
}

func TestOpSinkRecordsAbortAsWrite(t *testing.T) {
	ops, sink := opCollector()
	tr := New().WithOpSink(sink)
	tr, err := tr.Write("root", "plan", TypePlan, AuthorModel, "the plan")
	if err != nil {
		t.Fatal(err)
	}
	tr, err = tr.Abort("plan", AuthorModel, "superseded")
	if err != nil {
		t.Fatal(err)
	}
	if len(*ops) != 2 {
		t.Fatalf("got %d operations, want 2", len(*ops))
	}
	abort := (*ops)[1]
	if abort.Kind != OpWrite || abort.Type != TypeAbort || abort.Parent != "plan" ||
		abort.Author != AuthorModel || abort.Content != "superseded" {
		t.Fatalf("unexpected abort operation: %+v", abort)
	}
	node, ok := tr.Node("plan")
	if !ok || !node.IsAborted() {
		t.Fatal("the aborted node is not marked aborted")
	}
}

func TestOpSinkSharedByDerivedVersions(t *testing.T) {
	ops, sink := opCollector()
	base := New()
	attached := base.WithOpSink(sink)

	old, err := base.Write("root", "old", TypeUser, AuthorUser, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(*ops) != 0 {
		t.Fatal("a tree without a sink reported an operation")
	}

	next, err := attached.Write("root", "a", TypeUser, AuthorUser, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.Write("a", "b", TypeUser, AuthorUser, ""); err != nil {
		t.Fatal(err)
	}
	if len(*ops) != 2 {
		t.Fatalf("derived versions did not share the sink: got %d operations", len(*ops))
	}

	if _, err := old.Write("root", "x", TypeUser, AuthorUser, ""); err != nil {
		t.Fatal(err)
	}
	if len(*ops) != 2 {
		t.Fatal("a pre-attachment version reported an operation")
	}
}

func TestOpSinkProjectionIsNotASession(t *testing.T) {
	ops, sink := opCollector()
	tr := New().WithOpSink(sink)
	tr, err := tr.Write("root", "a", TypeUser, AuthorUser, "keep")
	if err != nil {
		t.Fatal(err)
	}
	tr, err = tr.Write("root", "b", TypeUser, AuthorUser, "drop")
	if err != nil {
		t.Fatal(err)
	}
	projection := tr.Extract(func(n *Node) bool { return n.Name == "a" })
	if _, err := projection.Write("a", "child", TypeUser, AuthorUser, "extra"); err != nil {
		t.Fatal(err)
	}
	if len(*ops) != 2 {
		t.Fatalf("a projection reported an operation: got %d operations", len(*ops))
	}
}

func TestReplayRebuildsTree(t *testing.T) {
	ops, sink := opCollector()
	tr := New().WithOpSink(sink)
	tr, err := tr.Write("root", "a", TypePlan, AuthorModel, "one")
	if err != nil {
		t.Fatal(err)
	}
	tr, err = tr.Write("a", "b", TypeSystem, AuthorProgram, "two")
	if err != nil {
		t.Fatal(err)
	}
	tr, err = tr.Modify("a", "one revised")
	if err != nil {
		t.Fatal(err)
	}
	tr, err = tr.Delete("b")
	if err != nil {
		t.Fatal(err)
	}
	merged := New()
	merged, err = merged.Write("root", "c", TypeUser, AuthorUser, "three")
	if err != nil {
		t.Fatal(err)
	}
	tr, err = tr.Merge(merged)
	if err != nil {
		t.Fatal(err)
	}

	rebuilt, err := Replay(*ops)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.RenderOutline(0) != tr.RenderOutline(0) {
		t.Fatalf("rebuilt tree differs:\n%s\n---\n%s", rebuilt.RenderOutline(0), tr.RenderOutline(0))
	}
	node, ok := rebuilt.Node("a")
	if !ok || node.Content != "one revised" {
		t.Fatal("rebuilt tree lost the modification")
	}
	if _, ok := rebuilt.Node("b"); ok {
		t.Fatal("rebuilt tree kept a deleted node")
	}
}

func TestReplayRejectsInapplicableOp(t *testing.T) {
	if _, err := Replay([]Op{{Kind: OpWrite, Parent: "missing", Name: "a", Type: TypeUser, Author: AuthorUser}}); err == nil {
		t.Fatal("a write to an unknown parent did not fail")
	}
	if _, err := Replay([]Op{{Kind: OpModify, Name: "missing", Content: "x"}}); err == nil {
		t.Fatal("a modify of a missing node did not fail")
	}
	if _, err := Replay([]Op{{Kind: OpKind("bogus"), Name: "a"}}); err == nil {
		t.Fatal("an unknown operation kind did not fail")
	}
}
