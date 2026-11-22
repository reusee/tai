package pipeline

import (
	"context"
	"io"
	"testing"

	"github.com/reusee/tai/changes"
)

// TestRunGoalStopsOnCancelledLoop pins goal mode's exit contract: a loop
// that ends with a cancelled context ends the whole run — no further loop
// starts, because each would reassemble the context and fail the same way,
// and no review request follows, because it is model work the user ended.
// See TheoryOfGoalCancellation.
func TestRunGoalStopsOnCancelledLoop(t *testing.T) {
	var loops int
	var reviews int
	result := RunGoal(context.Background(), GoalOptions{
		Output: io.Discard,
		Generate: func(
			ctx context.Context,
			loop int,
			feedback GoalFeedback,
			summaries GoalLoopSummaries,
			loopModel string,
			continuation SessionTreeContinuation,
		) (Result, []AttemptStat, error) {
			loops++
			return Result{}, nil, context.Canceled
		},
		Review: func(ctx context.Context, output io.Writer, diffs []changes.FileDiff) error {
			reviews++
			return nil
		},
	})
	if loops != 1 {
		t.Fatalf("expected one loop, got %d", loops)
	}
	if reviews != 0 {
		t.Fatalf("expected no review request after the cancellation, got %d", reviews)
	}
	if result.Achieved {
		t.Fatal("expected the cancelled run to end unachieved")
	}
}
