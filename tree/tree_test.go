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
	tr, err := New().Write("root", "input-1", TypeUser, AuthorUser, "hello")
	if err != nil {
		t.Fatal(err)
	}
	n, ok := tr.Node("input-1")
	if !ok {
		t.Fatal("node missing")
	}
	if n.Parent != "root" || n.Type != TypeUser || n.Author != AuthorUser || n.Content != "hello" {
		t.Fatalf("unexpected node: %+v", n)
	}
	if n.InsertTime.IsZero() {
		t.Fatal("insert time unset")
	}
}

func TestWriteValidation(t *testing.T) {
	base := New()
	if _, err := base.Write("root", "", TypeUser, AuthorUser, "x"); !errors.Is(err, ErrBadName) {
		t.Fatalf("want ErrBadName, got %v", err)
	}
	tr, err := base.Write("root", "a", TypeUser, AuthorUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Write("root", "a", TypeUser, AuthorUser, "y"); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("want ErrDuplicateName, got %v", err)
	}
	if _, err := tr.Write("missing", "b", TypeUser, AuthorUser, "x"); !errors.Is(err, ErrUnknownParent) {
		t.Fatalf("want ErrUnknownParent, got %v", err)
	}
	if _, err := tr.Write("root", "b", TypeUser, "robot", "x"); !errors.Is(err, ErrBadAuthor) {
		t.Fatalf("want ErrBadAuthor, got %v", err)
	}
}

func TestPathCopyingSharesUntouchedNodes(t *testing.T) {
	tr1, err := New().Write("root", "a", TypeUser, AuthorUser, "1")
	if err != nil {
		t.Fatal(err)
	}
	a1 := mustNode(t, tr1, "a")
	r1 := mustNode(t, tr1, "root")

	tr2, err := tr1.Write("root", "b", TypeUser, AuthorUser, "2")
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

	tr2, err := tr.Write("c", "d", Type("shell"), AuthorProgram, "d")
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
	tr3, err := tr2.Write("b", "e", Type("shell"), AuthorProgram, "e")
	if err != nil {
		t.Fatal(err)
	}
	if mustNode(t, tr2, "c") != mustNode(t, tr3, "c") {
		t.Fatal("off-path node must be shared by pointer")
	}
}

func TestMerge(t *testing.T) {
	base, err := New().Write("root", "shared", TypeUser, AuthorUser, "s")
	if err != nil {
		t.Fatal(err)
	}
	branch1, err := base.WriteAll(
		WriteOp{Parent: "shared", Name: "a", Type: TypeModel, Author: AuthorModel, Content: "a"},
		WriteOp{Parent: "root", Name: "z", Type: TypeUser, Author: AuthorUser, Content: "z"},
	)
	if err != nil {
		t.Fatal(err)
	}
	branch2, err := base.WriteAll(
		WriteOp{Parent: "shared", Name: "b", Type: TypeModel, Author: AuthorModel, Content: "b"},
		WriteOp{Parent: "b", Name: "c", Type: Type("shell"), Author: AuthorModel, Content: "c"},
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
	if b.Parent != "shared" || b.Type != TypeModel || b.Author != AuthorModel || b.Content != "b" {
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
	tr1, err := base.Write("root", "n", TypeUser, AuthorUser, "one")
	if err != nil {
		t.Fatal(err)
	}
	tr2, err := base.Write("root", "n", TypeUser, AuthorUser, "two")
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
		WriteOp{Parent: "root", Name: "response-1", Type: TypeModel, Author: AuthorModel, Content: "r"},
		WriteOp{Parent: "response-1", Name: "block-1", Type: Type("shell"), Author: AuthorModel, Content: "b"},
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
	base, err := New().Write("root", "dup", TypeUser, AuthorUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := base.WriteAll(
		WriteOp{Parent: "root", Name: "new", Type: TypeUser, Author: AuthorUser, Content: "n"},
		WriteOp{Parent: "root", Name: "dup", Type: TypeUser, Author: AuthorUser, Content: "d"},
		WriteOp{Parent: "dup", Name: "child", Type: TypeUser, Author: AuthorUser, Content: "c"},
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
		WriteOp{Parent: "root", Name: "a", Type: TypeUser, Author: AuthorUser, Content: "x", InsertTime: at},
		WriteOp{Parent: "root", Name: "b", Type: TypeUser, Author: AuthorUser, Content: "y"},
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
	tr2, err := tr.Write("root", "response-1", TypeModel, AuthorModel, "1")
	if err != nil {
		t.Fatal(err)
	}
	if got := tr2.AutoName("response"); got != "response-2" {
		t.Fatalf("second auto name = %q", got)
	}
}

func TestWriteAuto(t *testing.T) {
	tr := New()
	tr2, name, err := tr.WriteAuto("root", "response", TypeModel, AuthorModel, "r")
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
		WriteOp{Parent: "root", Name: "a", Type: TypeModel, Author: AuthorModel, Content: "a"},
		WriteOp{Parent: "a", Name: "b", Type: Type("shell"), Author: AuthorModel, Content: "b"},
		WriteOp{Parent: "b", Name: "c", Type: TypeBlockResult, Author: AuthorProgram, Content: "c"},
		WriteOp{Parent: "root", Name: "d", Type: TypeUser, Author: AuthorUser, Content: "d"},
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

func TestSubtreeToDepth(t *testing.T) {
	tr, err := New().WriteAll(
		WriteOp{Parent: "root", Name: "a", Type: TypeModel, Author: AuthorModel, Content: "a"},
		WriteOp{Parent: "a", Name: "b", Type: Type("shell"), Author: AuthorModel, Content: "b"},
		WriteOp{Parent: "b", Name: "c", Type: TypeBlockResult, Author: AuthorProgram, Content: "c"},
	)
	if err != nil {
		t.Fatal(err)
	}
	shallow := tr.SubtreeToDepth("root", 1)
	if len(shallow) != 2 || shallow[0].Name != "root" || shallow[1].Name != "a" {
		t.Fatalf("depth-1 subtree = %v", shallow)
	}
	if got := tr.SubtreeToDepth("root", 0); len(got) != 1 || got[0].Name != "root" {
		t.Fatalf("depth-0 subtree = %v", got)
	}
	full := tr.SubtreeToDepth("b", 5)
	if len(full) != 2 || full[0].Name != "b" || full[1].Name != "c" {
		t.Fatalf("subtree of b = %v", full)
	}
	if got := tr.SubtreeToDepth("missing", 2); got != nil {
		t.Fatal("missing subtree must be nil")
	}
}

func TestByTypeByAuthor(t *testing.T) {
	tr, err := New().WriteAll(
		WriteOp{Parent: "root", Name: "i", Type: TypeUser, Author: AuthorUser, Content: ""},
		WriteOp{Parent: "root", Name: "r", Type: TypeModel, Author: AuthorModel, Content: ""},
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

// TestCategoryAndEmoji verifies the category layer: every type maps to
// its category, Node.Category derives from the type, ByCategory selects
// the family, and every type and category carries a non-empty emoji.
// A block node's type is its block kind: an unknown kind derives to
// the block category and the fallback emoji, and a built-in kind
// carries its predefined emoji. Summary is a block kind, and attempt
// is a structure kind. See TheoryOfTree.
func TestCategoryAndEmoji(t *testing.T) {
	tr, err := New().WriteAll(
		WriteOp{Parent: "root", Name: "a", Type: TypeAttemptStart, Author: AuthorProgram, Content: "x"},
		WriteOp{Parent: "root", Name: "b", Type: TypeRequest, Author: AuthorProgram, Content: "y"},
		WriteOp{Parent: "root", Name: "c", Type: TypeUser, Author: AuthorUser, Content: "z"},
		WriteOp{Parent: "root", Name: "d", Type: Type("shell"), Author: AuthorModel, Content: "b"},
		WriteOp{Parent: "root", Name: "e", Type: TypeError, Author: AuthorProgram, Content: "e"},
		WriteOp{Parent: "root", Name: "f", Type: TypeSummary, Author: AuthorModel, Content: "s"},
		WriteOp{Parent: "root", Name: "g", Type: TypeAttempt, Author: AuthorProgram, Content: "a"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustNode(t, tr, "a").Category(); got != CategoryEvent {
		t.Fatalf("attempt-start category = %v, want event", got)
	}
	if got := mustNode(t, tr, "c").Category(); got != CategoryMessage {
		t.Fatalf("user category = %v, want message", got)
	}
	if got := mustNode(t, tr, "d").Category(); got != CategoryBlock {
		t.Fatalf("shell block category = %v, want block", got)
	}
	if got := mustNode(t, tr, "e").Category(); got != CategoryError {
		t.Fatalf("error category = %v, want error", got)
	}
	if got := mustNode(t, tr, "f").Category(); got != CategoryBlock {
		t.Fatalf("summary category = %v, want block", got)
	}
	if got := mustNode(t, tr, "g").Category(); got != CategoryStructure {
		t.Fatalf("attempt category = %v, want structure", got)
	}
	if got := len(tr.ByCategory(CategoryEvent)); got != 2 {
		t.Fatalf("ByCategory(event) = %d, want 2", got)
	}
	// A block node's type is its block kind: an unknown kind derives
	// to the block category with the fallback emoji, and a built-in
	// kind carries its predefined emoji.
	if got := Type("custom-kind").Category(); got != CategoryBlock {
		t.Fatalf("unknown kind category = %v, want block", got)
	}
	if got := Type("custom-kind").Emoji(); got != "🧱" {
		t.Fatalf("unknown kind emoji = %q, want the fallback glyph", got)
	}
	if got := Type("shell").Emoji(); got != "🐚" {
		t.Fatalf("shell kind emoji = %q, want the predefined glyph", got)
	}
	for _, n := range tr.Subtree("root") {
		if n.Type.Emoji() == "" {
			t.Fatalf("type %q carries no emoji", n.Type)
		}
		if n.Category().Emoji() == "" {
			t.Fatalf("category %q carries no emoji", n.Category())
		}
	}
}

func TestExtract(t *testing.T) {
	insertTime := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	tr, err := New().WriteAll(
		WriteOp{Parent: "root", Name: "a", Type: TypeModel, Author: AuthorModel, Content: "a", InsertTime: insertTime},
		WriteOp{Parent: "a", Name: "b", Type: Type("shell"), Author: AuthorModel, Content: "b"},
		WriteOp{Parent: "b", Name: "c", Type: TypeBlockResult, Author: AuthorProgram, Content: "c"},
		WriteOp{Parent: "root", Name: "d", Type: TypeUser, Author: AuthorUser, Content: "d"},
	)
	if err != nil {
		t.Fatal(err)
	}
	proj := tr.Extract(func(n *Node) bool {
		return n.Type != Type("shell") && n.Type != TypeBlockResult
	})
	if _, ok := proj.Node("b"); ok {
		t.Fatal("a pruned node must be absent from the projection")
	}
	if _, ok := proj.Node("c"); ok {
		t.Fatal("the descendant of a pruned node must be absent")
	}
	a, ok := proj.Node("a")
	if !ok || a.Parent != "root" {
		t.Fatalf("a matching node must keep its ancestor path: %+v", a)
	}
	if !a.InsertTime.Equal(insertTime) {
		t.Fatal("the projection must preserve insert times")
	}
	if _, ok := proj.Node("d"); !ok {
		t.Fatal("the other matching node must be kept")
	}
	if _, ok := tr.Node("b"); !ok {
		t.Fatal("the receiver must be unchanged")
	}

	// The ancestors above a selection stay as path context even when
	// they do not match.
	only := tr.Extract(func(n *Node) bool { return n.Type == TypeBlockResult })
	if _, ok := only.Node("c"); !ok {
		t.Fatal("the selected node must be kept")
	}
	if _, ok := only.Node("d"); ok {
		t.Fatal("a node off the selected path must be pruned")
	}
	if d := only.Depth("c"); d != 3 {
		t.Fatalf("the selected node must keep its ancestor chain, depth = %d", d)
	}

	// The projection is a tree in its own right: it accepts further writes.
	grown, err := proj.Write("a", "e", TypeSummary, AuthorModel, "e")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := grown.Node("e"); !ok {
		t.Fatal("the projection must accept further writes")
	}
}

func TestRenderOutline(t *testing.T) {
	tr, err := New().WriteAll(
		WriteOp{Parent: "root", Name: "a", Type: TypeModel, Author: AuthorModel, Content: "first line\nsecond line"},
		WriteOp{Parent: "a", Name: "b", Type: Type("shell"), Author: AuthorModel, Content: strings.Repeat("x", 50)},
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
	if !strings.Contains(lines[1], "a [model/model] first line") {
		t.Fatalf("a line: %q", lines[1])
	}
	if strings.Contains(lines[1], "second") {
		t.Fatal("preview must take the first line only")
	}
	if !strings.HasPrefix(lines[2], "    b [shell/model] ") {
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

func TestModify(t *testing.T) {
	insertTime := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
	tr, err := New().WriteAll(
		WriteOp{Parent: "root", Name: "plan-1", Type: TypePlan, Author: AuthorModel, Content: "do x", InsertTime: insertTime},
		WriteOp{Parent: "plan-1", Name: "step", Type: TypePlan, Author: AuthorModel, Content: "step"},
		WriteOp{Parent: "root", Name: "other", Type: TypeUser, Author: AuthorUser, Content: "other"},
	)
	if err != nil {
		t.Fatal(err)
	}

	modified, err := tr.Modify("plan-1", "do y")
	if err != nil {
		t.Fatal(err)
	}
	n := mustNode(t, modified, "plan-1")
	if n.Content != "do y" {
		t.Fatalf("content = %q, want %q", n.Content, "do y")
	}
	if n.Parent != "root" || n.Type != TypePlan || n.Author != AuthorModel {
		t.Fatalf("identity fields must be preserved: %+v", n)
	}
	if !n.InsertTime.Equal(insertTime) {
		t.Fatal("a rewrite must keep the node's chronology")
	}
	if kids := n.Children(); len(kids) != 1 || kids[0].Name != "step" {
		t.Fatal("children must be preserved")
	}
	if mustNode(t, modified, "step") != mustNode(t, tr, "step") {
		t.Fatal("the untouched subtree must be shared by pointer")
	}
	if mustNode(t, modified, "root") == mustNode(t, tr, "root") {
		t.Fatal("the path to the root must be copied")
	}
	if mustNode(t, modified, "other") != mustNode(t, tr, "other") {
		t.Fatal("an off-path node must be shared by pointer")
	}
	if mustNode(t, tr, "plan-1").Content != "do x" {
		t.Fatal("the original tree must be unchanged")
	}

	if _, err := tr.Modify("missing", "x"); !errors.Is(err, ErrUnknownNode) {
		t.Fatalf("want ErrUnknownNode, got %v", err)
	}
	if _, err := tr.Modify("root", "x"); !errors.Is(err, ErrBadName) {
		t.Fatalf("want ErrBadName for the root, got %v", err)
	}
}

func TestChildrenDefensiveCopy(t *testing.T) {
	tr, err := New().Write("root", "a", TypeUser, AuthorUser, "x")
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

func TestDelete(t *testing.T) {
	tr, err := New().WriteAll(
		WriteOp{Parent: "root", Name: "response-1", Type: TypeModel, Author: AuthorModel, Content: "r"},
		WriteOp{Parent: "response-1", Name: "block-1", Type: Type("shell"), Author: AuthorModel, Content: "b"},
		WriteOp{Parent: "block-1", Name: "block-result-1", Type: TypeBlockResult, Author: AuthorProgram, Content: "ok"},
		WriteOp{Parent: "root", Name: "input-1", Type: TypeUser, Author: AuthorUser, Content: "i"},
	)
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := tr.Delete("block-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"block-1", "block-result-1"} {
		if _, ok := deleted.Node(name); ok {
			t.Fatalf("deleted node %q must be absent", name)
		}
		if _, ok := tr.Node(name); !ok {
			t.Fatalf("the receiver must be unchanged: %q missing", name)
		}
	}
	if mustNode(t, deleted, "input-1") != mustNode(t, tr, "input-1") {
		t.Fatal("an off-path node must be shared by pointer")
	}
	if mustNode(t, deleted, "response-1") == mustNode(t, tr, "response-1") {
		t.Fatal("the path to the root must be copied")
	}
	if mustNode(t, deleted, "root") == mustNode(t, tr, "root") {
		t.Fatal("the path to the root must be copied")
	}

	// A deleted name can be written again; the old tree keeps its own node.
	rewritten, err := deleted.Write("response-1", "block-1", Type("shell"), AuthorModel, "b2")
	if err != nil {
		t.Fatal(err)
	}
	if n := mustNode(t, rewritten, "block-1"); n.Content != "b2" {
		t.Fatalf("the reused name must carry the new write: %q", n.Content)
	}
	if mustNode(t, tr, "block-1").Content != "b" {
		t.Fatal("the old tree must keep its own node")
	}

	if _, err := tr.Delete("missing"); !errors.Is(err, ErrUnknownNode) {
		t.Fatalf("want ErrUnknownNode, got %v", err)
	}
	if _, err := tr.Delete("root"); !errors.Is(err, ErrBadName) {
		t.Fatalf("want ErrBadName for the root, got %v", err)
	}
}
