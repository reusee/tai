package tree

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewTreeRootOnly(t *testing.T) {
	tr := New()
	sub := tr.Subtree("root")
	if len(sub) != 1 || sub[0].Name != "root" {
		t.Fatalf("expected single root node, got %d", len(sub))
	}
	if tr.Root().Parent != "" {
		t.Fatal("root must have no parent")
	}
}

func TestWriteBasic(t *testing.T) {
	tr, err := New().Write("root", "input-1", TypeInput, AuthorUser, "hello")
	if err != nil {
		t.Fatal(err)
	}
	n, ok := tr.Node("input-1")
	if !ok {
		t.Fatal("node missing")
	}
	if n.Parent != "root" || n.Type != TypeInput || n.Author != AuthorUser || n.Content != "hello" {
		t.Fatalf("unexpected node: %+v", n)
	}
	if n.InsertTime.IsZero() {
		t.Fatal("insert time unset")
	}
}

func TestWriteValidation(t *testing.T) {
	base := New()
	if _, err := base.Write("root", "", TypeInput, AuthorUser, "x"); !errors.Is(err, ErrBadName) {
		t.Fatalf("want ErrBadName, got %v", err)
	}
	tr, err := base.Write("root", "a", TypeInput, AuthorUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Write("root", "a", TypeInput, AuthorUser, "y"); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("want ErrDuplicateName, got %v", err)
	}
	if _, err := tr.Write("missing", "b", TypeInput, AuthorUser, "x"); !errors.Is(err, ErrUnknownParent) {
		t.Fatalf("want ErrUnknownParent, got %v", err)
	}
	if _, err := tr.Write("root", "b", TypeInput, "robot", "x"); !errors.Is(err, ErrBadAuthor) {
		t.Fatalf("want ErrBadAuthor, got %v", err)
	}
}

func TestPathCopyingSharesUntouchedNodes(t *testing.T) {
	tr1, err := New().Write("root", "a", TypeInput, AuthorUser, "1")
	if err != nil {
		t.Fatal(err)
	}
	a1 := mustNode(t, tr1, "a")
	r1 := mustNode(t, tr1, "root")

	tr2, err := tr1.Write("root", "b", TypeInput, AuthorUser, "2")
	if err != nil {
		t.Fatal(err)
	}
	a2 := mustNode(t, tr2, "a")
	r2 := mustNode(t, tr2, "root")

	if a1 != a2 {
		t.Fatal("untouched node must be shared by pointer")
	}
	if r1 == r2 {
		t.Fatal("root must be copied on write")
	}
	if len(r1.children) != 1 || r1.children[0].Name != "a" {
		t.Fatalf("old tree must be unchanged, children: %d", len(r1.children))
	}
	if len(r2.children) != 2 {
		t.Fatalf("new tree must carry both children, got %d", len(r2.children))
	}
}

func TestPathCopyingDeepPath(t *testing.T) {
	tr, err := New().WriteAll(
		WriteOp{Parent: "root", Name: "a", Type: TypePlan, Author: AuthorModel, Content: "a"},
		WriteOp{Parent: "a", Name: "b", Type: TypePlan, Author: AuthorModel, Content: "b"},
		WriteOp{Parent: "b", Name: "c", Type: TypePlan, Author: AuthorModel, Content: "c"},
	)
	if err != nil {
		t.Fatal(err)
	}

	tr2, err := tr.Write("c", "d", TypeBlock, AuthorProgram, "d")
	if err != nil {
		t.Fatal(err)
	}
	// Every node on the parent-to-root path must be copied.
	for _, name := range []string{"c", "b", "a", "root"} {
		if mustNode(t, tr, name) == mustNode(t, tr2, name) {
			t.Fatalf("path node %q must be copied", name)
		}
	}

	// A sibling write off the c subtree shares the c node.
	tr3, err := tr2.Write("b", "e", TypeBlock, AuthorProgram, "e")
	if err != nil {
		t.Fatal(err)
	}
	if mustNode(t, tr2, "c") != mustNode(t, tr3, "c") {
		t.Fatal("off-path node must be shared by pointer")
	}
}

func TestMerge(t *testing.T) {
	base, err := New().Write("root", "shared", TypeInput, AuthorUser, "s")
	if err != nil {
		t.Fatal(err)
	}
	branch1, err := base.WriteAll(
		WriteOp{Parent: "shared", Name: "a", Type: TypeResponse, Author: AuthorModel, Content: "a"},
		WriteOp{Parent: "root", Name: "z", Type: TypeInput, Author: AuthorUser, Content: "z"},
	)
	if err != nil {
		t.Fatal(err)
	}
	branch2, err := base.WriteAll(
		WriteOp{Parent: "shared", Name: "b", Type: TypeResponse, Author: AuthorModel, Content: "b"},
		WriteOp{Parent: "b", Name: "c", Type: TypeBlock, Author: AuthorModel, Content: "c"},
	)
	if err != nil {
		t.Fatal(err)
	}
	bOriginal := mustNode(t, branch2, "b")

	merged, err := branch1.Merge(branch2)
	if err != nil {
		t.Fatal(err)
	}
	b := mustNode(t, merged, "b")
	if b.Parent != "shared" || b.Type != TypeResponse || b.Author != AuthorModel || b.Content != "b" {
		t.Fatalf("grafted node unexpected: %+v", b)
	}
	if !b.InsertTime.Equal(bOriginal.InsertTime) {
		t.Fatal("grafted node must keep its original insert time")
	}
	if _, ok := merged.Node("c"); !ok {
		t.Fatal("a nested node must graft under its already-grafted parent")
	}
	if _, ok := merged.Node("a"); !ok {
		t.Fatal("the receiver's nodes must survive")
	}
	if mustNode(t, merged, "z") != mustNode(t, branch1, "z") {
		t.Fatal("a node off every graft path must be shared by pointer")
	}
	if _, ok := branch1.Node("b"); ok {
		t.Fatal("the receiver must be unchanged")
	}
	if _, ok := branch2.Node("a"); ok {
		t.Fatal("the merged-in tree must be unchanged")
	}
	if same, err := branch1.Merge(nil); err != nil || same != branch1 {
		t.Fatal("merging nil must return the receiver unchanged")
	}
}

func TestMergeConflict(t *testing.T) {
	base := New()
	tr1, err := base.Write("root", "n", TypeInput, AuthorUser, "one")
	if err != nil {
		t.Fatal(err)
	}
	tr2, err := base.Write("root", "n", TypeInput, AuthorUser, "two")
	if err != nil {
		t.Fatal(err)
	}
	merged, err := tr1.Merge(tr2)
	if !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("want ErrDuplicateName, got %v", err)
	}
	if merged != nil {
		t.Fatal("a failed merge must return no tree")
	}
}

func mustNode(t *testing.T, tr *Tree, name string) *Node {
	t.Helper()
	n, ok := tr.Node(name)
	if !ok {
		t.Fatalf("node %q missing", name)
	}
	return n
}

func TestWriteAllAtomicSuccess(t *testing.T) {
	base := New()
	tr, err := base.WriteAll(
		WriteOp{Parent: "root", Name: "response-1", Type: TypeResponse, Author: AuthorModel, Content: "r"},
		WriteOp{Parent: "response-1", Name: "block-1", Type: TypeBlock, Author: AuthorModel, Content: "b"},
		WriteOp{Parent: "block-1", Name: "block-result-1", Type: TypeBlockResult, Author: AuthorProgram, Content: "ok"},
	)
	if err != nil {
		t.Fatal(err)
	}
	n, ok := tr.Node("block-result-1")
	if !ok || n.Parent != "block-1" {
		t.Fatalf("later op must reference an earlier write, got %+v", n)
	}
	if _, ok := base.Node("response-1"); ok {
		t.Fatal("receiver must be untouched by a batch")
	}
}

func TestWriteAllAtomicFailure(t *testing.T) {
	base, err := New().Write("root", "dup", TypeInput, AuthorUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := base.WriteAll(
		WriteOp{Parent: "root", Name: "new", Type: TypeInput, Author: AuthorUser, Content: "n"},
		WriteOp{Parent: "root", Name: "dup", Type: TypeInput, Author: AuthorUser, Content: "d"},
		WriteOp{Parent: "dup", Name: "child", Type: TypeInput, Author: AuthorUser, Content: "c"},
	)
	if err == nil {
		t.Fatal("expected duplicate-name error")
	}
	if tr != nil {
		t.Fatal("failed batch must return no tree")
	}
	if _, ok := base.Node("new"); ok {
		t.Fatal("failed batch must leave the receiver untouched")
	}
}

func TestWriteOpInsertTime(t *testing.T) {
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	tr, err := New().WriteAll(
		WriteOp{Parent: "root", Name: "a", Type: TypeInput, Author: AuthorUser, Content: "x", InsertTime: at},
		WriteOp{Parent: "root", Name: "b", Type: TypeInput, Author: AuthorUser, Content: "y"},
	)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := tr.Node("a")
	if !a.InsertTime.Equal(at) {
		t.Fatalf("explicit insert time = %v, want %v", a.InsertTime, at)
	}
	b, _ := tr.Node("b")
	if b.InsertTime.IsZero() {
		t.Fatal("a zero insert time must take the current time")
	}
}

func TestAbort(t *testing.T) {
	tr, err := New().Write("root", "plan-1", TypePlan, AuthorModel, "do x")
	if err != nil {
		t.Fatal(err)
	}
	aborted, err := tr.Abort("plan-1", AuthorModel, "wrong direction")
	if err != nil {
		t.Fatal(err)
	}
	n, _ := aborted.Node("plan-1")
	if !n.IsAborted() {
		t.Fatal("plan should be aborted")
	}
	children := n.Children()
	if len(children) != 1 {
		t.Fatalf("abort writes one child, got %d", len(children))
	}
	c := children[0]
	if c.Type != TypeAbort || c.Author != AuthorModel || c.Content != "wrong direction" || c.Parent != "plan-1" {
		t.Fatalf("unexpected abort child: %+v", c)
	}
	// The original tree stays unmodified.
	old, _ := tr.Node("plan-1")
	if old.IsAborted() {
		t.Fatal("original tree must be unchanged")
	}
	if _, err := tr.Abort("missing", AuthorUser, "why"); !errors.Is(err, ErrUnknownParent) {
		t.Fatalf("want ErrUnknownParent, got %v", err)
	}
}

func TestAutoName(t *testing.T) {
	tr := New()
	if got := tr.AutoName("response"); got != "response-1" {
		t.Fatalf("first auto name = %q", got)
	}
	tr2, err := tr.Write("root", "response-1", TypeResponse, AuthorModel, "1")
	if err != nil {
		t.Fatal(err)
	}
	if got := tr2.AutoName("response"); got != "response-2" {
		t.Fatalf("second auto name = %q", got)
	}
}

func TestWriteAuto(t *testing.T) {
	tr := New()
	tr2, name, err := tr.WriteAuto("root", "response", TypeResponse, AuthorModel, "r")
	if err != nil {
		t.Fatal(err)
	}
	if name != "response-1" {
		t.Fatalf("name = %q", name)
	}
	if _, ok := tr2.Node(name); !ok {
		t.Fatal("node missing")
	}
}

func TestSubtreeAndDepth(t *testing.T) {
	tr, err := New().WriteAll(
		WriteOp{Parent: "root", Name: "a", Type: TypeResponse, Author: AuthorModel, Content: "a"},
		WriteOp{Parent: "a", Name: "b", Type: TypeBlock, Author: AuthorModel, Content: "b"},
		WriteOp{Parent: "b", Name: "c", Type: TypeBlockResult, Author: AuthorProgram, Content: "c"},
		WriteOp{Parent: "root", Name: "d", Type: TypeInput, Author: AuthorUser, Content: "d"},
	)
	if err != nil {
		t.Fatal(err)
	}
	sub := tr.Subtree("a")
	if len(sub) != 3 || sub[0].Name != "a" || sub[1].Name != "b" || sub[2].Name != "c" {
		t.Fatal("subtree must be depth-first and include the start node")
	}
	if got := tr.Subtree("missing"); got != nil {
		t.Fatal("missing subtree must be nil")
	}
	if d := tr.Depth("c"); d != 3 {
		t.Fatalf("depth of c = %d, want 3", d)
	}
	if d := tr.Depth("root"); d != 0 {
		t.Fatalf("depth of root = %d, want 0", d)
	}
	if d := tr.Depth("missing"); d != -1 {
		t.Fatalf("depth of missing = %d, want -1", d)
	}
}

func TestByTypeByAuthor(t *testing.T) {
	tr, err := New().WriteAll(
		WriteOp{Parent: "root", Name: "i", Type: TypeInput, Author: AuthorUser, Content: ""},
		WriteOp{Parent: "root", Name: "r", Type: TypeResponse, Author: AuthorModel, Content: ""},
		WriteOp{Parent: "r", Name: "s", Type: TypeSummary, Author: AuthorModel, Content: ""},
		WriteOp{Parent: "s", Name: "res", Type: TypeBlockResult, Author: AuthorProgram, Content: ""},
	)
	if err != nil {
		t.Fatal(err)
	}
	results := tr.ByType(TypeBlockResult)
	if len(results) != 1 || results[0].Name != "res" {
		t.Fatalf("unexpected ByType result: %d entries", len(results))
	}
	if got := tr.ByAuthor(AuthorModel); len(got) != 2 {
		t.Fatalf("ByAuthor model = %d, want 2", len(got))
	}
	if got := tr.ByAuthor(AuthorProgram); len(got) != 1 || got[0].Name != "res" {
		t.Fatal("unexpected ByAuthor program result")
	}
	// root, r, and res all contain "r".
	if got := tr.Filter(func(n *Node) bool { return strings.Contains(n.Name, "r") }); len(got) != 3 {
		t.Fatalf("Filter = %d, want 3", len(got))
	}
}

func TestRenderOutline(t *testing.T) {
	tr, err := New().WriteAll(
		WriteOp{Parent: "root", Name: "a", Type: TypeResponse, Author: AuthorModel, Content: "first line\nsecond line"},
		WriteOp{Parent: "a", Name: "b", Type: TypeBlock, Author: AuthorModel, Content: strings.Repeat("x", 50)},
	)
	if err != nil {
		t.Fatal(err)
	}
	aborted, err := tr.Abort("b", AuthorProgram, "no")
	if err != nil {
		t.Fatal(err)
	}
	out := aborted.RenderOutline(10)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 { // root, a, b, b-abort-1
		t.Fatalf("outline lines = %d: %q", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "root [root/]") {
		t.Fatalf("root line: %q", lines[0])
	}
	if !strings.Contains(lines[1], "a [response/model] first line") {
		t.Fatalf("a line: %q", lines[1])
	}
	if strings.Contains(lines[1], "second") {
		t.Fatal("preview must take the first line only")
	}
	if !strings.HasPrefix(lines[2], "    b [block/model] ") {
		t.Fatalf("b line: %q", lines[2])
	}
	if !strings.Contains(lines[2], strings.Repeat("x", 10)+"…") {
		t.Fatalf("preview must truncate at 10 runes: %q", lines[2])
	}
	// The aborted marker sits on the aborted node's own outline line, not
	// on its abort child's line, whose [abort] type is self-evident.
	if !strings.Contains(lines[2], "(aborted)") {
		t.Fatalf("aborted node must be marked on its own line: %q", lines[2])
	}
	if !strings.HasPrefix(lines[3], "      b-abort-1 [abort/program] no") {
		t.Fatalf("abort child line: %q", lines[3])
	}
}

func TestPreviewRunes(t *testing.T) {
	if got := PreviewRunes("hello\nworld", 0); got != "hello" {
		t.Fatalf("first line = %q", got)
	}
	if got := PreviewRunes("hello", 0); got != "hello" {
		t.Fatalf("no truncation = %q", got)
	}
	if got := PreviewRunes("你好世界", 2); got != "你好…" {
		t.Fatalf("rune truncation = %q", got)
	}
}

func TestChildrenDefensiveCopy(t *testing.T) {
	tr, err := New().Write("root", "a", TypeInput, AuthorUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	root, _ := tr.Node("root")
	kids := root.Children()
	kids[0] = nil
	if root.Children()[0] == nil {
		t.Fatal("Children must return a defensive copy")
	}
}
