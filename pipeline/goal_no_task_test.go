package pipeline

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/flags"
)

// TestMakeGoalLoopGeneratorNoTask reproduces the bare-tai dead loop: a
// goal run without any chat input must yield ErrNoTask before any
// generation session runs, instead of producing an empty outcome the
// runner treats as a model output failure and loops on.
// See ErrNoTask and TheoryOfGoalNoTask.
func TestMakeGoalLoopGeneratorNoTask(t *testing.T) {
	var calls int
	scope := dscope.New(
		&flags.Chats{},
		func() GenerateWithResultWithStats {
			return func(ctx context.Context, output io.Writer) (Result, []AttemptStat, error) {
				calls++
				return Result{}, nil, nil
			}
		},
	)
	generate := makeGoalLoopGenerator(scope.Reset)
	_, _, err := generate(context.Background(), 1, "", nil, "", SessionTreeContinuation{})
	if !errors.Is(err, ErrNoTask) {
		t.Fatalf("expected ErrNoTask, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected no generation session, got %d", calls)
	}
}

// TestRunGoalNoTaskStopsRun verifies the runner stops on ErrNoTask
// instead of looping to the iteration budget.
func TestRunGoalNoTaskStopsRun(t *testing.T) {
	var buf bytes.Buffer
	var calls int
	result := RunGoal(context.Background(), GoalOptions{
		Output: &buf,
		Generate: func(ctx context.Context, loop int, feedback GoalFeedback, summaries GoalLoopSummaries, reviewModel string, _ SessionTreeContinuation) (Result, []AttemptStat, error) {
			calls++
			return Result{}, nil, ErrNoTask
		},
		Review: func(ctx context.Context, output io.Writer, diffs []changes.FileDiff) error {
			return nil
		},
	})
	if calls != 1 {
		t.Fatalf("expected the run to stop after 1 loop, got %d", calls)
	}
	if result.LoopsRun != 1 {
		t.Fatalf("expected LoopsRun 1, got %d", result.LoopsRun)
	}
	if result.Achieved {
		t.Fatal("expected Achieved false")
	}
	if !strings.Contains(buf.String(), "No task provided") {
		t.Fatalf("expected a no-task verdict, got %q", buf.String())
	}
}
