package main

import (
	"strings"
	"testing"

	"github.com/reusee/tai/taiui"
	"github.com/reusee/tai/tree"
)

// planFixtureTree builds a session tree for one goal loop whose plan
// carries two finished and two pending entries, plus a soft-deleted one.
// The counts feed the Plan tab's progress label. See
// TheoryOfTUIDynamicPlanTab.
func planFixtureTree(t *testing.T) *tree.Tree {
	t.Helper()
	tr, err := tree.New().WriteAll(
		tree.WriteOp{Parent: "root", Name: "loop-1", Type: tree.TypeLoop, Author: tree.AuthorProgram, Content: "goal loop 1"},
		tree.WriteOp{Parent: "loop-1", Name: "loop-1-plan", Type: tree.TypePlan, Author: tree.AuthorProgram, Content: "plan root"},
		tree.WriteOp{Parent: "loop-1-plan", Name: "entry-a", Type: tree.TypePlan, Author: tree.AuthorModel, Content: "write the plan tests"},
		tree.WriteOp{Parent: "entry-a", Name: "done-a", Type: tree.TypeDone, Author: tree.AuthorProgram, Content: "done"},
		tree.WriteOp{Parent: "loop-1-plan", Name: "entry-b", Type: tree.TypePlan, Author: tree.AuthorModel, Content: "second finished entry"},
		tree.WriteOp{Parent: "entry-b", Name: "done-b", Type: tree.TypeDone, Author: tree.AuthorProgram, Content: "done"},
		tree.WriteOp{Parent: "loop-1-plan", Name: "entry-c", Type: tree.TypePlan, Author: tree.AuthorModel, Content: "pending one"},
		tree.WriteOp{Parent: "loop-1-plan", Name: "entry-d", Type: tree.TypePlan, Author: tree.AuthorModel, Content: "pending two"},
		tree.WriteOp{Parent: "loop-1-plan", Name: "entry-e", Type: tree.TypePlan, Author: tree.AuthorModel, Content: "dropped entry"},
		tree.WriteOp{Parent: "entry-e", Name: "deleted-e", Type: treePlanDeleteMarkType, Author: tree.AuthorProgram, Content: "deleted"},
	)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// TestTUIPlanTabOpensOnPlanRoot pins the tab's appearance: a loop whose
// plan root exists opens the Plan tab directly after the Tree tab, and
// the tab starts expanded. See TheoryOfTUIDynamicPlanTab.
func TestTUIPlanTabOpensOnPlanRoot(t *testing.T) {
	tui := newTUIForTest()
	tui.setTree(planFixtureTree(t))

	tui.mu.Lock()
	defer tui.mu.Unlock()
	if !tui.hasPlanTab {
		t.Fatal("expected the Plan tab open while the loop carries a plan")
	}
	if got := tui.tabIndex(tabPlan); got != 1 {
		t.Fatalf("expected the Plan tab directly after the Tree tab, got index %d", got)
	}
	if !tui.tabs.Expanded[1] {
		t.Fatal("the Plan tab must start expanded")
	}
	if got := tui.tabCount(); got != 4 {
		t.Fatalf("expected 4 tabs, got %d", got)
	}
	if tui.planRoot != "loop-1-plan" {
		t.Fatalf("expected the plan root name, got %q", tui.planRoot)
	}
}

// TestTUIPlanTabClosesOnLoopChange pins the lifecycle: a newer loop node
// means the previous loop ended, so its Plan tab is destroyed and the
// new loop starts without one. See TheoryOfTUIDynamicPlanTab.
func TestTUIPlanTabClosesOnLoopChange(t *testing.T) {
	tui := newTUIForTest()
	tr := planFixtureTree(t)
	tui.setTree(tr)

	next, err := tr.Write("root", "loop-2", tree.TypeLoop, tree.AuthorProgram, "goal loop 2")
	if err != nil {
		t.Fatal(err)
	}
	tui.setTree(next)

	tui.mu.Lock()
	defer tui.mu.Unlock()
	if tui.hasPlanTab {
		t.Fatal("a newer loop must destroy the previous loop's Plan tab")
	}
	if got := tui.tabCount(); got != 3 {
		t.Fatalf("expected 3 tabs after the loop change, got %d", got)
	}
}

// TestTUIPlanTabLabelCountsProgress pins the title's progress form: the
// entries carrying a done mark over the entries still carrying work; a
// soft-deleted entry counts in neither number. See
// TheoryOfTUIDynamicPlanTab.
func TestTUIPlanTabLabelCountsProgress(t *testing.T) {
	tui := newTUIForTest()
	tui.setTree(planFixtureTree(t))

	tui.mu.Lock()
	defer tui.mu.Unlock()
	if got := tui.planTabLabel(); got != "Plan (2 / 2)" {
		t.Fatalf("expected the progress label %q, got %q", "Plan (2 / 2)", got)
	}
}

// TestTUIPlanTabDestroyedOnGenEnd pins the run's end: the last loop's
// Plan tab goes with the session. See TheoryOfTUIDynamicPlanTab.
func TestTUIPlanTabDestroyedOnGenEnd(t *testing.T) {
	tui := newTUIForTest()
	tui.setTree(planFixtureTree(t))
	tui.genEnd(nil)

	tui.mu.Lock()
	defer tui.mu.Unlock()
	if tui.hasPlanTab {
		t.Fatal("the run's end must destroy the Plan tab")
	}
	if tui.currentLoop != "" {
		t.Fatalf("expected the loop record cleared, got %q", tui.currentLoop)
	}
}

// TestTUIPlanPaneFoldsIndependently pins the pane separation: a fold in
// the Plan pane touches the Plan pane's state only, so the Tree pane's
// view never moves with it. See TheoryOfTUIDynamicPlanTab.
func TestTUIPlanPaneFoldsIndependently(t *testing.T) {
	tui := newTUIForTest()
	tui.setTree(planFixtureTree(t))

	tui.mu.Lock()
	defer tui.mu.Unlock()
	tui.withPane(tabPlan, func() {
		tui.toggleTreeNodeByName("entry-c")
	})
	if !tui.plan.expanded["entry-c"] {
		t.Fatal("the Plan pane must record its own expansion")
	}
	if tui.treeTab.expanded["entry-c"] {
		t.Fatal("a Plan-pane fold must not touch the Tree pane")
	}
}

// TestTUIPlanDisplayRendersEntries pins the rendering: the Plan tab
// shows the plan subtree with the Tree tab's own rendering, so an entry
// renders its content preview. See TheoryOfTUIDynamicPlanTab.
func TestTUIPlanDisplayRendersEntries(t *testing.T) {
	tui := newTUIForTest()
	tui.setTree(planFixtureTree(t))

	box := taiui.Box{Top: 0, Left: 0, Bottom: 10, Right: 60}
	tui.mu.Lock()
	defer tui.mu.Unlock()
	display := wrappedDisplay(tui, 1, box)
	if len(display) == 0 {
		t.Fatal("expected the Plan tab to render rows")
	}
	found := false
	for _, line := range display {
		if strings.Contains(line.Text, "write the plan tests") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the entry's preview in the Plan tab, got %v", displayTexts(display))
	}
}

// TestTUIPlanTabRendersInFrame pins the rendered frame's dynamic layout:
// with the Plan tab open and every tab expanded, one render carries the
// Plan tab's progress label and every other tab's title, so the renderer
// enumerates the layout instead of a fixed tab count and the Logs tab —
// shifted to the last index by the insertion — still renders. See
// TheoryOfTUIDynamicPlanTab.
func TestTUIPlanTabRendersInFrame(t *testing.T) {
	tui := newTUIForTest()
	tui.width, tui.height = 80, 24
	tui.setTree(planFixtureTree(t))
	var sb strings.Builder
	tui.screen = taiui.NewTerminalScreen(&sb, 80, 24)
	tui.mu.Lock()
	for i := range tui.tabs.Expanded {
		tui.tabs.Expanded[i] = true
	}
	tui.mu.Unlock()

	tui.render()

	rendered := sb.String()
	if !strings.Contains(rendered, "Plan (2 / 2)") {
		t.Fatalf("expected the Plan tab's progress label in the frame, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Logs") {
		t.Fatalf("expected the Logs tab title in the frame after the Plan tab shifted it, got:\n%s", rendered)
	}
}
