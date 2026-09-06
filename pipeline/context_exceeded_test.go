package pipeline

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
)

func TestRunContextExceededEndsRun(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			return appendThenErrorPhase(
				"partial model output",
				errors.New(`Error 400: This model's maximum context length is 128000 tokens. However, you requested 130000 tokens.`),
			)
		}

		_, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   nil,
			PhaseBuilder: phaseBuilder,
			RetryOnError: true,
			MaxRetries:   3,
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "context summary", Prompt: "context handoff"}, nil
			},
		})
		// A context-exceeded failure cannot be repaired by retrying
		// the attempt: the run must end instead. See
		// TheoryOfContextExceededHandoff.
		var exceededErr *ContextExceededError
		if err == nil || !errors.As(err, &exceededErr) {
			t.Fatalf("expected ContextExceededError, got: %v", err)
		}
		if callCount != 1 {
			t.Fatalf("expected 1 call (no retry), got %d", callCount)
		}
		if exceededErr.Handoff == nil || exceededErr.Handoff.Prompt != "context handoff" {
			t.Fatalf("expected the handoff to be carried, got %+v", exceededErr.Handoff)
		}
	})
}

func TestRunGoalContextExceededFeedsNextLoop(t *testing.T) {
	calls := 0
	var feedbacks []GoalFeedback
	result := RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(ctx context.Context, _ int, feedback GoalFeedback, _ GoalLoopSummaries, _ string, _ SessionTreeContinuation) (Result, []AttemptStat, error) {
			calls++
			feedbacks = append(feedbacks, feedback)
			switch calls {
			case 1, 2:
				return Result{}, nil, &ContextExceededError{
					Err:     errors.New("maximum context length exceeded"),
					Handoff: &Handoff{Prompt: "handoff notes from the over-limit loop"},
				}
			case 3:
				// The declaring loop carries applied changes; a done
				// block without changes would end the run directly.
				// See TheoryOfGoalMode.
				return doneWithChangesResult(), nil, nil
			}
			return doneResult(), nil, nil
		},
		Review: noopReview,
	})
	if !result.Achieved {
		t.Fatal("goal should be achieved")
	}
	// Two context-exceeded handoffs count toward the consecutive-error
	// bound but leave room for the run to continue and confirm the
	// goal. See TheoryOfContextExceededHandoff.
	if calls != 4 {
		t.Fatalf("expected 4 loops (2 handoffs, done + verification), got %d", calls)
	}
	if !strings.Contains(string(feedbacks[1]), "maximum context length") || !strings.Contains(string(feedbacks[1]), "handoff notes") {
		t.Fatalf("the handoff feedback must carry the error and the handoff notes, got %q", feedbacks[1])
	}
}

func TestRunGoalContextExceededStopsAfterConsecutive(t *testing.T) {
	calls := 0
	result := RunGoal(context.Background(), GoalOptions{
		Output: &bytes.Buffer{},
		Generate: func(ctx context.Context, _ int, _ GoalFeedback, _ GoalLoopSummaries, _ string, _ SessionTreeContinuation) (Result, []AttemptStat, error) {
			calls++
			return Result{}, nil, &ContextExceededError{
				Err: errors.New("maximum context length exceeded"),
			}
		},
		Review: noopReview,
	})
	// The over-limit context is accumulated by the loop itself, so the
	// failure counts toward the consecutive-error bound: three
	// identical failures stop the run. See
	// TheoryOfContextExceededHandoff.
	if calls != maxConsecutiveGoalErrors {
		t.Fatalf("ran %d loops, want %d", calls, maxConsecutiveGoalErrors)
	}
	if result.Achieved {
		t.Fatal("goal must not be achieved")
	}
}
