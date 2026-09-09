package pipeline

import (
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline/codetypes"
	"github.com/reusee/tai/tree"
)

func TestSystemPromptPlan(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
	).Call(func(
		prompt SystemPrompt,
	) {
		s := string(prompt)
		if !strings.Contains(s, "Plan-Op Block Kind") {
			t.Fatal("system prompt must include the plan-op section in every codes session")
		}
		if !strings.Contains(s, "You decide whether to plan") {
			t.Fatal("system prompt must let the model decide whether to plan")
		}
		if !strings.Contains(s, "needs no plan") {
			t.Fatal("system prompt must state that a simple task needs no plan")
		}
		if !strings.Contains(s, "the flow then ends") {
			t.Fatal("system prompt must state that marking the plan root done ends the flow")
		}
	})
}

// TestPendingPlanEntries verifies the pending-entry collector behind
// the plan feedback: every pending entry is returned in depth-first
// insertion order, done, deleted, and aborted entries are skipped, a
// parent surfaces when all its plan-entry children are resolved, a
// done root reports complete, and a root without entries reports
// empty. See TheoryOfPlan.
func TestPendingPlanEntries(t *testing.T) {
	build := func(t *testing.T) (*tree.Tree, string) {
		tr, err := tree.New().Write("root", planRootNameOf("root"), tree.TypePlan, tree.AuthorProgram, "objective")
		if err != nil {
			t.Fatal(err)
		}
		return tr, planRootNameOf("root")
	}
	markDone := func(tr *tree.Tree, parent string) *tree.Tree {
		next, _, err := tr.WriteAuto(parent, "done", tree.TypeDone, tree.AuthorProgram, "ok")
		if err != nil {
			t.Fatal(err)
		}
		return next
	}
	names := func(entries []*tree.Node) []string {
		var out []string
		for _, e := range entries {
			out = append(out, e.Name)
		}
		return out
	}

	tr, root := build(t)
	next, err := tr.Write(root, "e1", tree.TypePlan, tree.AuthorModel, "first")
	if err != nil {
		t.Fatal(err)
	}
	tr = next
	next, err = tr.Write(root, "e2", tree.TypePlan, tree.AuthorModel, "second")
	if err != nil {
		t.Fatal(err)
	}
	tr = next

	// Every pending entry is returned in insertion order.
	entries, complete, empty := pendingPlanEntries(tr, root)
	if complete || empty {
		t.Fatalf("expected pending entries, got complete=%v empty=%v", complete, empty)
	}
	if got := names(entries); len(got) != 2 || got[0] != "e1" || got[1] != "e2" {
		t.Fatalf("expected [e1 e2], got %v", got)
	}

	// A done entry is skipped; the remaining entry still surfaces.
	tr = markDone(tr, "e1")
	entries, _, _ = pendingPlanEntries(tr, root)
	if got := names(entries); len(got) != 1 || got[0] != "e2" {
		t.Fatalf("expected [e2] after e1 done, got %v", got)
	}

	// A pending descendant surfaces before its parent.
	next, err = tr.Write("e2", "c1", tree.TypePlan, tree.AuthorModel, "child")
	if err != nil {
		t.Fatal(err)
	}
	tr = next
	entries, _, _ = pendingPlanEntries(tr, root)
	if got := names(entries); len(got) != 1 || got[0] != "c1" {
		t.Fatalf("expected descendant c1, got %v", got)
	}

	// With every child resolved, the parent surfaces for the model to
	// mark or refine.
	tr = markDone(tr, "c1")
	entries, _, _ = pendingPlanEntries(tr, root)
	if got := names(entries); len(got) != 1 || got[0] != "e2" {
		t.Fatalf("expected surfaced parent e2, got %v", got)
	}

	// The root surfaces when every entry is resolved but not done.
	tr = markDone(tr, "e2")
	entries, _, _ = pendingPlanEntries(tr, root)
	if got := names(entries); len(got) != 1 || got[0] != root {
		t.Fatalf("expected surfaced root, got %v", got)
	}

	// A done root reports complete.
	tr = markDone(tr, root)
	_, complete, _ = pendingPlanEntries(tr, root)
	if !complete {
		t.Fatal("expected complete after the root is done")
	}

	// A plan root without entries reports empty so the model decomposes.
	fresh, freshRoot := build(t)
	_, _, empty = pendingPlanEntries(fresh, freshRoot)
	if !empty {
		t.Fatal("expected empty for a plan root without entries")
	}

	// An aborted entry is skipped.
	aborted, abortRoot := build(t)
	next, err = aborted.Write(abortRoot, "a1", tree.TypePlan, tree.AuthorModel, "aborted")
	if err != nil {
		t.Fatal(err)
	}
	aborted = next
	next, err = aborted.Write(abortRoot, "a2", tree.TypePlan, tree.AuthorModel, "kept")
	if err != nil {
		t.Fatal(err)
	}
	aborted = next
	next, err = aborted.Abort("a1", tree.AuthorProgram, "wrong direction")
	if err != nil {
		t.Fatal(err)
	}
	aborted = next
	entries, _, _ = pendingPlanEntries(aborted, abortRoot)
	if got := names(entries); len(got) != 1 || got[0] != "a2" {
		t.Fatalf("expected [a2] with a1 aborted, got %v", got)
	}
}

func TestApplyPlanOpsAtomic(t *testing.T) {
	tr, err := tree.New().Write("root", planRootNameOf("root"), tree.TypePlan, tree.AuthorProgram, "objective")
	if err != nil {
		t.Fatal(err)
	}
	pctx := &components.ProcessContext{
		SessionTree:   tr,
		SessionParent: "root",
		Blocks: []blocks.Block{
			{Kind: "plan-op", Attributes: map[string]string{"op": "add", "name": "e1"}, Body: "first"},
			{Kind: "plan-op", Attributes: map[string]string{"op": "done", "name": "missing"}, Body: "note"},
			{Kind: "plan-op", Attributes: map[string]string{"op": "add", "name": "e2"}, Body: "second"},
		},
	}
	result := applyPlanOps(pctx)
	// The invalid done op discards the whole batch: no entry exists and
	// the input tree is returned unchanged.
	if _, ok := result.Tree.Node("e1"); ok {
		t.Fatal("the atomic batch must discard valid operations alongside the invalid one")
	}
	if _, ok := result.Tree.Node("e2"); ok {
		t.Fatal("the atomic batch must discard valid operations alongside the invalid one")
	}
	if len(result.Parts) == 0 {
		t.Fatal("expected feedback parts naming the errors")
	}

	// A valid batch applies every operation and keeps the plan root.
	pctx.Blocks = []blocks.Block{
		{Kind: "plan-op", Attributes: map[string]string{"op": "add", "name": "e1"}, Body: "first"},
	}
	result = applyPlanOps(pctx)
	if _, ok := result.Tree.Node("e1"); !ok {
		t.Fatal("expected the entry to be added")
	}
	if node, ok := result.Tree.Node(planRootNameOf("root")); !ok || node.Type != tree.TypePlan {
		t.Fatal("expected the plan root to be ensured")
	}
}

// TestPlanRootPerLoop verifies the per-loop plan ownership: the plan
// root derives from the session parent and hangs under the loop node,
// a later loop starts with no plan at all before its first plan-op
// batch, and the earlier loop's plan stays pending and untouched. See
// TheoryOfPlan.
func TestPlanRootPerLoop(t *testing.T) {
	tr, err := tree.New().Write("root", "loop-1", tree.TypeLoop, tree.AuthorProgram, "goal loop 1")
	if err != nil {
		t.Fatal(err)
	}
	pctx := &components.ProcessContext{
		SessionTree:   tr,
		SessionParent: "loop-1",
		Blocks: []blocks.Block{
			{Kind: "plan-op", Attributes: map[string]string{"op": "add", "name": "e1"}, Body: "first"},
		},
	}
	result := applyPlanOps(pctx)
	planNode, ok := result.Tree.Node(planRootNameOf("loop-1"))
	if !ok || planNode.Type != tree.TypePlan {
		t.Fatalf("expected the loop's plan root: %+v", planNode)
	}
	if planNode.Parent != "loop-1" {
		t.Fatalf("plan root parent = %q, want %q (the loop node)", planNode.Parent, "loop-1")
	}

	// The next loop has no plan of its own before any plan-op block
	// arrives: no plan carries across loops.
	next, err := result.Tree.Write("root", "loop-2", tree.TypeLoop, tree.AuthorProgram, "goal loop 2")
	if err != nil {
		t.Fatal(err)
	}
	if planHasPendingWork(next, planRootNameOf("loop-2")) {
		t.Fatal("a fresh loop must start with no plan; no plan carries across loops")
	}
	// The earlier loop's plan stays pending and untouched.
	if !planHasPendingWork(next, planRootNameOf("loop-1")) {
		t.Fatal("the first loop's plan must stay pending and untouched")
	}

	// The second loop's own plan root is ensured when its first plan-op
	// batch arrives, and the entries it adds are its own pending work.
	nextPctx := &components.ProcessContext{
		SessionTree:   next,
		SessionParent: "loop-2",
		Blocks: []blocks.Block{
			{Kind: "plan-op", Attributes: map[string]string{"op": "add", "name": "fresh"}, Body: "second loop work"},
		},
	}
	result2 := applyPlanOps(nextPctx)
	if _, ok := result2.Tree.Node(planRootNameOf("loop-2")); !ok {
		t.Fatal("expected the second loop's own plan root")
	}
	if !planHasPendingWork(result2.Tree, planRootNameOf("loop-2")) {
		t.Fatal("the second loop's added entry must be its own pending work")
	}
}

func TestPlanFeedbackNotices(t *testing.T) {
	root := planRootNameOf("root")
	tr, err := tree.New().Write("root", root, tree.TypePlan, tree.AuthorProgram, "objective")
	if err != nil {
		t.Fatal(err)
	}
	// An empty plan yields no plan feedback: the model decides whether
	// to plan.
	parts, complete := planFeedback(tr, root, false)
	if complete || parts != nil {
		t.Fatalf("expected no plan feedback for an empty plan, got %d parts complete=%v", len(parts), complete)
	}

	// The surfaced root yields the root notice.
	next, err := tr.Write(root, "e1", tree.TypePlan, tree.AuthorModel, "only")
	if err != nil {
		t.Fatal(err)
	}
	tr = next
	marked, _, err := tr.WriteAuto("e1", "done", tree.TypeDone, tree.AuthorProgram, "ok")
	if err != nil {
		t.Fatal(err)
	}
	tr = marked
	parts, complete = planFeedback(tr, root, false)
	if complete {
		t.Fatal("a resolved-but-unmarked root is not complete")
	}
	if !strings.Contains(string(parts[0].(generators.Text)), "plan root") {
		t.Fatal("expected the surfaced-root notice")
	}

	// Several pending entries render as one multi-entry notice listing
	// every one of them.
	next, err = tr.Write(root, "e2", tree.TypePlan, tree.AuthorModel, "second")
	if err != nil {
		t.Fatal(err)
	}
	tr = next
	next, err = tr.Write(root, "e3", tree.TypePlan, tree.AuthorModel, "third")
	if err != nil {
		t.Fatal(err)
	}
	tr = next
	parts, complete = planFeedback(tr, root, false)
	if complete || len(parts) != 1 {
		t.Fatalf("expected one multi-entry part, got %d complete=%v", len(parts), complete)
	}
	text := string(parts[0].(generators.Text))
	if !strings.Contains(text, "Pending plan entries") ||
		!strings.Contains(text, "- e2:") || !strings.Contains(text, "- e3:") {
		t.Fatal("expected the multi-entry notice to list every pending entry")
	}

	// The completion notice fires exactly once: the second call with
	// notified=true returns no parts.
	done, _, err := tr.WriteAuto(root, "done", tree.TypeDone, tree.AuthorProgram, "ok")
	if err != nil {
		t.Fatal(err)
	}
	tr = done
	parts, complete = planFeedback(tr, root, false)
	if !complete || len(parts) != 1 {
		t.Fatalf("expected one completion part, got %d complete=%v", len(parts), complete)
	}
	parts, complete = planFeedback(tr, root, true)
	if !complete || parts != nil {
		t.Fatalf("expected no repeated completion part, got %d", len(parts))
	}
}

// TestPlanSoftDelete verifies the soft-delete semantics of the plan-op
// delete operation: the deleted mark is written under the entry and the
// entry stays in the tree, deleting an already-deleted entry is a
// no-op, the pending-entry search skips deleted entries, done
// validation counts deleted subtasks as resolved, and every operation
// other than delete is rejected on a deleted entry. See TheoryOfPlan.
func TestPlanSoftDelete(t *testing.T) {
	tr, err := tree.New().Write("root", planRootNameOf("root"), tree.TypePlan, tree.AuthorProgram, "objective")
	if err != nil {
		t.Fatal(err)
	}
	next, err := tr.Write(planRootNameOf("root"), "e1", tree.TypePlan, tree.AuthorModel, "first")
	if err != nil {
		t.Fatal(err)
	}
	tr = next

	tr, part, err := applyOnePlanOp(tr, blocks.Block{
		Kind:       "plan-op",
		Attributes: map[string]string{"op": "delete", "name": "e1"},
		Body:       "no longer needed",
	}, planRootNameOf("root"))
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := tr.Node("e1")
	if !ok {
		t.Fatal("soft delete must keep the entry node in the tree")
	}
	if planDeletedChild(entry) == nil {
		t.Fatal("expected a deleted mark under the entry")
	}
	if !strings.Contains(string(part.(generators.Text)), "e1") {
		t.Fatal("expected a confirmation part naming the entry")
	}

	after, _, err := applyOnePlanOp(tr, blocks.Block{
		Kind:       "plan-op",
		Attributes: map[string]string{"op": "delete", "name": "e1"},
	}, planRootNameOf("root"))
	if err != nil {
		t.Fatalf("delete on a deleted entry must be idempotent: %v", err)
	}
	if after != tr {
		t.Fatal("idempotent delete must not change the tree")
	}

	for _, op := range []string{"done", "edit", "reopen"} {
		_, _, err := applyOnePlanOp(tr, blocks.Block{
			Kind:       "plan-op",
			Attributes: map[string]string{"op": op, "name": "e1"},
			Body:       "note",
		}, planRootNameOf("root"))
		if err == nil || !strings.Contains(err.Error(), "is deleted") {
			t.Fatalf("op %s on a deleted entry must be rejected, got %v", op, err)
		}
	}

	_, _, err = applyOnePlanOp(tr, blocks.Block{
		Kind:       "plan-op",
		Attributes: map[string]string{"op": "add", "name": "e2", "parent": "e1"},
		Body:       "subtask",
	}, planRootNameOf("root"))
	if err == nil || !strings.Contains(err.Error(), "is deleted") {
		t.Fatalf("add under a deleted entry must be rejected, got %v", err)
	}

	_, _, err = applyOnePlanOp(tr, blocks.Block{
		Kind:       "plan-op",
		Attributes: map[string]string{"op": "add", "name": "e1"},
		Body:       "again",
	}, planRootNameOf("root"))
	if err == nil || !strings.Contains(err.Error(), "choose a new name") {
		t.Fatalf("reusing a deleted name must be rejected with guidance, got %v", err)
	}

	next, err = tr.Write(planRootNameOf("root"), "e2", tree.TypePlan, tree.AuthorModel, "second")
	if err != nil {
		t.Fatal(err)
	}
	tr = next
	next, err = tr.Write("e2", "c1", tree.TypePlan, tree.AuthorModel, "child")
	if err != nil {
		t.Fatal(err)
	}
	tr = next
	tr, _, err = applyOnePlanOp(tr, blocks.Block{
		Kind:       "plan-op",
		Attributes: map[string]string{"op": "delete", "name": "c1"},
		Body:       "dropped",
	}, planRootNameOf("root"))
	if err != nil {
		t.Fatal(err)
	}

	entries, _, _ := pendingPlanEntries(tr, planRootNameOf("root"))
	if len(entries) != 1 || entries[0].Name != "e2" {
		t.Fatalf("expected the deleted subtask to be skipped and e2 to surface, got %v", entries)
	}

	tr, _, err = applyOnePlanOp(tr, blocks.Block{
		Kind:       "plan-op",
		Attributes: map[string]string{"op": "done", "name": "e2"},
		Body:       "the only subtask is deleted",
	}, planRootNameOf("root"))
	if err != nil {
		t.Fatalf("a deleted subtask must count as resolved for done: %v", err)
	}

	entries, _, _ = pendingPlanEntries(tr, planRootNameOf("root"))
	if len(entries) != 1 || entries[0].Name != planRootNameOf("root") {
		t.Fatalf("expected the surfaced root, got %v", entries)
	}
}

func TestRunPlanDrivenRounds(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			switch callCount {
			case 1:
				return appendPhase("<<貞觀 plan-op:?op=add&name=e1\nDo the first thing.\n貞觀\n" +
					"<<乾亨 plan-op:?op=add&name=e2\nDo the second thing.\n乾亨\n" +
					"<<明夷 summary\n- planned the work\n明夷\n")
			case 2:
				return appendPhase("<<大有 plan-op:?op=done&name=e1\nFirst entry done.\n大有\n" +
					"<<泰臨 summary\n- first entry done\n泰臨\n")
			case 3:
				return appendPhase("<<既濟 plan-op:?op=done&name=e2\nSecond entry done.\n既濟\n" +
					"<<未濟 plan-op:?op=done&name=root-plan\nAll entries complete.\n未濟\n" +
					"<<復卦 summary\n- all entries done\n復卦\n")
			default:
				return appendPhase("<<復卦 summary\n- acknowledged completion\n復卦\n")
			}
		}

		result, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   components.ComponentSet{NewPlanOpComponent()},
			PhaseBuilder: phaseBuilder,
			PlanMode:     true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Four generations: decompose, first entry, second entry plus the
		// root mark, then the closing round after the completion notice.
		// The feedback lists every pending entry each round, so the model
		// could batch several entries into one round.
		if callCount != 4 {
			t.Fatalf("expected 4 generations, got %d", callCount)
		}

		var stateText strings.Builder
		for c := range result.FinalState.Contents() {
			for _, p := range c.Parts {
				if text, ok := p.(generators.Text); ok {
					stateText.WriteString(string(text))
				}
			}
		}
		s := stateText.String()
		if !strings.Contains(s, "Pending plan entries") ||
			!strings.Contains(s, "- e1:") || !strings.Contains(s, "- e2:") {
			t.Fatal("the plan feedback must provide the pending entries across rounds")
		}
		if !strings.Contains(s, "The plan is complete") {
			t.Fatal("expected the completion notice before the session ends")
		}

		// The plan root lives under the session root and carries its
		// done mark; every entry is done.
		planNode, ok := result.SessionTree.Node(planRootNameOf("root"))
		if !ok || planNode.Type != tree.TypePlan {
			t.Fatalf("plan root missing: %+v", planNode)
		}
		if planNode.Parent != "root" {
			t.Fatalf("plan root parent = %q, want %q", planNode.Parent, "root")
		}
		doneCount := 0
		for _, c := range planNode.Children() {
			if c.Type == tree.TypeDone {
				doneCount++
			}
		}
		if doneCount != 1 {
			t.Fatalf("expected the plan root's done mark, got %d", doneCount)
		}
	})
}
