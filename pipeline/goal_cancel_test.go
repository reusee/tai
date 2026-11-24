package pipeline

import (
	"context"
	"io"
	"testing"
)

// TestRunGoalStopsOnCancelledLoop pins goal mode's exit contract: a loop
// that ends with a cancelled context ends the whole run — no further loop
// starts, because each would reassemble the context and fail the same way.
// See TheoryOfGoalCancellation.
func TestRunGoalStopsOnCancelledLoop(t *testing.T) {
	var loops int
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
	})
	if loops != 1 {
		t.Fatalf("expected one loop, got %d", loops)
	}
	if result.Achieved {
		t.Fatal("expected the cancelled run to end unachieved")
	}
}
