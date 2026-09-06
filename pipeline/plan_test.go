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

func TestNextPendingPlanEntry(t *testing.T) {
	build := func(t *testing.T) (*tree.Tree, string) {
		tr, err := tree.New().Write("root", planRootName, tree.TypePlan, tree.AuthorProgram, "objective")
		if err != nil {
			t.Fatal(err)
		}
		return tr, planRootName
	}
	markDone := func(tr *tree.Tree, parent string) *tree.Tree {
		next, _, err := tr.WriteAuto(parent, "done", tree.TypeDone, tree.AuthorProgram, "ok")
		if err != nil {
			t.Fatal(err)
		}
		return next
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

	// Depth-first order picks the first pending entry.
	name, _, complete, empty := nextPendingPlanEntry(tr, root)
	if complete || empty || name != "e1" {
		t.Fatalf("expected e1 first, got %q complete=%v empty=%v", name, complete, empty)
	}

	// A done entry is skipped.
	tr = markDone(tr, "e1")
	name, _, _, _ = nextPendingPlanEntry(tr, root)
	if name != "e2" {
		t.Fatalf("expected e2 after e1 done, got %q", name)
	}

	// A pending descendant is selected before its parent surfaces.
	next, err = tr.Write("e2", "c1", tree.TypePlan, tree.AuthorModel, "child")
	if err != nil {
		t.Fatal(err)
	}
	tr = next
	name, _, _, _ = nextPendingPlanEntry(tr, root)
	if name != "c1" {
		t.Fatalf("expected descendant c1, got %q", name)
	}

	// With every child resolved, the parent surfaces for the model to
	// mark or refine.
	tr = markDone(tr, "c1")
	name, _, _, _ = nextPendingPlanEntry(tr, root)
	if name != "e2" {
		t.Fatalf("expected surfaced parent e2, got %q", name)
	}

	// The root surfaces when every entry is resolved but not done.
	tr = markDone(tr, "e2")
	name, _, _, _ = nextPendingPlanEntry(tr, root)
	if name != root {
		t.Fatalf("expected surfaced root, got %q", name)
	}

	// A done root reports complete.
	tr = markDone(tr, root)
	_, _, complete, _ = nextPendingPlanEntry(tr, root)
	if !complete {
		t.Fatal("expected complete after the root is done")
	}

	// A plan root without entries reports empty so the model decomposes.
	fresh, freshRoot := build(t)
	_, _, _, empty = nextPendingPlanEntry(fresh, freshRoot)
	if !empty {
		t.Fatal("expected empty for a plan root without entries")
	}
}

func TestApplyPlanOpsAtomic(t *testing.T) {
	tr, err := tree.New().Write("root", planRootName, tree.TypePlan, tree.AuthorProgram, "objective")
	if err != nil {
		t.Fatal(err)
	}
	pctx := &components.ProcessContext{
		SessionTree: tr,
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
	if node, ok := result.Tree.Node(planRootName); !ok || node.Type != tree.TypePlan {
		t.Fatal("expected the plan root to be ensured")
	}
}

func TestPlanFeedbackNotices(t *testing.T) {
	root := planRootName
	tr, err := tree.New().Write("root", planRootName, tree.TypePlan, tree.AuthorProgram, "objective")
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
					"<<未濟 plan-op:?op=done&name=plan\nAll entries complete.\n未濟\n" +
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
		if !strings.Contains(s, "Current entry: e1") || !strings.Contains(s, "Current entry: e2") {
			t.Fatal("the plan feedback must provide the pending entries across rounds")
		}
		if !strings.Contains(s, "The plan is complete") {
			t.Fatal("expected the completion notice before the session ends")
		}

		// The plan root carries its done mark and every entry is done.
		planNode, ok := result.SessionTree.Node(planRootName)
		if !ok || planNode.Type != tree.TypePlan {
			t.Fatalf("plan root missing: %+v", planNode)
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
