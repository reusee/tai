package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/tree"
)

// TestFinishWithErrorPublishesFinalTree reproduces the record whose goal
// loop adopted a stale session tree: the result must be published after the
// loop's last operation applied — the drained events and the run-error
// node — because the goal runner adopts it as the run tree for the next
// loop. A result published earlier hides those names, and the next loop's
// auto-named nodes collide with them, which is how a recorded stream came
// to carry the same node name twice. See TheoryOfSessionTree and
// TheoryOfLoopEvents.
func TestFinishWithErrorPublishesFinalTree(t *testing.T) {
	withRun(t, func(run Run) {
		var result Result
		var yielded *tree.Tree
		for tr := range run(context.Background(), RunOptions{
			InitialState: generators.NewPrompts("", nil),
			PhaseBuilder: func(generators.Generator) generators.Phase {
				return errorPhase(errors.New("boom"))
			},
		}, &result) {
			if tr != nil {
				yielded = tr
			}
		}
		if yielded == nil {
			t.Fatal("expected the run to yield its session tree")
		}
		if len(yielded.ByType(tree.TypeRunError)) == 0 {
			t.Fatal("expected the yielded tree to carry the run-error node")
		}
		if result.SessionTree == nil {
			t.Fatal("expected the result to carry the run's session tree")
		}
		if len(result.SessionTree.ByType(tree.TypeRunError)) == 0 {
			t.Fatal("expected the result tree to carry the run-error node: a tree published before the loop's last operation hides the names the next loop must avoid")
		}
	})
}
