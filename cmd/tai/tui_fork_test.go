package main

import (
	"context"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/taiui"
	"github.com/reusee/tai/tree"
)

// TestForkTUIDisplayForwardsTreesToTUI verifies that the run's tree
// iterator reaches the TUI through the tap fork: every yielded tree is
// stored by setTree, so the Tree tab renders the same tree the pipeline
// writes. See TheoryOfTUIDisplayFork and pipeline.TheoryOfLoopEvents.
func TestForkTUIDisplayForwardsTreesToTUI(t *testing.T) {
	tui := newTUIForTest()
	scope := forkTUIDisplay(
		dscope.New(modes.ForTest(t), new(pipeline.Module)),
		tui,
	)

	scope.Call(func(run pipeline.Run) {
		var result pipeline.Result
		for _, err := range run(context.Background(), pipeline.RunOptions{
			InitialState: generators.NewPrompts("", nil),
			PhaseBuilder: func(_ generators.Generator) generators.Phase {
				return func(_ context.Context, state generators.State) (generators.Phase, generators.State, error) {
					state, err := state.AppendContent(&generators.Content{
						Role: generators.RoleModel,
						Parts: []generators.Part{
							generators.Text("ok\n"),
						},
					})
					if err != nil {
						return nil, nil, err
					}
					return nil, state, nil
				}
			},
		}, &result) {
			if err != nil {
				t.Fatal(err)
			}
		}
	})

	tui.mu.Lock()
	defer tui.mu.Unlock()
	if tui.treeView == nil {
		t.Fatal("expected the TUI to hold the run's tree")
	}
	if len(tui.treeView.ByCategory(tree.CategoryEvent)) == 0 {
		t.Fatal("expected event nodes in the TUI's tree")
	}
	// The tree is the pipeline's own session tree: the attempt's
	// response node is present.
	if _, ok := tui.treeView.Node("response-1"); !ok {
		t.Fatal("expected the response node in the TUI's tree")
	}
}

// TestForkTUIDisplayDecoratesHandoffState verifies that the handoff
// decorator from the display scope observes content parts, so handoff
// output is highlighted per part and per thinking state in the Output
// tab. See TheoryOfTUIHandoff.
func TestForkTUIDisplayDecoratesHandoffState(t *testing.T) {
	tui := newTUIForTest()
	scope := forkTUIDisplay(
		dscope.New(modes.ForTest(t), new(pipeline.Module)),
		tui,
	)

	scope.Call(func(decorate pipeline.HandoffStateDecorator) {
		state := decorate(generators.NewPrompts("", nil))
		_, err := state.AppendContent(&generators.Content{
			Role: generators.RoleModel,
			Parts: []generators.Part{
				generators.Thought("handoff thinking"),
				generators.Text("handoff text\n"),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	tui.mu.Lock()
	defer tui.mu.Unlock()
	lines := tui.output.Lines()
	if len(lines) == 0 {
		t.Fatal("expected lines in the output buffer")
	}
	if lines[0].Text != "handoff thinking" || lines[0].Color != outputColorThoughtLine {
		t.Fatalf("expected the thought line in the thought color, got %+v", lines[0])
	}
	found := false
	for _, line := range lines {
		if line.Text == "handoff text" {
			found = true
			if line.Color != taiui.NoColor {
				t.Fatalf("expected the text line in the default color, got %+v", lines)
			}
		}
	}
	if !found {
		t.Fatalf("expected the handoff text line in the output buffer, got %v", lines)
	}
}

// TestForkTUIDisplayForwardsGoalTreeToTUI verifies that the goal tree
// observer forked by forkTUIDisplay forwards the goal runner's tree to
// the TUI, so the Tree tab renders the verdict nodes without a writer
// fork. See TheoryOfTUIDisplayFork.
func TestForkTUIDisplayForwardsGoalTreeToTUI(t *testing.T) {
	tui := newTUIForTest()
	scope := forkTUIDisplay(
		dscope.New(modes.ForTest(t), new(pipeline.Module)),
		tui,
	)

	scope.Call(func(observe pipeline.GoalTreeObserver) {
		if observe == nil {
			t.Fatal("expected the TUI fork to provide a goal tree observer")
		}
		observe(tree.New())
	})

	tui.mu.Lock()
	defer tui.mu.Unlock()
	if tui.treeView == nil {
		t.Fatal("expected the goal tree stored in the TUI")
	}
}
