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
	if _, ok := tui.treeView.Node("model-1"); !ok {
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

// TestForkTUIDisplayKeepsSessionTreeContinuation pins the decorator
// mechanism: a SessionTreeContinuation forked AFTER the display fork
// must reach Module.Run, so the loop writes its session nodes under
// the continuation's loop node and the TUI's tree carries the loop
// node. The static Run wrapper this replaces shadowed Module.Run,
// freezing the continuation at zero — every loop opened a fresh tree
// and the Tree tab showed only the current loop. See
// TheoryOfTUIDisplayFork and pipeline.TheoryOfRunDecorators.
func TestForkTUIDisplayKeepsSessionTreeContinuation(t *testing.T) {
	tui := newTUIForTest()
	scope := forkTUIDisplay(
		dscope.New(modes.ForTest(t), new(pipeline.Module)),
		tui,
	)
	// Simulate the goal runner: the loop node exists in the run tree
	// and the continuation is forked into the loop's scope after the
	// display fork.
	runTree, err := tree.New().Write("root", "loop-1", tree.TypeLoop, tree.AuthorProgram, "goal loop 1")
	if err != nil {
		t.Fatal(err)
	}
	scope = scope.Fork(func() pipeline.SessionTreeContinuation {
		return pipeline.SessionTreeContinuation{Tree: runTree, Parent: "loop-1"}
	})

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
		if result.SessionTree == nil {
			t.Fatal("expected the result to carry the continued tree")
		}
	})

	tui.mu.Lock()
	defer tui.mu.Unlock()
	loopNode, ok := tui.treeView.Node("loop-1")
	if !ok || loopNode.Type != tree.TypeLoop {
		t.Fatalf("expected the loop-1 node in the TUI's tree, got ok=%v", ok)
	}
	response, ok := tui.treeView.Node("model-1")
	if !ok || response.Parent != "loop-1" {
		t.Fatalf("expected the loop's response under loop-1, got ok=%v", ok)
	}
}
