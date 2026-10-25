package pipeline

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/tree"
)

// runGoalWithApply runs a two-loop goal run: the first loop advances nothing
// and completes nothing, so the run continues; the second declares the goal
// achieved. The apply hook receives every loop number, and the goal tree
// observer receives each tree the runner writes, so a test reads what the run
// recorded. See TheoryOfLoopChangeApply.
func runGoalWithApply(t *testing.T, apply LoopChangeApply) (GoalResult, []int, *tree.Tree) {
	t.Helper()
	var loops []int
	var lastTree *tree.Tree
	result := RunGoal(context.Background(), GoalOptions{
		Output:   new(strings.Builder),
		GoalTree: func(tr *tree.Tree) { lastTree = tr },
		ApplyChanges: func(loop int) (int, []string, error) {
			loops = append(loops, loop)
			if apply == nil {
				return 0, nil, nil
			}
			return apply(loop)
		},
		Generate: func(
			ctx context.Context,
			loop int,
			feedback GoalFeedback,
			summaries GoalLoopSummaries,
			loopModel string,
			continuation SessionTreeContinuation,
		) (Result, []AttemptStat, error) {
			if loop < 2 {
				return Result{SessionTree: continuation.Tree}, nil, nil
			}
			return Result{
				RemainingBlocks: []blocks.Block{{Kind: "done"}},
				SessionTree:     continuation.Tree,
			}, nil, nil
		},
		Review: func(context.Context, io.Writer, []changes.FileDiff) error { return nil },
	})
	return result, loops, lastTree
}

// goalTreeContains reports whether any goal structure node of the tree carries
// text. See TheoryOfGoalMode.
func goalTreeContains(tr *tree.Tree, text string) bool {
	if tr == nil {
		return false
	}
	for _, n := range tr.Filter(func(n *tree.Node) bool { return n.Type == tree.TypeGoal }) {
		if strings.Contains(n.Content, text) {
			return true
		}
	}
	return false
}

func TestRunGoalAppliesEveryLoopToTheHost(t *testing.T) {
	result, loops, _ := runGoalWithApply(t, nil)
	if !result.Achieved {
		t.Fatal("run did not achieve the goal")
	}
	if len(loops) != 2 || loops[0] != 1 || loops[1] != 2 {
		t.Fatalf("apply hook received loops %v, want [1 2]", loops)
	}
}

func TestRunGoalReportsKeptBackChanges(t *testing.T) {
	result, _, observed := runGoalWithApply(t, func(loop int) (int, []string, error) {
		return 0, []string{"modified /f: host content changed"}, nil
	})
	if !result.Achieved {
		t.Fatal("a conflict stopped the run")
	}
	if !goalTreeContains(observed, "host content changed") {
		t.Fatal("the run tree does not carry the conflict")
	}
}

func TestRunGoalApplyFailureDoesNotStopTheRun(t *testing.T) {
	result, _, observed := runGoalWithApply(t, func(loop int) (int, []string, error) {
		return 0, nil, errors.New("disk full")
	})
	if !result.Achieved {
		t.Fatal("a failed apply stopped the run")
	}
	if !goalTreeContains(observed, "disk full") {
		t.Fatal("the run tree does not carry the apply failure")
	}
}
